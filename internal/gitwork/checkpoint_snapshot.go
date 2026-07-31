package gitwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

const maximumCheckpointDirtyBytes int64 = 1 << 30

type checkpointSnapshotRepository interface {
	LoadCheckpoint(
		context.Context,
		domain.CheckpointID,
	) (checkpoint.PersistedCheckpoint, error)
	GetRepository(context.Context, domain.RepositoryID) (storage.Repository, error)
}

type checkpointObservation struct {
	verification WorktreeVerification
	diff         TaskDiff
	hashes       []checkpoint.DirtyFileHash
}

// CaptureCheckpointWorktreeState creates an immutable recovery commit through
// an isolated Git index. It never moves HEAD, changes the task branch, stages
// the user's files in the real index, or cleans the dirty worktree.
func (service *Service) CaptureCheckpointWorktreeState(
	ctx context.Context,
	taskID domain.TaskID,
	checkpointID domain.CheckpointID,
) (checkpoint.WorktreeState, error) {
	if service == nil || taskID.IsZero() || checkpointID.IsZero() {
		return checkpoint.WorktreeState{},
			errors.New("checkpoint Git service and identities are required")
	}
	before, err := service.observeCheckpointWorktree(ctx, taskID)
	if err != nil {
		return checkpoint.WorktreeState{}, err
	}
	reference := checkpointReferencePrefix + checkpointID.String()
	preserved, err := service.preserveCheckpointCommit(
		ctx,
		before.verification.Binding,
		reference,
	)
	if err != nil {
		return checkpoint.WorktreeState{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = service.RemoveCheckpointWorktreePreservation(
				context.Background(),
				taskID,
				checkpointID,
				reference,
				preserved,
			)
		}
	}()
	after, err := service.observeCheckpointWorktree(ctx, taskID)
	if err != nil {
		return checkpoint.WorktreeState{}, err
	}
	if before.verification.Binding != after.verification.Binding ||
		before.verification.Dirty != after.verification.Dirty ||
		!slices.Equal(
			before.verification.ChangedPaths,
			after.verification.ChangedPaths,
		) ||
		before.diff.Identity != after.diff.Identity ||
		!slices.Equal(before.hashes, after.hashes) {
		return checkpoint.WorktreeState{}, errors.New(
			"task worktree changed while checkpoint state was preserved",
		)
	}
	cleanup = false
	binding := after.verification.Binding
	return checkpoint.WorktreeState{
		RepositoryID:            binding.RepositoryID,
		WorktreeBindingRevision: binding.Revision,
		BaseRevision:            binding.BaseRevision,
		HeadRevision:            binding.HeadRevision,
		PreservedRevision:       preserved,
		PreservedRef:            reference,
		DirtyFiles:              after.hashes,
		DiffSHA256:              after.diff.Identity,
	}, nil
}

// RemoveCheckpointWorktreePreservation removes only the exact private ref
// created for an uncommitted or deduplicated checkpoint attempt.
func (service *Service) RemoveCheckpointWorktreePreservation(
	ctx context.Context,
	taskID domain.TaskID,
	checkpointID domain.CheckpointID,
	reference string,
	revision string,
) error {
	if service == nil || taskID.IsZero() || checkpointID.IsZero() ||
		reference != checkpointReferencePrefix+checkpointID.String() ||
		revision == "" {
		return errors.New("checkpoint preservation cleanup identity is invalid")
	}
	binding, err := service.bindings.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return err
	}
	_, err = service.runner.Run(
		ctx,
		binding.WorktreePath,
		"git",
		"update-ref",
		"-d",
		reference,
		revision,
	)
	if err != nil {
		return fmt.Errorf("remove checkpoint preservation ref: %w", err)
	}
	return nil
}

func (service *Service) observeCheckpointWorktree(
	ctx context.Context,
	taskID domain.TaskID,
) (checkpointObservation, error) {
	verification, err := service.VerifyTaskWorktree(ctx, taskID)
	if err != nil {
		return checkpointObservation{}, err
	}
	if verification.RecoveryNeeded ||
		!verification.PathPresent ||
		!verification.RootMatches ||
		!verification.BranchMatches ||
		!verification.HeadMatches {
		return checkpointObservation{},
			errors.New("task worktree is not safe for checkpoint capture")
	}
	paths := append([]string(nil), verification.ChangedPaths...)
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if len(paths) > 2048 {
		return checkpointObservation{},
			errors.New("task worktree dirty-file count exceeds checkpoint bound")
	}
	hashes, err := hashCheckpointDirtyFiles(ctx, verification.Binding, paths)
	if err != nil {
		return checkpointObservation{}, err
	}
	diff, err := service.GetTaskDiff(ctx, TaskDiffQuery{TaskID: taskID})
	if err != nil {
		return checkpointObservation{}, err
	}
	if diff.TaskID != taskID ||
		diff.BaseRevision != verification.Binding.BaseRevision ||
		diff.HeadRevision != verification.Binding.HeadRevision {
		return checkpointObservation{},
			errors.New("task diff is not bound to the verified worktree")
	}
	return checkpointObservation{
		verification: verification,
		diff:         diff,
		hashes:       hashes,
	}, nil
}

func (service *Service) preserveCheckpointCommit(
	ctx context.Context,
	binding storage.WorktreeBinding,
	reference string,
) (string, error) {
	runner, ok := service.runner.(environmentCommandRunner)
	if !ok {
		return "", errors.New(
			"command runner does not support isolated checkpoint indexes",
		)
	}
	temporary, err := os.CreateTemp(
		service.root,
		".codeflux-checkpoint-index-*",
	)
	if err != nil {
		return "", fmt.Errorf("create isolated checkpoint index: %w", err)
	}
	indexPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(indexPath)
		return "", fmt.Errorf("close isolated checkpoint index: %w", err)
	}
	if err := os.Remove(indexPath); err != nil {
		return "", fmt.Errorf("prepare isolated checkpoint index: %w", err)
	}
	defer os.Remove(indexPath)
	environment := map[string]string{
		"GIT_INDEX_FILE":      indexPath,
		"GIT_AUTHOR_NAME":     "Codeflux Checkpoint",
		"GIT_AUTHOR_EMAIL":    "checkpoint@codeflux.invalid",
		"GIT_AUTHOR_DATE":     "2000-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME":  "Codeflux Checkpoint",
		"GIT_COMMITTER_EMAIL": "checkpoint@codeflux.invalid",
		"GIT_COMMITTER_DATE":  "2000-01-01T00:00:00Z",
	}
	if _, err := runner.RunEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"read-tree",
		binding.HeadRevision,
	); err != nil {
		return "", fmt.Errorf("seed isolated checkpoint index: %w", err)
	}
	if _, err := runner.RunEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"add",
		"-A",
		"--",
		".",
	); err != nil {
		return "", fmt.Errorf("stage isolated checkpoint state: %w", err)
	}
	treeResult, err := runner.RunEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"write-tree",
	)
	if err != nil {
		return "", fmt.Errorf("write checkpoint tree: %w", err)
	}
	tree := strings.TrimSpace(string(treeResult.Stdout))
	commitResult, err := runner.RunEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"commit-tree",
		tree,
		"-p",
		binding.HeadRevision,
		"-m",
		"Codeflux preserved checkpoint",
	)
	if err != nil {
		return "", fmt.Errorf("write checkpoint commit: %w", err)
	}
	commit := strings.TrimSpace(string(commitResult.Stdout))
	if err := validateObjectID(commit); err != nil {
		return "", err
	}
	if existing, resolveErr := service.gitText(
		ctx,
		binding.WorktreePath,
		"rev-parse",
		"--verify",
		reference+"^{commit}",
	); resolveErr == nil {
		if existing != commit {
			return "", errors.New(
				"checkpoint preservation ref already identifies another commit",
			)
		}
		return commit, nil
	}
	if _, err := service.runner.Run(
		ctx,
		binding.WorktreePath,
		"git",
		"update-ref",
		reference,
		commit,
	); err != nil {
		return "", fmt.Errorf("pin checkpoint preservation ref: %w", err)
	}
	return commit, nil
}

func hashCheckpointDirtyFiles(
	ctx context.Context,
	binding storage.WorktreeBinding,
	paths []string,
) ([]checkpoint.DirtyFileHash, error) {
	result := make([]checkpoint.DirtyFileHash, 0, len(paths))
	var total int64
	for _, relativePath := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, err := ResolveTaskPath(binding, relativePath)
		if errors.Is(err, os.ErrNotExist) {
			result = append(result, checkpoint.DirtyFileHash{Path: relativePath})
			continue
		}
		if err != nil {
			if missingCheckpointPath(binding, relativePath) {
				result = append(
					result,
					checkpoint.DirtyFileHash{Path: relativePath},
				)
				continue
			}
			return nil, err
		}
		info, err := os.Lstat(resolved.Absolute)
		if errors.Is(err, os.ErrNotExist) {
			result = append(
				result,
				checkpoint.DirtyFileHash{Path: resolved.Relative},
			)
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New(
				"checkpoint dirty path is not a regular file",
			)
		}
		if info.Size() < 0 ||
			info.Size() > maximumCheckpointDirtyBytes-total {
			return nil, errors.New(
				"checkpoint dirty-file bytes exceed the hash bound",
			)
		}
		total += info.Size()
		digest, err := hashCheckpointFile(ctx, resolved.Absolute)
		if err != nil {
			return nil, err
		}
		result = append(result, checkpoint.DirtyFileHash{
			Path: resolved.Relative, Exists: true, SHA256: digest,
		})
	}
	return result, nil
}

func missingCheckpointPath(
	binding storage.WorktreeBinding,
	relativePath string,
) bool {
	if relativePath == "" || strings.Contains(relativePath, "\\") ||
		filepath.IsAbs(relativePath) ||
		strings.Contains(relativePath, "..") {
		return false
	}
	target := filepath.Join(
		binding.WorktreePath,
		filepath.FromSlash(relativePath),
	)
	_, err := os.Lstat(target)
	return errors.Is(err, os.ErrNotExist)
}

func hashCheckpointFile(
	ctx context.Context,
	path string,
) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count != 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type outputCommandRunner interface {
	RunOutput(
		context.Context,
		string,
		string,
		string,
		...string,
	) error
}

// RunOutput executes a fixed-argument command with stdout written directly to
// an already scoped file, avoiding the bounded in-memory process buffer.
func (ExecRunner) RunOutput(
	ctx context.Context,
	directory string,
	executable string,
	outputPath string,
	arguments ...string,
) error {
	output, err := os.OpenFile(
		outputPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return err
	}
	defer output.Close()
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	command.Stdout = output
	var stderr boundedBuffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", executable, err)
	}
	if stderr.overflow {
		return errors.New("command error output exceeded the bounded capture limit")
	}
	return output.Sync()
}

// PreserveCheckpointPatch materializes a binary-safe patch from the durable
// preserved ref. It resolves the shared repository directly and therefore
// remains available when the task worktree has disappeared.
func (service *Service) PreserveCheckpointPatch(
	ctx context.Context,
	checkpointID domain.CheckpointID,
) (string, bool, error) {
	if service == nil {
		return "", false, errors.New(
			"durable checkpoint snapshot repository is required",
		)
	}
	repository, ok := service.checkpoints.(checkpointSnapshotRepository)
	if !ok {
		return "", false, errors.New(
			"durable checkpoint snapshot repository is required",
		)
	}
	persisted, err := repository.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return "", false, err
	}
	state, err := checkpoint.DecodeCanonicalState(
		persisted.StateJSON,
		persisted.StateSHA256,
	)
	if err != nil {
		return "", false, err
	}
	source, err := repository.GetRepository(
		ctx,
		state.Snapshot.RepositoryID,
	)
	if err != nil {
		return "", false, err
	}
	pinned, err := service.gitText(
		ctx,
		source.CanonicalPath,
		"rev-parse",
		"--verify",
		persisted.PreservedRef+"^{commit}",
	)
	if err != nil || pinned != persisted.PreservedRevision ||
		pinned != state.Snapshot.PreservedRevision {
		return "", false, nil
	}
	runner, ok := service.runner.(outputCommandRunner)
	if !ok {
		return "", false, errors.New(
			"command runner does not support checkpoint patch export",
		)
	}
	directory := filepath.Join(service.root, ".checkpoint-patches")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", false, err
	}
	finalPath := filepath.Join(directory, checkpointID.String()+".patch")
	if info, statErr := os.Stat(finalPath); statErr == nil &&
		info.Mode().IsRegular() {
		return finalPath, true, nil
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", false, statErr
	}
	temporary, err := os.CreateTemp(directory, checkpointID.String()+"-*.tmp")
	if err != nil {
		return "", false, err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", false, err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return "", false, err
	}
	defer os.Remove(temporaryPath)
	if err := runner.RunOutput(
		ctx,
		source.CanonicalPath,
		"git",
		temporaryPath,
		"diff",
		"--binary",
		"--no-color",
		"--no-ext-diff",
		state.Snapshot.BaseRevision,
		persisted.PreservedRevision,
		"--",
	); err != nil {
		return "", false, err
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", false, err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(finalPath, 0o600)
	}
	return finalPath, true, nil
}

var _ checkpoint.WorktreeStateSource = (*Service)(nil)

package gitwork

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

const maximumBranchAttempts = 8

var (
	ErrWorktreeConflict = errors.New("task worktree conflict")
	ErrWorktreeMissing  = errors.New("task worktree missing")
	ErrExternalCommit   = errors.New("task worktree contains an external commit")
	ErrWorktreeMoved    = errors.New("task worktree moved")
)

// BindingRepository is the durable worktree-binding boundary.
type BindingRepository interface {
	CreateWorktreeBinding(context.Context, storage.CreateWorktreeBinding) (storage.WorktreeBinding, error)
	GetWorktreeBinding(context.Context, domain.TaskID) (storage.WorktreeBinding, error)
	AdvanceWorktreeBinding(context.Context, storage.AdvanceWorktreeBinding) (storage.WorktreeBinding, error)
	TransitionWorktreeBinding(context.Context, storage.TransitionWorktreeBinding) (storage.WorktreeBinding, error)
}

// Service owns isolated task worktree lifecycle.
type Service struct {
	root        string
	bindings    BindingRepository
	runner      CommandRunner
	random      io.Reader
	events      EditEventRecorder
	checkpoints CheckpointRepository
}

// CreateWorktreeInput identifies the exact task and base Git revision.
type CreateWorktreeInput struct {
	TaskID         domain.TaskID
	RepositoryID   domain.RepositoryID
	RepositoryPath string
	BaseRevision   string
}

// CreateWorktreeResult reports the durable binding and primary-worktree risk.
type CreateWorktreeResult struct {
	Binding      storage.WorktreeBinding
	PrimaryDirty bool
}

// WorktreeVerification reports attributable worktree state without treating
// ordinary uncommitted task edits as an error.
type WorktreeVerification struct {
	Binding        storage.WorktreeBinding
	PathPresent    bool
	RootMatches    bool
	BranchMatches  bool
	HeadMatches    bool
	Dirty          bool
	ChangedPaths   []string
	RecoveryNeeded bool
}

// NewService validates the external task-worktree root.
func NewService(
	root string,
	bindings BindingRepository,
	runner CommandRunner,
	randomSource io.Reader,
) (*Service, error) {
	if bindings == nil || runner == nil {
		return nil, errors.New("binding repository and command runner are required")
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve worktree root: %w", err)
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != absolute ||
		filepath.Dir(absolute) == absolute {
		return nil, errors.New("worktree root must be a clean absolute non-root path")
	}
	return &Service{
		root: absolute, bindings: bindings, runner: runner, random: randomSource,
	}, nil
}

// WorktreePath applies the fixed root/repository/task naming convention.
func (service *Service) WorktreePath(
	repositoryID domain.RepositoryID,
	taskID domain.TaskID,
) (string, error) {
	if repositoryID.IsZero() || taskID.IsZero() {
		return "", errors.New("repository and task IDs are required")
	}
	path := filepath.Join(service.root, repositoryID.String(), taskID.String())
	if !pathWithin(service.root, path) {
		return "", errors.New("derived worktree path escaped its root")
	}
	return path, nil
}

// CreateTaskWorktree creates and verifies a dedicated branch/worktree before
// recording all binding fields in one SQLite transaction.
func (service *Service) CreateTaskWorktree(
	ctx context.Context,
	input CreateWorktreeInput,
) (CreateWorktreeResult, error) {
	if input.TaskID.IsZero() || input.RepositoryID.IsZero() {
		return CreateWorktreeResult{}, errors.New("task and repository IDs are required")
	}
	repositoryPath, err := canonicalDirectory(input.RepositoryPath)
	if err != nil {
		return CreateWorktreeResult{}, err
	}
	if pathWithin(repositoryPath, service.root) || pathWithin(service.root, repositoryPath) {
		return CreateWorktreeResult{}, errors.New("worktree root and repository must not overlap")
	}
	base, err := service.gitText(
		ctx,
		repositoryPath,
		"rev-parse",
		"--verify",
		input.BaseRevision+"^{commit}",
	)
	if err != nil {
		return CreateWorktreeResult{}, fmt.Errorf("verify base revision: %w", err)
	}
	if err := validateObjectID(base); err != nil {
		return CreateWorktreeResult{}, err
	}
	if input.BaseRevision != base {
		return CreateWorktreeResult{}, errors.New("base revision must be the full resolved Git object ID")
	}
	worktreePath, err := service.WorktreePath(input.RepositoryID, input.TaskID)
	if err != nil {
		return CreateWorktreeResult{}, err
	}
	if _, err := os.Lstat(worktreePath); err == nil {
		return CreateWorktreeResult{}, ErrWorktreeConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return CreateWorktreeResult{}, fmt.Errorf("inspect worktree path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		return CreateWorktreeResult{}, fmt.Errorf("create worktree parent: %w", err)
	}
	status, err := service.gitText(ctx, repositoryPath, "status", "--porcelain=v1")
	if err != nil {
		return CreateWorktreeResult{}, fmt.Errorf("inspect primary worktree: %w", err)
	}

	branch, err := service.createWorktree(ctx, repositoryPath, worktreePath, input.TaskID, base)
	if err != nil {
		return CreateWorktreeResult{}, err
	}
	cleanup := func() {
		_, _ = service.runner.Run(
			context.Background(),
			repositoryPath,
			"git",
			"worktree",
			"remove",
			"--force",
			worktreePath,
		)
		_, _ = service.runner.Run(
			context.Background(),
			repositoryPath,
			"git",
			"branch",
			"-D",
			branch,
		)
	}
	head, err := service.gitText(ctx, worktreePath, "rev-parse", "--verify", "HEAD")
	if err != nil || head != base {
		cleanup()
		return CreateWorktreeResult{}, errors.New("new task worktree did not start at the expected base")
	}
	workspaceID, err := domain.NewWorkspaceID()
	if err != nil {
		cleanup()
		return CreateWorktreeResult{}, fmt.Errorf("create workspace identity: %w", err)
	}
	binding, err := service.bindings.CreateWorktreeBinding(
		ctx,
		storage.CreateWorktreeBinding{
			WorkspaceID:  workspaceID,
			TaskID:       input.TaskID,
			RepositoryID: input.RepositoryID,
			BaseRevision: base,
			HeadRevision: base,
			BranchName:   branch,
			WorktreePath: worktreePath,
		},
	)
	if err != nil {
		cleanup()
		return CreateWorktreeResult{}, err
	}
	return CreateWorktreeResult{
		Binding: binding, PrimaryDirty: status != "",
	}, nil
}

func (service *Service) createWorktree(
	ctx context.Context,
	repositoryPath string,
	worktreePath string,
	taskID domain.TaskID,
	base string,
) (string, error) {
	for attempt := 0; attempt < maximumBranchAttempts; attempt++ {
		branch, err := service.branchName(taskID)
		if err != nil {
			return "", err
		}
		_, err = service.runner.Run(
			ctx,
			repositoryPath,
			"git",
			"show-ref",
			"--verify",
			"--quiet",
			"refs/heads/"+branch,
		)
		if err == nil {
			continue
		}
		if _, err := service.runner.Run(
			ctx,
			repositoryPath,
			"git",
			"-c",
			"core.hooksPath="+service.disabledHooksPath(),
			"worktree",
			"add",
			"-b",
			branch,
			worktreePath,
			base,
		); err != nil {
			if _, statErr := os.Lstat(worktreePath); statErr == nil {
				_, _ = service.runner.Run(
					context.Background(),
					repositoryPath,
					"git",
					"worktree",
					"remove",
					"--force",
					worktreePath,
				)
			}
			_, _ = service.runner.Run(
				context.Background(),
				repositoryPath,
				"git",
				"branch",
				"-D",
				branch,
			)
			return "", fmt.Errorf("create task worktree: %w", err)
		}
		return branch, nil
	}
	return "", errors.New("allocate task branch: collision retry limit exhausted")
}

func (service *Service) branchName(taskID domain.TaskID) (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := io.ReadFull(service.random, randomBytes); err != nil {
		return "", fmt.Errorf("read task branch entropy: %w", err)
	}
	taskPart := strings.TrimPrefix(taskID.String(), "tsk_")
	taskPart = strings.ReplaceAll(taskPart, "-", "")
	if len(taskPart) > 16 {
		taskPart = taskPart[len(taskPart)-16:]
	}
	return "codeflux/task/" + taskPart + "-" + hex.EncodeToString(randomBytes), nil
}

// VerifyTaskWorktree detects deletion, movement, branch replacement, external
// commits, and current uncommitted edits.
func (service *Service) VerifyTaskWorktree(
	ctx context.Context,
	taskID domain.TaskID,
) (WorktreeVerification, error) {
	binding, err := service.bindings.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return WorktreeVerification{}, err
	}
	report := WorktreeVerification{Binding: binding}
	info, err := os.Stat(binding.WorktreePath)
	if errors.Is(err, os.ErrNotExist) {
		report.RecoveryNeeded = true
		return report, ErrWorktreeMissing
	}
	if err != nil || !info.IsDir() {
		report.RecoveryNeeded = true
		return report, ErrWorktreeMissing
	}
	report.PathPresent = true
	canonical, err := canonicalDirectory(binding.WorktreePath)
	if err != nil || canonical != binding.WorktreePath ||
		!pathWithin(service.root, canonical) {
		report.RecoveryNeeded = true
		return report, ErrWorktreeMoved
	}
	root, err := service.gitText(ctx, canonical, "rev-parse", "--show-toplevel")
	if err != nil {
		report.RecoveryNeeded = true
		return report, ErrWorktreeMoved
	}
	root, err = canonicalDirectory(root)
	if err != nil || root != canonical {
		report.RecoveryNeeded = true
		return report, ErrWorktreeMoved
	}
	report.RootMatches = true
	branch, err := service.gitText(ctx, canonical, "symbolic-ref", "--short", "HEAD")
	if err != nil || branch != binding.BranchName {
		report.RecoveryNeeded = true
		return report, ErrExternalCommit
	}
	report.BranchMatches = true
	head, err := service.gitText(ctx, canonical, "rev-parse", "--verify", "HEAD")
	if err != nil || head != binding.HeadRevision {
		report.RecoveryNeeded = true
		return report, ErrExternalCommit
	}
	report.HeadMatches = true
	statusResult, err := service.runner.Run(
		ctx,
		canonical,
		"git",
		"status",
		"--porcelain=v1",
		"-z",
		"--untracked-files=all",
	)
	if err != nil {
		return report, fmt.Errorf("inspect task worktree: %w", err)
	}
	report.ChangedPaths = parsePorcelainPaths(string(statusResult.Stdout))
	report.Dirty = len(report.ChangedPaths) != 0
	return report, nil
}

// AbandonTaskWorktree releases the binding without deleting its path or branch.
func (service *Service) AbandonTaskWorktree(
	ctx context.Context,
	taskID domain.TaskID,
) (storage.WorktreeBinding, error) {
	binding, err := service.bindings.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return storage.WorktreeBinding{}, err
	}
	return service.bindings.TransitionWorktreeBinding(
		ctx,
		storage.TransitionWorktreeBinding{
			TaskID:           taskID,
			ExpectedRevision: binding.Revision,
			From:             storage.WorktreeBindingActive,
			To:               storage.WorktreeBindingReleased,
		},
	)
}

// CleanupTaskWorktree removes only a released binding's validated worktree.
// The task branch remains for recovery and attribution.
func (service *Service) CleanupTaskWorktree(
	ctx context.Context,
	taskID domain.TaskID,
	repositoryPath string,
) error {
	binding, err := service.bindings.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return err
	}
	if binding.State != storage.WorktreeBindingReleased {
		return errors.New("only a released worktree may be cleaned up")
	}
	canonicalRepository, err := canonicalDirectory(repositoryPath)
	if err != nil {
		return err
	}
	expected, err := service.WorktreePath(binding.RepositoryID, binding.TaskID)
	if err != nil {
		return err
	}
	if binding.WorktreePath != expected || !pathWithin(service.root, expected) {
		return ErrWorktreeMoved
	}
	if _, err := os.Stat(expected); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect released task worktree: %w", err)
	}
	if _, err := service.runner.Run(
		ctx,
		canonicalRepository,
		"git",
		"worktree",
		"remove",
		"--force",
		expected,
	); err != nil {
		return fmt.Errorf("remove released task worktree: %w", err)
	}
	return nil
}

func (service *Service) gitText(
	ctx context.Context,
	directory string,
	arguments ...string,
) (string, error) {
	result, err := service.runner.Run(ctx, directory, "git", arguments...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func (service *Service) disabledHooksPath() string {
	return filepath.Join(service.root, ".codeflux-hooks-disabled")
}

func canonicalDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("directory path must not be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve directory links: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("path must identify a directory")
	}
	return filepath.Clean(resolved), nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateObjectID(value string) error {
	if len(value) != 40 && len(value) != 64 {
		return errors.New("git object ID must contain 40 or 64 lowercase hexadecimal characters")
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return errors.New("git object ID must contain 40 or 64 lowercase hexadecimal characters")
		}
	}
	return nil
}

func parsePorcelainPaths(status string) []string {
	fields := strings.Split(status, "\x00")
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 4 {
			continue
		}
		path := field[3:]
		if separator := strings.Index(path, " -> "); separator >= 0 {
			path = path[separator+4:]
		}
		paths = append(paths, filepath.ToSlash(path))
	}
	return paths
}

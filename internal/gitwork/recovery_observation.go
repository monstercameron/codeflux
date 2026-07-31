package gitwork

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/storage"
)

// ObserveRecoveryWorktree reads current Git and file facts without requiring
// the task branch, HEAD, dirty state, or in-progress Git operation to match the
// checkpoint. It never updates Git metadata, the index, or workspace files.
func (service *Service) ObserveRecoveryWorktree(
	ctx context.Context,
	binding storage.WorktreeBinding,
	snapshot checkpoint.Snapshot,
) (checkpoint.RecoveryWorktreeFacts, error) {
	if service == nil || binding.TaskID.IsZero() ||
		binding.RepositoryID.IsZero() || snapshot.TaskID.IsZero() {
		return checkpoint.RecoveryWorktreeFacts{},
			errors.New("recovery worktree observation identities are required")
	}
	facts := checkpoint.RecoveryWorktreeFacts{
		RepositoryID:            binding.RepositoryID,
		BindingRevision:         binding.Revision,
		DirtyFiles:              []checkpoint.DirtyFileHash{},
		UnresolvedGitOperations: []string{},
	}
	info, err := os.Stat(binding.WorktreePath)
	if err != nil || !info.IsDir() {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return checkpoint.RecoveryWorktreeFacts{}, ctxErr
		}
		return facts, nil
	}
	facts.Exists = true
	expected, expectedErr := service.WorktreePath(
		binding.RepositoryID,
		binding.TaskID,
	)
	canonical, canonicalErr := canonicalDirectory(binding.WorktreePath)
	if expectedErr != nil || canonicalErr != nil ||
		!samePath(expected, binding.WorktreePath) ||
		!samePath(canonical, binding.WorktreePath) ||
		!pathWithin(service.root, canonical) {
		return facts, nil
	}
	root, err := service.gitText(ctx, canonical, "rev-parse", "--show-toplevel")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return checkpoint.RecoveryWorktreeFacts{}, ctxErr
		}
		return facts, nil
	}
	root, err = canonicalDirectory(root)
	if err != nil || !samePath(root, canonical) {
		return facts, nil
	}
	facts.Owned = binding.TaskID == snapshot.TaskID &&
		binding.RepositoryID == snapshot.RepositoryID

	head, err := service.gitText(ctx, canonical, "rev-parse", "--verify", "HEAD")
	if err != nil || validateObjectID(head) != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return checkpoint.RecoveryWorktreeFacts{}, ctxErr
		}
		return facts, nil
	}
	facts.HeadRevision = head
	status, err := service.runner.Run(
		ctx,
		canonical,
		"git",
		"status",
		"--porcelain=v1",
		"-z",
		"--untracked-files=all",
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return checkpoint.RecoveryWorktreeFacts{}, ctxErr
		}
		return facts, nil
	}
	paths := parsePorcelainPaths(string(status.Stdout))
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if len(paths) > 2048 {
		facts.Owned = false
		return facts, nil
	}
	hashes, err := hashCheckpointDirtyFiles(ctx, binding, paths)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return checkpoint.RecoveryWorktreeFacts{}, ctxErr
		}
		facts.Owned = false
		return facts, nil
	}
	facts.DirtyFiles = hashes
	facts.UnresolvedGitOperations = service.observeUnresolvedGitOperations(
		ctx,
		canonical,
	)
	diff, err := service.observeRecoveryTaskDiff(
		ctx,
		binding,
		head,
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return checkpoint.RecoveryWorktreeFacts{}, ctxErr
		}
		return facts, nil
	}
	facts.DiffSHA256 = diff.Identity
	return facts, nil
}

func (service *Service) observeRecoveryTaskDiff(
	ctx context.Context,
	binding storage.WorktreeBinding,
	head string,
) (TaskDiff, error) {
	environment, cleanup, err := service.alternateDiffIndex(
		ctx,
		binding.WorktreePath,
	)
	if err != nil {
		return TaskDiff{}, err
	}
	defer cleanup()
	tracked, err := service.runWithEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"diff",
		"--binary",
		"--no-color",
		"--no-ext-diff",
		"-M",
		binding.BaseRevision,
		"--",
	)
	if err != nil {
		return TaskDiff{}, err
	}
	numstat, err := service.runWithEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"diff",
		"--numstat",
		"-z",
		"-M",
		binding.BaseRevision,
		"--",
	)
	if err != nil {
		return TaskDiff{}, err
	}
	files, err := parseNumstat(numstat.Stdout)
	if err != nil {
		return TaskDiff{}, err
	}
	slices.SortFunc(files, func(left, right DiffFile) int {
		return strings.Compare(left.Path, right.Path)
	})
	for index := range files {
		file := &files[index]
		if file.Category != "" {
			continue
		}
		content, _ := os.ReadFile(filepath.Join(
			binding.WorktreePath,
			filepath.FromSlash(file.Path),
		))
		if len(content) == 0 && file.DeletedLines != 0 {
			baseContent, showErr := service.runner.Run(
				ctx,
				binding.WorktreePath,
				"git",
				"show",
				binding.BaseRevision+":"+file.Path,
			)
			if showErr == nil {
				content = baseContent.Stdout
			}
		}
		file.Category = classifyDiffPath(file.Path, content)
	}
	diff := TaskDiff{
		TaskID: binding.TaskID, BaseRevision: binding.BaseRevision,
		HeadRevision: head, UnifiedDiff: string(tracked.Stdout), Files: files,
	}
	diff.Identity = computeDiffIdentity(diff)
	return diff, nil
}

func (service *Service) observeUnresolvedGitOperations(
	ctx context.Context,
	worktreePath string,
) []string {
	markers := []struct {
		name string
		path string
	}{
		{name: "bisect", path: "BISECT_LOG"},
		{name: "cherry-pick", path: "CHERRY_PICK_HEAD"},
		{name: "merge", path: "MERGE_HEAD"},
		{name: "rebase", path: "rebase-apply"},
		{name: "rebase", path: "rebase-merge"},
		{name: "revert", path: "REVERT_HEAD"},
		{name: "sequencer", path: "sequencer/todo"},
	}
	var operations []string
	for _, marker := range markers {
		resolved, err := service.gitText(
			ctx,
			worktreePath,
			"rev-parse",
			"--git-path",
			marker.path,
		)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(worktreePath, resolved)
		}
		if _, err := os.Stat(resolved); err == nil {
			operations = append(operations, marker.name)
		}
	}
	slices.Sort(operations)
	return slices.Compact(operations)
}

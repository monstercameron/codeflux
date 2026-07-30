package coordinator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
)

type WorktreeRecoveryStore interface {
	ListActiveWorktreeBindings(context.Context, int) ([]storage.WorktreeBinding, error)
	MarkWorktreeRecoveryRequired(
		context.Context,
		domain.TaskID,
		uint64,
		string,
	) (storage.WorktreeBinding, error)
}

type TaskRunRecoveryStore interface {
	RecoverUnownedTaskRuns(
		context.Context,
	) ([]storage.UnownedTaskRunRecoveryCandidate, error)
}

func RecoverUnownedTaskRuns(
	ctx context.Context,
	store TaskRunRecoveryStore,
) ([]storage.UnownedTaskRunRecoveryCandidate, error) {
	if store == nil {
		return nil, errors.New("task run recovery store is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return store.RecoverUnownedTaskRuns(ctx)
}

func RecoverInvalidWorktrees(
	ctx context.Context,
	store WorktreeRecoveryStore,
	runner gitwork.CommandRunner,
) ([]storage.WorktreeRecoveryCandidate, error) {
	if store == nil || runner == nil {
		return nil, errors.New("worktree recovery store and command runner are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bindings, err := store.ListActiveWorktreeBindings(ctx, 1000)
	if err != nil {
		return nil, err
	}
	var candidates []storage.WorktreeRecoveryCandidate
	for _, binding := range bindings {
		reason, err := inspectWorktreeBinding(ctx, binding, runner)
		if err != nil {
			return candidates, err
		}
		if reason == "" {
			continue
		}
		recovered, err := store.MarkWorktreeRecoveryRequired(
			ctx, binding.TaskID, binding.Revision, reason,
		)
		if err != nil {
			return candidates, err
		}
		candidates = append(candidates, storage.WorktreeRecoveryCandidate{
			Binding: recovered, Reason: reason,
		})
	}
	return candidates, nil
}

func inspectWorktreeBinding(
	ctx context.Context,
	binding storage.WorktreeBinding,
	runner gitwork.CommandRunner,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(binding.WorktreePath) ||
		filepath.Dir(filepath.Clean(binding.WorktreePath)) == filepath.Clean(binding.WorktreePath) {
		return "task worktree path is no longer a safe absolute non-root path", nil
	}
	info, err := os.Stat(binding.WorktreePath)
	if err != nil || !info.IsDir() {
		return "task worktree is missing or inaccessible", nil
	}
	canonical, err := filepath.EvalSymlinks(binding.WorktreePath)
	if err != nil {
		return "task worktree canonical path cannot be resolved", nil
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "task worktree canonical path cannot be resolved", nil
	}
	canonical = filepath.Clean(canonical)
	if !sameFilesystemPath(canonical, binding.WorktreePath) {
		return "task worktree moved or its canonical path diverged", nil
	}
	root, err := gitRecoveryText(
		ctx, runner, canonical, "rev-parse", "--show-toplevel",
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "task worktree is no longer a valid Git worktree", nil
	}
	rootCanonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "task worktree Git root cannot be resolved", nil
	}
	rootCanonical, err = filepath.Abs(rootCanonical)
	if err != nil {
		return "task worktree Git root cannot be resolved", nil
	}
	if !sameFilesystemPath(canonical, rootCanonical) {
		return "task worktree moved or its Git root diverged", nil
	}
	branch, err := gitRecoveryText(
		ctx, runner, canonical, "symbolic-ref", "--short", "HEAD",
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "task worktree branch diverged from durable metadata", nil
	}
	if branch != binding.BranchName {
		return "task worktree branch diverged from durable metadata", nil
	}
	head, err := gitRecoveryText(
		ctx, runner, canonical, "rev-parse", "--verify", "HEAD",
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "task worktree HEAD diverged from durable metadata", nil
	}
	if head != binding.HeadRevision {
		return "task worktree HEAD diverged from durable metadata", nil
	}
	return "", nil
}

func gitRecoveryText(
	ctx context.Context,
	runner gitwork.CommandRunner,
	directory string,
	arguments ...string,
) (string, error) {
	result, err := runner.Run(ctx, directory, "git", arguments...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func sameFilesystemPath(left, right string) bool {
	relative, err := filepath.Rel(filepath.Clean(left), filepath.Clean(right))
	return err == nil && relative == "."
}

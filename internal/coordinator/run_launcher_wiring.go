package coordinator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
)

// buildRunLauncher wires the launcher that turns a started run into a running
// worker.
//
// A coordinator that owns its own workers gets a real launcher. One driven by
// an external worker controller gets a launcher that refuses by name, because
// the alternative — no launcher, and a start path that silently records a run —
// is the failure this whole wiring exists to remove.
func buildRunLauncher(
	options ApplicationOptions,
	repositories runLauncherRepositories,
	application *Application,
) (RunLauncher, error) {
	if application.runtime == nil {
		return unavailableRunLauncher{
			reason: "this coordinator does not own worker processes, so it cannot start a " +
				"run; the configured worker controller owns execution",
		}, nil
	}

	root := strings.TrimSpace(options.WorktreeRoot)
	if root == "" {
		// Beside the database, because the worktrees and the rows describing
		// them are one unit: moving a database without its worktrees leaves
		// every binding pointing at nothing.
		root = filepath.Join(filepath.Dir(options.DatabasePath), "worktrees")
	}
	worktrees, err := gitwork.NewService(root, repositories, gitwork.ExecRunner{}, nil)
	if err != nil {
		return nil, fmt.Errorf("prepare the task worktree root: %w", err)
	}
	// A launched run is the path that actually edits a repository, so an
	// unbound recorder here means the edits with the most to answer for are
	// the ones with no durable record.
	launcherEditEvents, err := gitwork.NewStorageEditEventRecorder(repositories)
	if err != nil {
		return nil, fmt.Errorf("prepare the edit event recorder: %w", err)
	}
	worktrees.SetEditEventRecorder(launcherEditEvents)

	executable := strings.TrimSpace(options.WorkerExecutable)
	if executable == "" {
		executable, err = DefaultWorkerExecutable()
		if err != nil {
			return nil, err
		}
	}
	return NewTaskRunLauncher(
		repositories,
		worktrees,
		application.runtime,
		application.scheduler,
		executable,
		application.Address,
		options.Random,
	)
}

// runLauncherRepositories is the storage surface the launcher and its worktree
// service need together.
type runLauncherRepositories interface {
	taskRunLauncherStore
	gitwork.BindingRepository
	// A launched run's mediated edits are recorded in the ordered task
	// journal, so the launcher's store must be able to append to it.
	gitwork.TaskEventAppender
}

// unavailableRunLauncher refuses to start a run, by name.
//
// It exists so an application configured without its own worker processes
// still fails loudly at the moment somebody presses start, rather than
// reporting success and doing nothing.
type unavailableRunLauncher struct{ reason string }

func (launcher unavailableRunLauncher) Launch(
	context.Context, domain.TaskID, domain.RunID, uint64, string,
) error {
	return errors.New(launcher.reason)
}

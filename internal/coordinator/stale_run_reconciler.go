package coordinator

import (
	"context"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// StaleRunReconciler gives an ending to runs whose process is gone.
//
// A run that returns without recording an ending is caught by ensureTerminal.
// This is the case that cannot be: the process died, was killed, or lost its
// database mid-transition, and there was no return for a defer to run on. The
// task sits marked running with nothing behind it — invisible in exactly the
// same way, arrived at from the outside.
//
// It never resumes anything. Restarting work that may have been half-done, on a
// provider that may have been the reason it died, is a decision with a bill
// attached and it belongs to a person. What this does is make the task
// findable: recovery-required, worktree untouched, reason recorded.
type StaleRunReconciler struct {
	repositories *storage.Repositories
	// after is how long a task may sit unchanged before it counts as stale.
	//
	// Generously long. A run legitimately spends minutes on one model call, and
	// reconciling a live run out from under itself would be a far worse defect
	// than leaving a dead one for another cycle.
	after time.Duration
}

// NewStaleRunReconciler builds one with a default staleness window.
func NewStaleRunReconciler(repositories *storage.Repositories) *StaleRunReconciler {
	return &StaleRunReconciler{repositories: repositories, after: time.Hour}
}

// SetStalenessWindow overrides how long a task may sit unchanged.
func (reconciler *StaleRunReconciler) SetStalenessWindow(after time.Duration) {
	if after > 0 {
		reconciler.after = after
	}
}

// Reconcile marks every stale active task as needing recovery, and reports how
// many it moved.
//
// Errors on individual tasks are skipped rather than returned: one task whose
// revision moved under this pass is a task somebody else is handling, which is
// the outcome this wants anyway.
func (reconciler *StaleRunReconciler) Reconcile(ctx context.Context) (int, error) {
	if reconciler == nil || reconciler.repositories == nil {
		return 0, nil
	}
	stale, err := reconciler.repositories.ListStaleRunningTasks(
		ctx, reconciler.after, 100)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, task := range stale {
		eventID, idErr := domain.NewEventID()
		if idErr != nil {
			continue
		}
		if _, err := reconciler.repositories.TransitionTask(
			ctx, storage.TransitionTask{
				EventID:          eventID,
				TaskID:           task.TaskID,
				ExpectedRevision: task.Revision,
				From:             task.State,
				To:               domain.TaskStateRecoveryRequired,
				IdempotencyKey: agentExecutionKey(
					"stale-run-reconciled-", task.TaskID.String()),
			},
		); err != nil {
			continue
		}
		moved++
		tracef("recover", "task %s sat %s since %s with no process behind it; "+
			"marked %s", task.TaskID, task.State,
			task.UpdatedAt.Format(time.RFC3339), domain.TaskStateRecoveryRequired)
	}
	return moved, nil
}

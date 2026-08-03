package coordinator

import (
	"context"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
)

// finalization is what a run's completion attempt actually decided.
//
// It exists because the alternative was a function that returned nothing and
// had fifteen bare returns in it. Every one of those was a real decision — the
// evidence did not resolve, the revision could not be read, a required stage
// did not hold — and every one of them left the task in `running` with no
// record that anything had concluded. The ladder then waited for a terminal
// state that nothing was ever going to write.
//
// A silent non-completion is the worst outcome available: the run has finished
// working, knows why it cannot finish formally, and says so to nobody.
type finalization struct {
	// Terminal is whether the task and run reached a state that will not
	// change without someone acting. Anything else must be dispositioned by the
	// caller before it returns.
	Terminal bool
	// Reason is why, in the words a person would use.
	Reason string
	// TaskState is what the task was moved to, when this moved it.
	TaskState domain.TaskState
}

// finaliseNonTerminalRun gives an ending to a run that produced none.
//
// The lifecycle adapter already fails a task whose Run returned an error. What
// nothing covered was a Run that returned *nil* with work still owed: it had
// finished, the task was still `running`, and no code anywhere was going to
// change that. Rung 3 reached its verified checkpoint at 60 seconds, finished
// its pipeline checks at 71, and then sat in `running` until the ladder's own
// timeout.
//
// Conditional on the task still being running, for the same reason the adapter
// is: a run that recorded a specific outcome of its own has given a better
// account than this generic one, and overwriting it would lose that.
func (execution *AgentExecution) finaliseNonTerminalRun(
	ctx context.Context,
	scope agentScope,
	taskID domain.TaskID,
	verified bool,
	reason string,
) finalization {
	if execution == nil || execution.repositories == nil {
		return finalization{Reason: reason}
	}
	task, err := execution.repositories.GetTask(ctx, taskID)
	if err != nil {
		return finalization{Reason: reason}
	}
	if task.State != domain.TaskStateRunning &&
		task.State != domain.TaskStateValidating {
		// Something already gave it an ending. That account is better.
		return finalization{Terminal: true, Reason: reason, TaskState: task.State}
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		return finalization{Reason: reason}
	}
	// A run that verified something and a run that verified nothing are
	// different endings and must not be recorded the same way. Both are
	// terminal; only one of them is work somebody can pick up.
	ending := domain.TaskStateFailed
	moved, err := execution.repositories.TransitionTask(ctx, storage.TransitionTask{
		EventID:          eventID,
		TaskID:           taskID,
		ExpectedRevision: task.Revision,
		From:             task.State,
		To:               ending,
		IdempotencyKey: agentExecutionKey(
			"agent-run-not-completed-", taskID.String()),
	})
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run could not be given an ending, so it stays running: "+
				err.Error())
		return finalization{Reason: reason}
	}
	_ = moved
	tracef("final", "task moved to %s — %s (verified work exists: %t)",
		ending, reason, verified)
	return finalization{Terminal: true, Reason: reason, TaskState: ending}
}

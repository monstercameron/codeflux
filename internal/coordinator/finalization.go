package coordinator

import (
	"context"
	"time"

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
	// floorHeld is whether the tree that exists builds, passes its tests and
	// reproduces the acceptance examples.
	floorHeld bool,
	// recoverable is whether the run stopped for a reason outside the work —
	// a provider that went away, a deadline — leaving a draft worth resuming.
	recoverable bool,
	reason string,
) finalization {
	if execution == nil || execution.repositories == nil {
		return finalization{Reason: reason}
	}
	// Finalisation runs on its own time, not on the provider's.
	//
	// The context that timed out waiting for a model is the same context this
	// would use to write the ending, so a run that died of a provider timeout
	// could not record that it had. The cancellation still decides what the
	// ending is; it must not prevent the ending being written down.
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

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
	//
	// failed means the work is bad. A run holding a revision that compiles,
	// passes its tests and reproduces the acceptance examples, with some
	// research obligation unmet, does not hold bad work — it holds usable work
	// and an unfinished question. Ladder rungs 5 and 6 both ended "failed"
	// while their programs were correct on disk, which is the record
	// contradicting the thing it describes.
	//
	// paused is the closest true statement the vocabulary has: the run stopped,
	// nothing is wrong with what it produced, and a person decides what happens
	// next. It is reached from running, which failed also is, so this is a
	// choice between two legal endings rather than a new one.
	//
	// recovery-required is the third: a draft nobody could check because the
	// machinery went away. That is not bad work, it is unfinished work, and
	// calling it failed loses the distinction that decides whether anybody
	// looks at it again. It is also the only ending that says "the worktree is
	// worth keeping", which is exactly true here and false of a real failure.
	ending := domain.TaskStateFailed
	switch {
	case floorHeld:
		ending = domain.TaskStatePaused
	case recoverable:
		ending = domain.TaskStateRecoveryRequired
	}
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
	tracef("final", "task moved to %s — %s (the tree passes the completion "+
		"floor: %t)", ending, reason, floorHeld)
	return finalization{Terminal: true, Reason: reason, TaskState: ending}
}

// ensureTerminal is the last word: a run may not leave its task active.
//
// Deferred from Run, so it holds for every path out — the ones that return an
// error, the ones that panic past a recover, and the ones somebody adds later
// without reading this file. It does nothing when an ending was already
// recorded, which is the ordinary case; what it catches is the path that
// forgot.
//
// recovery-required rather than failed, deliberately. A run that reached the
// end without recording an outcome has produced work nobody has judged, and the
// two endings say different things about whether the worktree is worth keeping.
func (execution *AgentExecution) ensureTerminal(
	ctx context.Context,
	scope agentScope,
	taskID domain.TaskID,
) {
	if execution == nil || execution.repositories == nil {
		return
	}
	// Its own context, for the same reason finaliseNonTerminalRun has one: the
	// path most likely to reach here without an ending is the one whose context
	// expired, and that context cannot be used to write the ending down.
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	task, err := execution.repositories.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	if task.State != domain.TaskStateRunning &&
		task.State != domain.TaskStateValidating {
		return
	}
	tracef("final", "the run returned with its task still %s; forcing %s",
		task.State, domain.TaskStateRecoveryRequired)
	eventID, err := domain.NewEventID()
	if err != nil {
		return
	}
	_, _ = execution.repositories.TransitionTask(ctx, storage.TransitionTask{
		EventID:          eventID,
		TaskID:           taskID,
		ExpectedRevision: task.Revision,
		From:             task.State,
		To:               domain.TaskStateRecoveryRequired,
		IdempotencyKey: agentExecutionKey(
			"agent-run-left-active-", taskID.String()),
	})
	execution.say(ctx, scope, events.KindMessageFinal,
		"This run returned without recording an ending, which is a defect in "+
			"the run rather than in the work. The task is marked as needing "+
			"recovery so the worktree is kept and somebody can pick it up.")
}

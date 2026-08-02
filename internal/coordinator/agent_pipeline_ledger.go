package coordinator

import (
	"context"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// pipelineLedger records how far one run actually got through the flow.
//
// The flow has thirty-two stages and this build performs a handful of them.
// Nothing forced that gap to be visible: a run recorded a plan, wrote a file,
// and reported success, and the record it left was indistinguishable from one
// that had also derived contracts, generated atoms against them, tested each
// atom before writing it, discharged composition obligations, measured branch
// coverage, and mutation-tested the result.
//
// So every stage is written down, and the ones nothing implements say so. The
// point is not to make the build look worse than it is; it is that "we did not
// check this" and "this passed" must never render the same way.
type pipelineLedger struct {
	repositories *storage.Repositories
	taskID       domain.TaskID
	runID        domain.RunID
	attempt      uint64
	// recorded remembers which stages this run has already spoken about, so
	// the closing sweep can fill in the rest without overwriting a real result
	// with a default one.
	recorded map[pipeline.Number]bool
}

// newPipelineLedger starts a ledger for one task attempt.
func newPipelineLedger(
	repositories *storage.Repositories,
	taskID domain.TaskID,
	runID domain.RunID,
) *pipelineLedger {
	return &pipelineLedger{
		repositories: repositories, taskID: taskID, runID: runID, attempt: 1,
		recorded: map[pipeline.Number]bool{},
	}
}

// record writes one stage outcome.
func (ledger *pipelineLedger) record(
	ctx context.Context,
	stage pipeline.Number,
	state pipeline.State,
	detail string,
	evidence map[string]any,
) {
	if ledger == nil || ledger.repositories == nil {
		return
	}
	runID := ledger.runID
	if _, err := ledger.repositories.RecordPipelineStageResult(
		ctx, storage.RecordPipelineStage{
			TaskID: ledger.taskID, RunID: &runID, Attempt: ledger.attempt,
			Stage: stage, State: state, DetailRedacted: detail,
			Evidence: evidence,
		},
	); err != nil {
		// A ledger that cannot be written must not stop the work it is
		// describing. The gap shows up as a missing stage row, which is itself
		// readable as "this run's record is incomplete".
		return
	}
	ledger.recorded[stage] = true
}

// satisfied records a stage that held, with what it produced.
func (ledger *pipelineLedger) satisfied(
	ctx context.Context,
	stage pipeline.Number,
	detail string,
	evidence map[string]any,
) {
	if evidence == nil {
		evidence = map[string]any{}
	}
	ledger.record(ctx, stage, pipeline.StateSatisfied, detail, evidence)
}

// failed records a stage whose gate did not hold.
func (ledger *pipelineLedger) failed(
	ctx context.Context,
	stage pipeline.Number,
	detail string,
) {
	ledger.record(ctx, stage, pipeline.StateFailed, detail, nil)
}

// close fills in every stage this run never spoke about.
//
// A stage the product cannot perform is recorded as not implemented rather
// than left absent, because an absent row reads as an oversight in the record
// while a present one is a statement about this build. The distinction is the
// whole reason the ledger exists.
func (ledger *pipelineLedger) close(ctx context.Context) {
	if ledger == nil {
		return
	}
	for _, stage := range pipeline.Flow {
		if ledger.recorded[stage.Number] {
			continue
		}
		ledger.record(ctx, stage.Number, pipeline.StateNotImplemented,
			"no part of this build performs this stage", nil)
	}
}

// require records a stage and reports whether the run may go on.
//
// This is the difference between a ledger and a gate. Recording that a stage
// failed and then proceeding anyway produces an accurate history of an
// unjustified result: every later stage is built on something known to be
// broken, and the final claim inherits that without saying so. A gate makes
// the failure stop the thing it invalidates.
func (ledger *pipelineLedger) require(
	ctx context.Context,
	stage pipeline.Number,
	held bool,
	detail string,
	evidence map[string]any,
) bool {
	if held {
		ledger.satisfied(ctx, stage, detail, evidence)
		return true
	}
	ledger.failed(ctx, stage, detail)
	return false
}

// blocked records a stage that could not run because something before it did
// not hold.
//
// It is distinct from failed: the stage itself was never given a fair chance,
// and recording it as a failure would blame the wrong thing.
func (ledger *pipelineLedger) blocked(
	ctx context.Context,
	stage pipeline.Number,
	detail string,
) {
	ledger.record(ctx, stage, pipeline.StateBlocked, detail, nil)
}

// decide records the result of one performed check.
//
// It exists so a check can report three things rather than two. A stage this
// run had no need of is skipped, not failed: a program with no parsing
// boundary has not failed fuzzing, and recording it as a failure would make
// the ledger read as broken where it is merely inapplicable.
func (ledger *pipelineLedger) decide(
	ctx context.Context,
	stage pipeline.Number,
	outcome stageOutcome,
) {
	switch {
	case outcome.Skipped:
		ledger.record(ctx, stage, pipeline.StateSkipped, outcome.Detail, nil)
	case outcome.Held:
		ledger.satisfied(ctx, stage, outcome.Detail, outcome.Evidence)
	default:
		ledger.record(ctx, stage, pipeline.StateFailed, outcome.Detail,
			outcome.Evidence)
	}
}

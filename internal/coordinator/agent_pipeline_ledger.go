package coordinator

import (
	"context"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// pipelineLedger records how far one run actually got through the flow.
//
// The flow's length is pipeline.Flow's, and this build performs a handful of
// its stages. The number is not written out here: it was recorded as
// thirty-two in four places and the flow had grown past it, so the record
// described a shorter flow than the one it performed (PIPE-007).
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
	// stageStarted is when the stage now being decided began (PIPE-006).
	//
	// The flow is sequential, so a stage starts when the previous one was
	// recorded. That makes the recorded span the stage's elapsed wall clock,
	// which is what a reader wants from a ledger. It is not the check's own
	// processing time, and it will stop being elapsed-per-stage when PIPE-058a
	// runs stages concurrently; that change has to stamp starts at each check
	// instead.
	stageStarted time.Time
	// now supplies the clock, so a test can measure a span without sleeping.
	now func() time.Time
	// duplicates collects every second write attempted for a stage in this
	// attempt (PIPE-002).
	//
	// Stage storage is first-write-wins, so a duplicate used to be a silent
	// discard: the second verdict was computed, sent, and dropped, and the
	// ledger showed the first one with nothing to say a better answer had
	// existed. Collecting them turns that into something a test can assert on
	// and a reader can see, without failing a user's run over a defect in the
	// recording rather than in the work.
	duplicates []duplicateStageWrite
}

// duplicateStageWrite is one refused second write.
type duplicateStageWrite struct {
	Stage pipeline.Number
	State pipeline.State
}

// newPipelineLedger starts a ledger for one task attempt (PIPE-003).
//
// The attempt number is read from the ledger rather than pinned to one. It was
// hardcoded, so a task started a second time wrote every stage under attempt
// one, where the first run's rows already sat: ON CONFLICT DO NOTHING dropped
// the new run's whole record, and the interface then showed the first run's
// ledger under the newer run's identity.
//
// A ledger that cannot read its own history starts at attempt one rather than
// refusing to exist. That reproduces the old behaviour for that one run, which
// is worse than a correct number and much better than a run with no record.
func newPipelineLedger(
	ctx context.Context,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	runID domain.RunID,
) *pipelineLedger {
	attempt := uint64(1)
	if repositories != nil {
		if next, err := repositories.NextPipelineAttempt(ctx, taskID); err == nil {
			attempt = next
		}
	}
	ledger := &pipelineLedger{
		repositories: repositories, taskID: taskID, runID: runID,
		attempt:  attempt,
		recorded: map[pipeline.Number]bool{},
		now:      time.Now,
	}
	ledger.stageStarted = ledger.clock()
	return ledger
}

// clock reads the ledger's time source.
func (ledger *pipelineLedger) clock() time.Time {
	if ledger == nil || ledger.now == nil {
		return time.Now()
	}
	return ledger.now()
}

// currentAttempt reports the attempt this ledger records under, so a reader
// assembled later looks at the same rows the run wrote.
func (ledger *pipelineLedger) currentAttempt() uint64 {
	if ledger == nil || ledger.attempt == 0 {
		return 1
	}
	return ledger.attempt
}

// record writes one stage outcome.
func (ledger *pipelineLedger) record(
	ctx context.Context,
	stage pipeline.Number,
	state pipeline.State,
	detail string,
	evidence map[string]any,
) {
	if ledger == nil {
		return
	}
	// A stage speaks once per attempt. A second write is a programming error
	// in the caller, not a state change, and is refused here rather than sent
	// to storage to be silently dropped.
	//
	// The check precedes the storage guard deliberately: whether a caller
	// wrote twice is a fact about the caller, not about whether a database
	// happened to be attached.
	if ledger.recorded[stage] {
		ledger.duplicates = append(ledger.duplicates,
			duplicateStageWrite{Stage: stage, State: state})
		return
	}
	if ledger.repositories == nil {
		return
	}
	// A stage belongs to a task; the run is how it is attributed. A ledger
	// holding no run identity records against the task alone rather than
	// failing the whole write on a foreign key, which matches this type's
	// existing stance that a record it cannot write must not stop the work it
	// is describing.
	var runID *domain.RunID
	if !ledger.runID.IsZero() {
		owned := ledger.runID
		runID = &owned
	}
	started := ledger.stageStarted
	// The next stage begins when this one was recorded, whether or not the
	// write succeeded: a failed write must not make the following stage look
	// as though it started before this one.
	defer func() { ledger.stageStarted = ledger.clock() }()
	if _, err := ledger.repositories.RecordPipelineStageResult(
		ctx, storage.RecordPipelineStage{
			TaskID: ledger.taskID, RunID: runID, Attempt: ledger.attempt,
			Stage: stage, State: state, DetailRedacted: detail,
			Evidence: evidence, StartedAt: started,
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
		// A skipped stage keeps its evidence. It was dropped, so a stage that
		// declined to claim anything also lost what it had found — which is
		// the reading a skip most needs, because "not done" with findings
		// attached is a different fact from "not done" alone (PIPE-010).
		ledger.record(ctx, stage, pipeline.StateSkipped, outcome.Detail,
			outcome.Evidence)
	case outcome.Held:
		ledger.satisfied(ctx, stage, outcome.Detail, outcome.Evidence)
	default:
		ledger.record(ctx, stage, pipeline.StateFailed, outcome.Detail,
			outcome.Evidence)
	}
}

// duplicateWrites reports every second write this attempt refused.
//
// It exists so a test can assert a run records each stage exactly once. An
// empty result is the only correct outcome; a non-empty one names a stage the
// run tried to speak about twice.
func (ledger *pipelineLedger) duplicateWrites() []duplicateStageWrite {
	if ledger == nil {
		return nil
	}
	return append([]duplicateStageWrite(nil), ledger.duplicates...)
}

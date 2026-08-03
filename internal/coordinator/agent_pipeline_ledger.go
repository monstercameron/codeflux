package coordinator

import (
	"context"
	"sync"
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
	// history is what each stage of the current attempt decided, and perRun is
	// the subset decided before the attempt loop began. See beginAttempt.
	history map[pipeline.Number]decidedStage
	perRun  map[pipeline.Number]decidedStage
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
	// mutex guards recorded, duplicates, and stageStarted against concurrent
	// writes (PIPE-058a).
	//
	// A pipelineLedger was never called from more than one goroutine before
	// PIPE-058a: agent_execution.go's own calls are all sequential, and
	// examineStructure's were a straight line of ledger.decide calls. Once
	// examineStructure's own scheduler runs a wave of stages concurrently,
	// every one of those goroutines calls back into this same ledger, and
	// without this mutex two goroutines racing on ledger.recorded — a plain
	// map — would corrupt it rather than merely disagree about a duplicate.
	// The mutex is unconditional rather than only taken by the concurrent
	// path: guarding the sequential path too costs nothing when it is never
	// contended, and a caller does not have to know or prove which path it
	// is on to be safe.
	mutex sync.Mutex
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
		history:  map[pipeline.Number]decidedStage{},
		now:      time.Now,
	}
	ledger.stageStarted = ledger.clock()
	return ledger
}

// decidedStage is one stage outcome, kept so a later attempt can carry
// forward what did not change.
type decidedStage struct {
	state    pipeline.State
	detail   string
	evidence map[string]any
	started  time.Time
}

// sealPreAttemptStages marks everything decided so far as belonging to the run
// rather than to one attempt.
//
// Intake, clarification and planning happen once, before the attempt loop.
// They are as true on attempt six as on attempt one, and an attempt that
// omitted them would read as though the run had stopped performing them.
func (ledger *pipelineLedger) sealPreAttemptStages() {
	if ledger == nil {
		return
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	ledger.perRun = make(map[pipeline.Number]decidedStage, len(ledger.history))
	for stage, decided := range ledger.history {
		ledger.perRun[stage] = decided
	}
}

// beginAttempt moves the ledger onto the next attempt of the same run.
//
// A run's attempt loop revises the work and re-decides every stage it reaches,
// and until this existed the ledger was built once per run and every attempt
// wrote into attempt one. Storage keeps the first row for a (task, attempt,
// stage), so attempts after the first were discarded and the flow permanently
// described the first draft — a run that converged after six attempts still
// read as whatever failed on attempt one. The ledger was not merely
// incomplete, it disagreed with the outcome the same run reported.
//
// The stages sealed before the loop are re-recorded under the new number
// rather than left out. They did not happen again, but they are still true,
// and the alternative — recording them as never performed — was the first
// thing this change got wrong.
func (ledger *pipelineLedger) beginAttempt(ctx context.Context) {
	if ledger == nil {
		return
	}
	ledger.mutex.Lock()
	ledger.attempt++
	ledger.recorded = map[pipeline.Number]bool{}
	ledger.history = map[pipeline.Number]decidedStage{}
	carried := make(map[pipeline.Number]decidedStage, len(ledger.perRun))
	for stage, decided := range ledger.perRun {
		carried[stage] = decided
	}
	ledger.mutex.Unlock()

	ledger.stageStarted = ledger.clock()
	for stage, decided := range carried {
		ledger.recordMeasured(
			ctx, stage, decided.state, decided.detail,
			decided.evidence, decided.started,
		)
	}
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

// record writes one stage outcome, stamping its start as the moment the
// previous stage was recorded — correct only for a sequential caller, which
// is what every call site through this method is (PIPE-006's documented
// limitation). A caller that measures its own stage's start individually,
// because more than one stage can be "the previous one" at once, uses
// recordMeasured instead (PIPE-058a).
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
	ledger.mutex.Lock()
	started := ledger.stageStarted
	ledger.mutex.Unlock()
	// The next sequential stage begins when this one was recorded, whether or
	// not the write succeeded: a failed write must not make the following
	// stage look as though it started before this one. This only ever
	// advances the shared cursor a purely sequential caller reads; a
	// concurrent caller does not touch it at all (see recordMeasured).
	defer func() {
		ledger.mutex.Lock()
		ledger.stageStarted = ledger.clock()
		ledger.mutex.Unlock()
	}()
	ledger.recordMeasured(ctx, stage, state, detail, evidence, started)
}

// recordMeasured writes one stage outcome using a start time the caller
// already measured itself, rather than inferring one from the ledger's
// shared, sequential-only stageStarted cursor (PIPE-006/PIPE-058a).
//
// A zero started reports the stage unmeasured, the same honest default
// storage.RecordPipelineStage.StartedAt already documents: ElapsedMeasured
// comes out false and the duration reads as "not measured" rather than a
// fabricated instant.
func (ledger *pipelineLedger) recordMeasured(
	ctx context.Context,
	stage pipeline.Number,
	state pipeline.State,
	detail string,
	evidence map[string]any,
	started time.Time,
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
	// happened to be attached. It is also what makes concurrent writers safe
	// to have racing at all: two goroutines deciding the same stage at once
	// — which should never happen given a correct scheduler, but this is the
	// backstop if it did — cannot both win, because the check-and-mark below
	// happens under the same lock as every other caller's.
	ledger.mutex.Lock()
	if ledger.recorded[stage] {
		ledger.duplicates = append(ledger.duplicates,
			duplicateStageWrite{Stage: stage, State: state})
		ledger.mutex.Unlock()
		return
	}
	ledger.mutex.Unlock()
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
	traceStage(stage, state, detail)
	ledger.mutex.Lock()
	ledger.recorded[stage] = true
	ledger.history[stage] = decidedStage{
		state: state, detail: detail, evidence: evidence, started: started,
	}
	ledger.mutex.Unlock()
}

// recordDecision writes one stage outcome computed by
// pipeline.RunConcurrently, using the scheduler's own per-check measured
// span rather than the sequential stageStarted cursor (PIPE-058a).
//
// A stage RunConcurrently marked Blocked without ever calling its check
// (PIPE-063) carries Measured==false, which this passes through as a zero
// started time — an honestly unmeasured span, not a fabricated instant, for
// work that never ran.
func (ledger *pipelineLedger) recordDecision(
	ctx context.Context,
	decision pipeline.Decision,
) {
	started := decision.StartedAt
	if !decision.Measured {
		started = time.Time{}
	}
	ledger.recordMeasured(
		ctx, decision.Stage, decision.State, decision.Detail, decision.Evidence,
		started)
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
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	return append([]duplicateStageWrite(nil), ledger.duplicates...)
}

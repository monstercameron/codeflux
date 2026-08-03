package storage

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
)

// TestPipelineStageZeroDurationDoesNotImplyUnmeasured is the load-bearing
// check for PIPE-006b's second design decision.
//
// Duration() alone cannot separate "this stage's start was never tracked"
// from "this stage was tracked and genuinely took zero elapsed
// microseconds": both produce a zero span, because an untracked start is
// stored as equal to the finish time (see RecordPipelineStage.StartedAt).
// ElapsedMeasured exists to carry the difference explicitly rather than
// leaving a reader to infer it from that equality, which a genuinely
// zero-elapsed measured stage would satisfy too.
//
// This test discriminates a defective implementation that computed
// ElapsedMeasured by comparing StartedAt to FinishedAt (an inference that
// looks correct on the common case) instead of recording whether a start was
// actually accepted: such an implementation would mark the fixed-clock case
// below as unmeasured, and this test would fail on that assertion while
// passing on every other one.
func TestPipelineStageZeroDurationDoesNotImplyUnmeasured(t *testing.T) {
	repositories, task := createTaskFixture(t, 9700)
	fixedClock := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	repositories.now = func() time.Time { return fixedClock }

	measuredZero, err := repositories.RecordPipelineStageResult(
		t.Context(),
		RecordPipelineStage{
			TaskID: task.ID, Attempt: 1, Stage: pipeline.StageInstructions,
			State: pipeline.StateSatisfied, DetailRedacted: "tracked, zero span",
			Evidence: map[string]any{"fixture": true},
			// StartedAt equals what repositories.now() will report as the
			// finish time, so this stage's tracked start and its finish
			// collide on the same microsecond -- a genuine zero-elapsed span.
			StartedAt: fixedClock,
		},
	)
	if err != nil {
		t.Fatalf("record measured-zero stage: %v", err)
	}
	if !measuredZero.ElapsedMeasured {
		t.Error("a stage given an explicit tracked start was reported as unmeasured")
	}
	if got := measuredZero.Duration(); got != 0 {
		t.Errorf("measured-zero stage duration = %s, want 0", got)
	}

	untracked, err := repositories.RecordPipelineStageResult(
		t.Context(),
		RecordPipelineStage{
			TaskID: task.ID, Attempt: 1, Stage: pipeline.StageClarification,
			State: pipeline.StateSatisfied, DetailRedacted: "no start tracked",
			Evidence: map[string]any{"fixture": true},
			// StartedAt is left at its zero value: no start was tracked.
		},
	)
	if err != nil {
		t.Fatalf("record untracked stage: %v", err)
	}
	if untracked.ElapsedMeasured {
		t.Error("a stage given no tracked start was reported as measured")
	}
	if got := untracked.Duration(); got != 0 {
		t.Errorf("untracked stage duration = %s, want 0", got)
	}

	// Both records report the same zero Duration(); only ElapsedMeasured
	// tells them apart, which is the whole point of the field.
	if measuredZero.Duration() != untracked.Duration() {
		t.Fatalf("fixture invariant broken: durations differ (%s vs %s)",
			measuredZero.Duration(), untracked.Duration())
	}
	if measuredZero.ElapsedMeasured == untracked.ElapsedMeasured {
		t.Fatal("a tracked zero-elapsed stage and an untracked stage were not distinguished")
	}

	// The in-memory result RecordPipelineStageResult returns is not what a
	// later reader sees: everything else goes through the database. Re-reading
	// through ListPipelineStages closes that gap and would catch a defect that
	// persisted the wrong column value while still returning the right one in
	// memory.
	recorded, err := repositories.ListPipelineStages(t.Context(), task.ID, 1)
	if err != nil {
		t.Fatalf("list pipeline stages: %v", err)
	}
	byStage := map[pipeline.Number]PipelineStageRecord{}
	for _, record := range recorded {
		byStage[record.Stage] = record
	}
	if !byStage[pipeline.StageInstructions].ElapsedMeasured {
		t.Error("re-read: tracked zero-elapsed stage persisted as unmeasured")
	}
	if byStage[pipeline.StageClarification].ElapsedMeasured {
		t.Error("re-read: untracked stage persisted as measured")
	}
}

// TestPipelineStageMeasuredElapsedSpanIsPositive proves the ordinary case: a
// start tracked strictly before the finish reports a positive, measured span.
func TestPipelineStageMeasuredElapsedSpanIsPositive(t *testing.T) {
	repositories, task := createTaskFixture(t, 9710)
	finish := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	repositories.now = func() time.Time { return finish }

	record, err := repositories.RecordPipelineStageResult(
		t.Context(),
		RecordPipelineStage{
			TaskID: task.ID, Attempt: 1, Stage: pipeline.StageAtoms,
			State: pipeline.StateSatisfied, DetailRedacted: "took a while",
			Evidence:  map[string]any{"fixture": true},
			StartedAt: finish.Add(-90 * time.Second),
		},
	)
	if err != nil {
		t.Fatalf("record measured stage: %v", err)
	}
	if !record.ElapsedMeasured {
		t.Error("a stage with a start strictly before the finish was reported as unmeasured")
	}
	if got := record.Duration(); got != 90*time.Second {
		t.Errorf("measured stage duration = %s, want 1m30s", got)
	}
}

// TestPipelineStageListRoundTripsElapsedMeasured proves ListPipelineStages
// reads the same ElapsedMeasured signal back that RecordPipelineStageResult
// wrote, rather than the read path silently dropping the column.
func TestPipelineStageListRoundTripsElapsedMeasured(t *testing.T) {
	repositories, task := createTaskFixture(t, 9720)
	finish := time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)
	repositories.now = func() time.Time { return finish }

	if _, err := repositories.RecordPipelineStageResult(
		t.Context(),
		RecordPipelineStage{
			TaskID: task.ID, Attempt: 1, Stage: pipeline.StageInstructions,
			State: pipeline.StateSatisfied, DetailRedacted: "measured",
			Evidence:  map[string]any{"fixture": true},
			StartedAt: finish.Add(-1 * time.Minute),
		},
	); err != nil {
		t.Fatalf("record measured stage: %v", err)
	}
	if _, err := repositories.RecordPipelineStageResult(
		t.Context(),
		RecordPipelineStage{
			TaskID: task.ID, Attempt: 1, Stage: pipeline.StageClarification,
			State: pipeline.StateSkipped, DetailRedacted: "unmeasured",
		},
	); err != nil {
		t.Fatalf("record unmeasured stage: %v", err)
	}

	recorded, err := repositories.ListPipelineStages(t.Context(), task.ID, 1)
	if err != nil {
		t.Fatalf("list pipeline stages: %v", err)
	}
	if len(recorded) != 2 {
		t.Fatalf("recorded stages = %d, want 2", len(recorded))
	}
	if recorded[0].Stage != pipeline.StageInstructions || !recorded[0].ElapsedMeasured {
		t.Errorf("instructions stage = %+v, want elapsed_measured=true", recorded[0])
	}
	if recorded[1].Stage != pipeline.StageClarification || recorded[1].ElapsedMeasured {
		t.Errorf("clarification stage = %+v, want elapsed_measured=false", recorded[1])
	}
}

// TestPipelineStageListReadBoundDoesNotClipTheFullFlow proves the defensive
// LIMIT added to ListPipelineStages does not truncate a real attempt that
// recorded every stage of the flow -- the read bound must never turn into a
// silent data loss for ordinary use.
func TestPipelineStageListReadBoundDoesNotClipTheFullFlow(t *testing.T) {
	repositories, task := createTaskFixture(t, 9730)
	clock := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	repositories.now = func() time.Time { return clock }

	for _, stage := range pipeline.Flow {
		if _, err := repositories.RecordPipelineStageResult(
			t.Context(),
			RecordPipelineStage{
				TaskID: task.ID, Attempt: 1, Stage: stage.Number,
				State: pipeline.StateNotImplemented, DetailRedacted: "fixture sweep",
			},
		); err != nil {
			t.Fatalf("record stage %d: %v", stage.Number, err)
		}
		clock = clock.Add(time.Second)
	}

	recorded, err := repositories.ListPipelineStages(t.Context(), task.ID, 1)
	if err != nil {
		t.Fatalf("list pipeline stages: %v", err)
	}
	if len(recorded) != len(pipeline.Flow) {
		t.Fatalf("recorded stages = %d, want the full flow's %d",
			len(recorded), len(pipeline.Flow))
	}
}

// TestPipelineStageListDefaultsAttemptToOne proves the read side's attempt
// default (PIPE-003's convention) matches the write side's, so a caller that
// omits an attempt reads the ledger it expects rather than an empty one.
func TestPipelineStageListDefaultsAttemptToOne(t *testing.T) {
	repositories, task := createTaskFixture(t, 9740)

	if _, err := repositories.RecordPipelineStageResult(
		t.Context(),
		RecordPipelineStage{
			TaskID: task.ID, Attempt: 1, Stage: pipeline.StageInstructions,
			State: pipeline.StateSatisfied, DetailRedacted: "default attempt",
			Evidence: map[string]any{"fixture": true},
		},
	); err != nil {
		t.Fatalf("record stage: %v", err)
	}

	recorded, err := repositories.ListPipelineStages(t.Context(), task.ID, 0)
	if err != nil {
		t.Fatalf("list pipeline stages: %v", err)
	}
	if len(recorded) != 1 || recorded[0].Attempt != 1 {
		t.Fatalf("recorded = %+v, want one stage under attempt 1", recorded)
	}
}

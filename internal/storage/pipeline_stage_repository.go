package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
)

// RecordPipelineStage is one stage of the delivery flow, as it went.
type RecordPipelineStage struct {
	TaskID  domain.TaskID
	RunID   *domain.RunID
	Attempt uint64
	Stage   pipeline.Number
	State   pipeline.State
	// DetailRedacted says what happened in this run's particular case, where
	// the stage's gate says what has to hold in general.
	DetailRedacted string
	// Evidence is whatever the stage produced. It is structured rather than
	// prose so a later reader can compare two runs rather than read two
	// paragraphs.
	Evidence map[string]any
	// StartedAt is when this stage began (PIPE-006).
	//
	// Both timestamp columns used to receive the write time, so the schema
	// modelled a duration and no duration was recordable: every stage in every
	// run took zero. A zero value here still writes the finish time into both,
	// which keeps a caller that does not track starts honest rather than
	// inventing a span for it.
	StartedAt time.Time
}

// PipelineStageRecord is one recorded stage.
type PipelineStageRecord struct {
	TaskID         domain.TaskID
	Attempt        uint64
	Stage          pipeline.Number
	Name           string
	State          pipeline.State
	Gate           string
	DetailRedacted string
	EvidenceJSON   string
	// StartedAt and FinishedAt bound the stage. They are equal for a stage
	// recorded without a tracked start.
	StartedAt  time.Time
	FinishedAt time.Time
	// ElapsedMeasured reports whether StartedAt was actually tracked for this
	// stage, as opposed to defaulted to FinishedAt because no caller-supplied
	// start was accepted (PIPE-006b).
	//
	// StartedAt and FinishedAt alone cannot carry this distinction: an
	// untracked stage stores its finish time in both columns, which is
	// bit-for-bit identical to a genuinely measured stage that happened to
	// take zero elapsed microseconds. Duration() would report zero for both,
	// so a reader relying on it alone reads "not measured" as "instant". This
	// field is written once, at insert time, by the same code that decides
	// whether to accept the caller's start, so it is not subject to that
	// ambiguity.
	ElapsedMeasured bool
}

// Duration reports how long the stage took, or zero when no span was recorded.
//
// A zero result is ambiguous on its own: it means either that the span was
// never measured, or that it was measured and genuinely came out to zero
// elapsed microseconds. Callers that must tell those apart read
// ElapsedMeasured alongside this value rather than testing it against zero.
func (record PipelineStageRecord) Duration() time.Duration {
	if record.StartedAt.IsZero() || record.FinishedAt.IsZero() {
		return 0
	}
	return record.FinishedAt.Sub(record.StartedAt)
}

// RecordPipelineStageResult appends one stage outcome to the ledger.
//
// Every stage of the flow is recorded, including the ones this build cannot
// perform. A run that skipped two thirds of the flow and a run that satisfied
// all of it otherwise produce the same evidence — a green result and a written
// file — and the difference between them is the entire question a person is
// asking when they look at this record.
func (repositories *Repositories) RecordPipelineStageResult(
	ctx context.Context,
	input RecordPipelineStage,
) (PipelineStageRecord, error) {
	if repositories == nil || repositories.database == nil {
		return PipelineStageRecord{}, errors.New("repositories are unavailable")
	}
	stage, known := pipeline.StageByNumber(input.Stage)
	if !known {
		return PipelineStageRecord{}, fmt.Errorf(
			"stage %d is not part of the flow", input.Stage)
	}
	if !input.State.Valid() {
		return PipelineStageRecord{}, fmt.Errorf(
			"stage state %q is not one the ledger accepts", input.State)
	}
	if input.TaskID.IsZero() {
		return PipelineStageRecord{}, errors.New("a stage must belong to a task")
	}
	if input.Attempt == 0 {
		input.Attempt = 1
	}
	evidence := input.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return PipelineStageRecord{}, fmt.Errorf("encode stage evidence: %w", err)
	}
	detail := input.DetailRedacted
	if len(detail) > 4096 {
		detail = detail[:4096]
	}

	identity := digestOfStage(input.TaskID.String(), input.Attempt, input.Stage)
	_, micros := repositories.timestamp()
	started := micros
	// measured records whether a caller-supplied start was actually accepted,
	// which is the fact ElapsedMeasured exists to carry. It is decided here,
	// once, because this is the only place that knows why started ended up
	// equal to micros: a genuine zero-elapsed stage never reaches this branch.
	measured := false
	if !input.StartedAt.IsZero() {
		if candidate := input.StartedAt.UnixMicro(); candidate >= 0 && candidate <= micros {
			started = candidate
			measured = true
		}
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO pipeline_stage_records (
			id, task_id, run_id, attempt, stage_number, stage_name, state,
			gate_redacted, detail_redacted, evidence_json,
			started_at_unix_micros, finished_at_unix_micros, elapsed_measured
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (task_id, attempt, stage_number) DO NOTHING`,
		identity, input.TaskID, nullableRunID(input.RunID), input.Attempt,
		int(input.Stage), stage.Name, string(input.State), stage.Gate,
		detail, string(encoded), started, micros, boolInt(measured),
	); err != nil {
		return PipelineStageRecord{}, repositoryWriteError(
			"record pipeline stage", err)
	}
	return PipelineStageRecord{
		TaskID: input.TaskID, Attempt: input.Attempt, Stage: input.Stage,
		Name: stage.Name, State: input.State, Gate: stage.Gate,
		DetailRedacted: detail, EvidenceJSON: string(encoded),
		StartedAt:       time.UnixMicro(started).UTC(),
		FinishedAt:      time.UnixMicro(micros).UTC(),
		ElapsedMeasured: measured,
	}, nil
}

// pipelineStageReadLimit bounds one ledger read.
//
// The true bound is the schema's own CHECK on stage_number (1 to 64,
// widened by PIPE-007/migration 000031 to outlive the current thirty-eight
// stage flow), together with the UNIQUE(task_id, attempt, stage_number)
// constraint: no task attempt can ever accumulate more than 64 rows. This
// LIMIT is defense in depth against that invariant rather than the reason
// the read is bounded, so a client of this repository never has to reason
// about an unbounded scan even if the schema constraint were ever loosened.
const pipelineStageReadLimit = 128

// ListPipelineStages returns one task's flow in order.
func (repositories *Repositories) ListPipelineStages(
	ctx context.Context,
	taskID domain.TaskID,
	attempt uint64,
) ([]PipelineStageRecord, error) {
	if attempt == 0 {
		attempt = 1
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT task_id, attempt, stage_number, stage_name, state,
			gate_redacted, detail_redacted, evidence_json,
			started_at_unix_micros, finished_at_unix_micros, elapsed_measured
		 FROM pipeline_stage_records
		 WHERE task_id = ? AND attempt = ?
		 ORDER BY stage_number
		 LIMIT ?`,
		taskID, attempt, pipelineStageReadLimit,
	)
	if err != nil {
		return nil, classify("list pipeline stages", err)
	}
	defer func() { _ = rows.Close() }()
	var records []PipelineStageRecord
	for rows.Next() {
		var record PipelineStageRecord
		var stageNumber int
		var startedMicros, finishedMicros int64
		var measured int
		if err := rows.Scan(&record.TaskID, &record.Attempt, &stageNumber,
			&record.Name, &record.State, &record.Gate, &record.DetailRedacted,
			&record.EvidenceJSON, &startedMicros, &finishedMicros,
			&measured); err != nil {
			return nil, classify("scan pipeline stage", err)
		}
		record.Stage = pipeline.Number(stageNumber)
		record.StartedAt = time.UnixMicro(startedMicros).UTC()
		record.FinishedAt = time.UnixMicro(finishedMicros).UTC()
		record.ElapsedMeasured = measured != 0
		records = append(records, record)
	}
	return records, rows.Err()
}

// digestOfStage names one stage record deterministically.
//
// The identity is derived rather than minted so that recording the same stage
// of the same attempt twice collides on its own primary key instead of
// producing a second, contradictory answer to one question.
func digestOfStage(
	taskID string,
	attempt uint64,
	stage pipeline.Number,
) string {
	return fmt.Sprintf("stage:%s:%d:%02d", taskID, attempt, stage)
}

// NextPipelineAttempt returns the attempt number one task's next run should
// record under (PIPE-003).
//
// It is derived from the ledger rather than counted elsewhere, because the
// ledger is where the number is used and the two cannot then disagree. A task
// with no recorded stages is on attempt one.
//
// The read is not transactional with the writes that follow it. Two runs
// starting for one task at the same instant could both compute the same
// attempt, and the stage table's ON CONFLICT would then merge them. That is
// bounded by the coordinator's own rule of at most one active task per
// repository; a stricter guarantee needs the attempt minted inside the
// transaction that creates the run, which is where it should move if that rule
// ever relaxes.
func (repositories *Repositories) NextPipelineAttempt(
	ctx context.Context,
	taskID domain.TaskID,
) (uint64, error) {
	if taskID.IsZero() {
		return 0, errors.New("task ID must not be empty")
	}
	var highest uint64
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(attempt), 0) FROM pipeline_stage_records WHERE task_id = ?`,
		taskID,
	).Scan(&highest); err != nil {
		return 0, classify("read the highest recorded pipeline attempt", err)
	}
	return highest + 1, nil
}

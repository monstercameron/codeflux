package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO pipeline_stage_records (
			id, task_id, run_id, attempt, stage_number, stage_name, state,
			gate_redacted, detail_redacted, evidence_json,
			started_at_unix_micros, finished_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (task_id, attempt, stage_number) DO NOTHING`,
		identity, input.TaskID, nullableRunID(input.RunID), input.Attempt,
		int(input.Stage), stage.Name, string(input.State), stage.Gate,
		detail, string(encoded), micros, micros,
	); err != nil {
		return PipelineStageRecord{}, repositoryWriteError(
			"record pipeline stage", err)
	}
	return PipelineStageRecord{
		TaskID: input.TaskID, Attempt: input.Attempt, Stage: input.Stage,
		Name: stage.Name, State: input.State, Gate: stage.Gate,
		DetailRedacted: detail, EvidenceJSON: string(encoded),
	}, nil
}

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
			gate_redacted, detail_redacted, evidence_json
		 FROM pipeline_stage_records
		 WHERE task_id = ? AND attempt = ?
		 ORDER BY stage_number`,
		taskID, attempt,
	)
	if err != nil {
		return nil, classify("list pipeline stages", err)
	}
	defer func() { _ = rows.Close() }()
	var records []PipelineStageRecord
	for rows.Next() {
		var record PipelineStageRecord
		var stageNumber int
		if err := rows.Scan(&record.TaskID, &record.Attempt, &stageNumber,
			&record.Name, &record.State, &record.Gate, &record.DetailRedacted,
			&record.EvidenceJSON); err != nil {
			return nil, classify("scan pipeline stage", err)
		}
		record.Stage = pipeline.Number(stageNumber)
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

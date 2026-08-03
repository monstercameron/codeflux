package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// PipelineStageLedgerStore reads recorded pipeline stage outcomes.
//
// It is the one seam PIPE-006b opens between the pipeline ledger, which only
// internal/coordinator writes (see agent_pipeline_ledger.go), and
// internal/transport, which must not import internal/storage directly. The
// method signature matches storage.Repositories.ListPipelineStages exactly,
// so the production dependency is satisfied without an adapter.
type PipelineStageLedgerStore interface {
	ListPipelineStages(
		ctx context.Context,
		taskID domain.TaskID,
		attempt uint64,
	) ([]storage.PipelineStageRecord, error)
}

// PipelineStageReadService adapts the pipeline stage ledger to
// transport.PipelineStageApplication, owning the one business decision this
// read makes: an attempt of zero resolves to attempt one, matching the
// ledger's own PIPE-003 convention (storage.NextPipelineAttempt starts
// numbering at one).
type PipelineStageReadService struct {
	store PipelineStageLedgerStore
}

// NewPipelineStageReadService validates the pipeline stage ledger dependency.
func NewPipelineStageReadService(store PipelineStageLedgerStore) (*PipelineStageReadService, error) {
	if store == nil {
		return nil, errors.New("pipeline stage ledger store is required")
	}
	return &PipelineStageReadService{store: store}, nil
}

// ListPipelineStages reads one task attempt's recorded flow and reports the
// attempt it actually read, so a caller that asked for attempt zero learns
// which attempt answered.
func (service *PipelineStageReadService) ListPipelineStages(
	ctx context.Context,
	query transport.PipelineStageQuery,
) ([]transport.PipelineStageSummary, uint64, error) {
	if query.TaskID.IsZero() {
		return nil, 0, errors.New("a pipeline stage ledger read needs a task")
	}
	attempt := query.Attempt
	if attempt == 0 {
		attempt = 1
	}
	records, err := service.store.ListPipelineStages(ctx, query.TaskID, attempt)
	if err != nil {
		return nil, 0, err
	}
	summaries := make([]transport.PipelineStageSummary, 0, len(records))
	for _, record := range records {
		summaries = append(summaries, transport.PipelineStageSummary{
			Number:          uint32(record.Stage),
			Name:            record.Name,
			State:           string(record.State),
			FinishedAt:      record.FinishedAt,
			ElapsedMeasured: record.ElapsedMeasured,
			Elapsed:         record.Duration(),
		})
	}
	return summaries, attempt, nil
}

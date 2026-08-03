package transport

import (
	"context"
	"errors"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PipelineStageSummary is one recorded pipeline stage, as the application
// layer reports it: enough to answer how far one task attempt got through the
// delivery flow and how long each stage took, without the stage's own gate
// text or evidence payload, which this read-only surface does not expose
// (PIPE-006b).
//
// ElapsedMeasured distinguishes a stage whose elapsed span was actually
// tracked from one that was not. The ledger stores an untracked start as
// equal to the finish, which is indistinguishable from a genuinely
// instantaneous stage unless this flag travels with Elapsed; a caller must
// read them together rather than treating a zero Elapsed as "instant".
type PipelineStageSummary struct {
	Number          uint32
	Name            string
	State           string
	FinishedAt      time.Time
	ElapsedMeasured bool
	Elapsed         time.Duration
}

// PipelineStageQuery bounds a ledger read to one task's one attempt.
//
// Attempt zero asks the application layer to resolve its own default
// (attempt one, matching storage.ListPipelineStages), not "the latest
// attempt"; this surface does not resolve that on a caller's behalf.
type PipelineStageQuery struct {
	TaskID  domain.TaskID
	Attempt uint64
}

// PipelineStageApplication reads the pipeline stage ledger a task attempt
// produced. It is read-only: nothing reachable from this interface writes the
// ledger, which stays owned by internal/coordinator's pipeline execution.
//
// ListPipelineStages returns the summaries in stage order together with the
// attempt they were actually read for, so a caller that sent zero can see
// which attempt the application layer resolved it to.
type PipelineStageApplication interface {
	ListPipelineStages(context.Context, PipelineStageQuery) ([]PipelineStageSummary, uint64, error)
}

// PipelineStageService exposes the pipeline stage ledger over the wire
// (PIPE-006b). It performs no pipeline logic of its own: authentication is
// enforced by the shared unary boundary, task identity is validated here, and
// every other decision -- including the attempt-zero default -- belongs to the
// application layer this service delegates to.
type PipelineStageService struct {
	codefluxv1.UnimplementedPipelineStageServiceServer
	stages PipelineStageApplication
}

// NewPipelineStageService validates the pipeline stage application dependency.
func NewPipelineStageService(stages PipelineStageApplication) (*PipelineStageService, error) {
	if stages == nil {
		return nil, errors.New("pipeline stage application is required")
	}
	return &PipelineStageService{stages: stages}, nil
}

// ListPipelineStages returns one task attempt's recorded flow.
func (service *PipelineStageService) ListPipelineStages(
	ctx context.Context,
	request *codefluxv1.ListPipelineStagesRequest,
) (*codefluxv1.ListPipelineStagesResponse, error) {
	if request == nil {
		return nil, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return nil, requestIdentityError("task_id", err)
	}
	summaries, attempt, err := service.stages.ListPipelineStages(ctx, PipelineStageQuery{
		TaskID: taskID, Attempt: request.GetAttempt(),
	})
	if err != nil {
		return nil, mapPipelineStageError(err)
	}
	response := &codefluxv1.ListPipelineStagesResponse{
		Attempt: attempt,
		Stages:  make([]*codefluxv1.PipelineStageView, 0, len(summaries)),
	}
	for _, summary := range summaries {
		view := &codefluxv1.PipelineStageView{
			StageNumber:     summary.Number,
			StageName:       summary.Name,
			State:           summary.State,
			ElapsedMeasured: summary.ElapsedMeasured,
		}
		if !summary.FinishedAt.IsZero() {
			view.FinishedAt = timestamppb.New(summary.FinishedAt)
		}
		// elapsed is meaningful only when elapsed_measured is true; an
		// unmeasured stage's Elapsed is always zero already, but the field is
		// left unset rather than sent as an explicit zero so a client cannot
		// mistake wire absence for "measured, and it was zero".
		if summary.ElapsedMeasured {
			view.Elapsed = durationpb.New(summary.Elapsed)
		}
		response.Stages = append(response.Stages, view)
	}
	return response, nil
}

func mapPipelineStageError(err error) error {
	if err == nil {
		return nil
	}
	return status.Error(codes.Unavailable, "the pipeline stage ledger could not be read")
}

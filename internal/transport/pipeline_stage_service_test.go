package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type pipelineStageApplicationStub struct {
	query     PipelineStageQuery
	summaries []PipelineStageSummary
	attempt   uint64
	err       error
}

func (stub *pipelineStageApplicationStub) ListPipelineStages(
	_ context.Context,
	query PipelineStageQuery,
) ([]PipelineStageSummary, uint64, error) {
	stub.query = query
	return stub.summaries, stub.attempt, stub.err
}

func testPipelineTaskID(t *testing.T) domain.TaskID {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return taskID
}

// TestPipelineStageServiceListRequiresTaskID proves a request with no task
// identity is refused before it reaches the application layer.
func TestPipelineStageServiceListRequiresTaskID(t *testing.T) {
	application := &pipelineStageApplicationStub{}
	service, err := NewPipelineStageService(application)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ListPipelineStages(
		context.Background(), &codefluxv1.ListPipelineStagesRequest{},
	)
	if err == nil {
		t.Fatal("a request with no task identity was accepted")
	}
	var validationErr *RequestValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want a RequestValidationError", err)
	}
	if application.query != (PipelineStageQuery{}) {
		t.Errorf("application was called with %+v despite invalid input", application.query)
	}
}

// TestPipelineStageServiceListDelegatesTaskAndAttempt proves the handler
// passes the request's task and attempt straight through without inventing a
// default of its own -- resolving attempt zero belongs to the application
// layer (internal/coordinator), not the transport handler.
func TestPipelineStageServiceListDelegatesTaskAndAttempt(t *testing.T) {
	taskID := testPipelineTaskID(t)
	taskProto, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	application := &pipelineStageApplicationStub{attempt: 1}
	service, err := NewPipelineStageService(application)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.ListPipelineStages(
		context.Background(),
		&codefluxv1.ListPipelineStagesRequest{TaskId: taskProto, Attempt: 0},
	)
	if err != nil {
		t.Fatalf("list pipeline stages: %v", err)
	}
	if application.query.TaskID != taskID {
		t.Errorf("delegated task = %v, want %v", application.query.TaskID, taskID)
	}
	if application.query.Attempt != 0 {
		t.Errorf("delegated attempt = %d, want the caller's own 0", application.query.Attempt)
	}
	if response.GetAttempt() != 1 {
		t.Errorf("response attempt = %d, want the resolved 1 the application reported",
			response.GetAttempt())
	}
}

// TestPipelineStageServiceListDistinguishesMeasuredFromUnmeasuredOnTheWire is
// the load-bearing check for PIPE-006b's wire-shape design decision: a client
// must not read a zero elapsed duration as "instant" when the span was never
// measured.
//
// It discriminates a handler that always sets the wire elapsed field (the
// natural mistake: forwarding summary.Elapsed unconditionally). Such a
// handler would send an explicit zero google.protobuf.Duration for the
// unmeasured stage, indistinguishable on the wire from a genuinely
// zero-elapsed measured one, and this test's assertion that Elapsed is nil
// for the unmeasured stage would fail against it.
func TestPipelineStageServiceListDistinguishesMeasuredFromUnmeasuredOnTheWire(t *testing.T) {
	taskID := testPipelineTaskID(t)
	taskProto, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	finished := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	application := &pipelineStageApplicationStub{
		attempt: 1,
		summaries: []PipelineStageSummary{
			{
				Number: 1, Name: "instructions", State: "satisfied",
				FinishedAt: finished, ElapsedMeasured: true,
				Elapsed: 90 * time.Second,
			},
			{
				Number: 2, Name: "clarification", State: "skipped",
				FinishedAt: finished, ElapsedMeasured: false,
				// Elapsed left at its zero value: nothing was measured.
			},
		},
	}
	service, err := NewPipelineStageService(application)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.ListPipelineStages(
		context.Background(),
		&codefluxv1.ListPipelineStagesRequest{TaskId: taskProto},
	)
	if err != nil {
		t.Fatalf("list pipeline stages: %v", err)
	}
	if len(response.GetStages()) != 2 {
		t.Fatalf("stages = %d, want 2", len(response.GetStages()))
	}

	measured := response.GetStages()[0]
	if !measured.GetElapsedMeasured() {
		t.Error("measured stage reported elapsed_measured=false")
	}
	if measured.GetElapsed() == nil || measured.GetElapsed().AsDuration() != 90*time.Second {
		t.Errorf("measured stage elapsed = %v, want 1m30s", measured.GetElapsed())
	}

	unmeasured := response.GetStages()[1]
	if unmeasured.GetElapsedMeasured() {
		t.Error("unmeasured stage reported elapsed_measured=true")
	}
	if unmeasured.Elapsed != nil {
		t.Errorf("unmeasured stage sent an explicit elapsed value %v, want the field left unset",
			unmeasured.Elapsed.AsDuration())
	}
}

// TestPipelineStageServiceListMapsApplicationErrorToUnavailable proves an
// application failure reaches the client as a safe status rather than a raw
// internal error.
func TestPipelineStageServiceListMapsApplicationErrorToUnavailable(t *testing.T) {
	taskID := testPipelineTaskID(t)
	taskProto, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	application := &pipelineStageApplicationStub{err: errors.New("database busy")}
	service, err := NewPipelineStageService(application)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ListPipelineStages(
		context.Background(),
		&codefluxv1.ListPipelineStagesRequest{TaskId: taskProto},
	)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable", status.Code(err))
	}
}

// TestNewPipelineStageServiceRejectsANilApplication proves the constructor
// refuses to build a service with no read path behind it.
func TestNewPipelineStageServiceRejectsANilApplication(t *testing.T) {
	if _, err := NewPipelineStageService(nil); err == nil {
		t.Fatal("a nil pipeline stage application was accepted")
	}
}

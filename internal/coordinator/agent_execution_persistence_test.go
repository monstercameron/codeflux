package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/storage"
)

func TestAgentExecutionPersistenceMapsRedactedRuntimeRecords(t *testing.T) {
	repository := &agentExecutionRepositoryStub{}
	persistence, err := NewAgentExecutionPersistence(repository)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	modelRequestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	start := agent.ToolStartRecord{
		TaskID: taskID, RunID: runID, PlanRevision: 3,
		PlanStepID: "step-001", Round: 2, RequestID: "model-call-7",
		ModelRequestID: modelRequestID, ToolName: executor.ToolApplyEdit,
		ToolSchemaVersion:     executor.ToolSchemaVersion,
		ArgumentsRedactedJSON: `{"content":"[REDACTED]","path":"main.go"}`,
		ArgumentsSHA256:       hex.EncodeToString(make([]byte, sha256.Size)),
		Authorization: agent.ToolAuthorization{
			DecisionID: "permission-decision-7", PolicyRevision: 9,
			PolicySHA256: hex.EncodeToString(make([]byte, sha256.Size)),
		},
	}
	if err := persistence.PersistToolStart(t.Context(), start); err != nil {
		t.Fatal(err)
	}
	transition := agent.PlanStepTransition{
		TaskID: taskID, RunID: runID, PlanRevision: 3,
		PlanStepID: "step-001", From: agent.StepPending,
		To: agent.StepInProgress, Reason: "approved tool action began",
		ToolRequestID: "model-call-7", ModelRequestID: modelRequestID,
	}
	if err := persistence.PersistPlanStepTransition(
		t.Context(),
		transition,
	); err != nil {
		t.Fatal(err)
	}
	result := executor.ToolResult{
		RequestID: "model-call-7", SchemaVersion: executor.ToolSchemaVersion,
		State: "succeeded", ExitCode: 0, Summary: "edit applied",
		StdoutRedacted: "[REDACTED]",
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if err := persistence.PersistToolResult(
		t.Context(),
		agent.ToolResultRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 3,
			PlanStepID: "step-001", Round: 2, RequestID: "model-call-7",
			Result: result, ResultSHA256: hex.EncodeToString(digest[:]),
		},
	); err != nil {
		t.Fatal(err)
	}

	if len(repository.order) != 3 ||
		repository.order[0] != "start" ||
		repository.order[1] != "transition" ||
		repository.order[2] != "result" {
		t.Fatalf("repository call order = %#v", repository.order)
	}
	request := repository.requests[0]
	if request.TaskID != taskID ||
		request.RunID != runID ||
		request.PlanRevision != 3 ||
		request.PlanStepID != "step-001" ||
		request.ModelRequestID != modelRequestID ||
		request.ToolName != string(executor.ToolApplyEdit) ||
		request.PermissionDecisionID == nil ||
		*request.PermissionDecisionID != "permission-decision-7" ||
		request.ArgumentsRedactedJSON != start.ArgumentsRedactedJSON {
		t.Fatalf("stored tool request = %#v", request)
	}
	storedTransition := repository.transitions[0]
	// The transition must name the tool request by the exact ID its own
	// ToolJournal stored it under (AUDIT-020): a transformed ID would
	// reference a row agent_tool_requests does not have under that key, and
	// the durable consistency trigger on agent_plan_step_transitions would
	// refuse it. request.ID is what PersistToolStart above actually stored
	// (record.RequestID, unchanged), so the transition must match it exactly.
	if storedTransition.From != storage.PlanStepPending ||
		storedTransition.To != storage.PlanStepInProgress ||
		storedTransition.ModelRequestID != modelRequestID ||
		storedTransition.ToolRequestID == nil ||
		*storedTransition.ToolRequestID != request.ID ||
		request.ID != start.RequestID {
		t.Fatalf("stored plan transition = %#v", storedTransition)
	}
	storedResult := repository.results[0]
	if storedResult.ToolRequestID != request.ID ||
		storedResult.State != storage.AgentToolResultSucceeded ||
		storedResult.ResultSHA256 != hex.EncodeToString(digest[:]) ||
		storedResult.ResultRedactedJSON != string(encoded) {
		t.Fatalf("stored tool result = %#v", storedResult)
	}
}

func TestAgentExecutionPersistenceRejectsMismatchedResultDigest(t *testing.T) {
	repository := &agentExecutionRepositoryStub{}
	persistence, err := NewAgentExecutionPersistence(repository)
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	err = persistence.PersistToolResult(
		context.Background(),
		agent.ToolResultRecord{
			TaskID: taskID, RunID: runID, PlanRevision: 1,
			PlanStepID: "step-001", RequestID: "call-1",
			Result: executor.ToolResult{
				RequestID: "call-1", SchemaVersion: executor.ToolSchemaVersion,
				State: "failed",
			},
			ResultSHA256: hex.EncodeToString(make([]byte, sha256.Size)),
		},
	)
	if err == nil || len(repository.results) != 0 {
		t.Fatalf("error = %v, repository results = %#v", err, repository.results)
	}
}

type agentExecutionRepositoryStub struct {
	order         []string
	requests      []storage.RecordAgentToolRequest
	results       []storage.RecordAgentToolResult
	transitions   []storage.RecordPlanStepTransition
	stepStates    []storage.PlanStepStatus
	transitionErr error
}

func (repository *agentExecutionRepositoryStub) RecordAgentToolRequest(
	_ context.Context,
	input storage.RecordAgentToolRequest,
) (storage.AgentToolRequest, error) {
	repository.order = append(repository.order, "start")
	repository.requests = append(repository.requests, input)
	return storage.AgentToolRequest{ID: input.ID}, nil
}

func (repository *agentExecutionRepositoryStub) RecordAgentToolResult(
	_ context.Context,
	input storage.RecordAgentToolResult,
) (storage.AgentToolResult, error) {
	repository.order = append(repository.order, "result")
	repository.results = append(repository.results, input)
	return storage.AgentToolResult{ID: input.ID}, nil
}

func (repository *agentExecutionRepositoryStub) RecordPlanStepTransition(
	_ context.Context,
	input storage.RecordPlanStepTransition,
) (storage.PlanStepTransition, error) {
	repository.order = append(repository.order, "transition")
	repository.transitions = append(repository.transitions, input)
	return storage.PlanStepTransition{ID: input.ID}, nil
}

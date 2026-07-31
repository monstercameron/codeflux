package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/storage"
)

// AgentExecutionRepository is the narrow durable boundary needed by the
// execution loop. storage.Repositories implements it.
type AgentExecutionRepository interface {
	RecordAgentToolRequest(
		context.Context,
		storage.RecordAgentToolRequest,
	) (storage.AgentToolRequest, error)
	RecordAgentToolResult(
		context.Context,
		storage.RecordAgentToolResult,
	) (storage.AgentToolResult, error)
	RecordPlanStepTransition(
		context.Context,
		storage.RecordPlanStepTransition,
	) (storage.PlanStepTransition, error)
}

// AgentExecutionPersistence maps the runtime's redacted journal and plan-step
// ports onto immutable storage records.
type AgentExecutionPersistence struct {
	repository AgentExecutionRepository
}

func NewAgentExecutionPersistence(
	repository AgentExecutionRepository,
) (*AgentExecutionPersistence, error) {
	if repository == nil {
		return nil, errors.New("agent execution repository is required")
	}
	return &AgentExecutionPersistence{repository: repository}, nil
}

func (persistence *AgentExecutionPersistence) PersistToolStart(
	ctx context.Context,
	record agent.ToolStartRecord,
) error {
	if persistence == nil || persistence.repository == nil {
		return errors.New("agent execution persistence is unavailable")
	}
	if record.ToolSchemaVersion <= 0 {
		return errors.New("agent tool schema version is invalid")
	}
	if record.Authorization.DecisionID == "" ||
		record.Authorization.PolicyRevision == 0 ||
		record.Authorization.PolicySHA256 == "" {
		return errors.New("agent tool authorization attribution is incomplete")
	}
	requestID := persistedAgentToolRequestID(record.RunID.String(), record.RequestID)
	decisionID := record.Authorization.DecisionID
	_, err := persistence.repository.RecordAgentToolRequest(
		ctx,
		storage.RecordAgentToolRequest{
			ID:                    requestID,
			TaskID:                record.TaskID,
			RunID:                 record.RunID,
			PlanRevision:          record.PlanRevision,
			PlanStepID:            record.PlanStepID,
			ModelRequestID:        record.ModelRequestID,
			ToolName:              string(record.ToolName),
			ToolSchemaVersion:     uint64(record.ToolSchemaVersion),
			ArgumentsRedactedJSON: record.ArgumentsRedactedJSON,
			ArgumentsSHA256:       record.ArgumentsSHA256,
			PermissionDecisionID:  &decisionID,
			IdempotencyKey: agentExecutionKey(
				"agent-tool-start-",
				record.RunID.String(),
				record.RequestID,
			),
		},
	)
	return err
}

func (persistence *AgentExecutionPersistence) PersistToolResult(
	ctx context.Context,
	record agent.ToolResultRecord,
) error {
	if persistence == nil || persistence.repository == nil {
		return errors.New("agent execution persistence is unavailable")
	}
	encoded, err := json.Marshal(record.Result)
	if err != nil {
		return fmt.Errorf("encode redacted agent tool result: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != record.ResultSHA256 {
		return errors.New("agent tool result digest does not match redacted result")
	}
	state, err := storedAgentToolResultState(record.Result.State)
	if err != nil {
		return err
	}
	requestID := persistedAgentToolRequestID(record.RunID.String(), record.RequestID)
	_, err = persistence.repository.RecordAgentToolResult(
		ctx,
		storage.RecordAgentToolResult{
			ID: agentExecutionKey(
				"agent-tool-result-",
				record.RunID.String(),
				record.RequestID,
			),
			ToolRequestID:      requestID,
			State:              state,
			ResultRedactedJSON: string(encoded),
			ResultSHA256:       record.ResultSHA256,
			IdempotencyKey: agentExecutionKey(
				"agent-tool-result-idem-",
				record.RunID.String(),
				record.RequestID,
			),
		},
	)
	return err
}

func (persistence *AgentExecutionPersistence) PersistPlanStepTransition(
	ctx context.Context,
	record agent.PlanStepTransition,
) error {
	if persistence == nil || persistence.repository == nil {
		return errors.New("agent execution persistence is unavailable")
	}
	from, err := storedPlanStepState(record.From)
	if err != nil {
		return err
	}
	to, err := storedPlanStepState(record.To)
	if err != nil {
		return err
	}
	var toolRequestID *string
	if record.ToolRequestID != "" {
		value := persistedAgentToolRequestID(
			record.RunID.String(),
			record.ToolRequestID,
		)
		toolRequestID = &value
	}
	identityParts := []string{
		record.RunID.String(),
		fmt.Sprint(record.PlanRevision),
		record.PlanStepID,
		string(record.From),
		string(record.To),
		record.Reason,
		record.ModelRequestID.String(),
		record.ToolRequestID,
	}
	_, err = persistence.repository.RecordPlanStepTransition(
		ctx,
		storage.RecordPlanStepTransition{
			ID:             agentExecutionKey("agent-step-transition-", identityParts...),
			TaskID:         record.TaskID,
			RunID:          record.RunID,
			PlanRevision:   record.PlanRevision,
			PlanStepID:     record.PlanStepID,
			From:           from,
			To:             to,
			ReasonRedacted: record.Reason,
			ModelRequestID: record.ModelRequestID,
			ToolRequestID:  toolRequestID,
			IdempotencyKey: agentExecutionKey(
				"agent-step-transition-idem-",
				identityParts...,
			),
		},
	)
	return err
}

func persistedAgentToolRequestID(runID, requestID string) string {
	return agentExecutionKey("agent-tool-request-", runID, requestID)
}

func agentExecutionKey(prefix string, values ...string) string {
	digest := sha256.New()
	var length [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(value))
	}
	return prefix + hex.EncodeToString(digest.Sum(nil))
}

func storedAgentToolResultState(
	state string,
) (storage.AgentToolResultState, error) {
	switch state {
	case "succeeded":
		return storage.AgentToolResultSucceeded, nil
	case "failed":
		return storage.AgentToolResultFailed, nil
	case "cancelled":
		return storage.AgentToolResultCancelled, nil
	case "outcome-unknown":
		return storage.AgentToolResultOutcomeUnknown, nil
	default:
		return "", fmt.Errorf("unsupported agent tool result state %q", state)
	}
}

func storedPlanStepState(
	state agent.StepState,
) (storage.PlanStepState, error) {
	switch state {
	case agent.StepPending:
		return storage.PlanStepPending, nil
	case agent.StepInProgress:
		return storage.PlanStepInProgress, nil
	case agent.StepImplemented:
		return storage.PlanStepImplemented, nil
	case agent.StepValidated:
		return storage.PlanStepValidated, nil
	case agent.StepFailed:
		return storage.PlanStepFailed, nil
	case agent.StepSkipped:
		return storage.PlanStepSkipped, nil
	default:
		return "", fmt.Errorf("unsupported agent plan step state %q", state)
	}
}

var (
	_ agent.ToolJournal   = (*AgentExecutionPersistence)(nil)
	_ agent.PlanStepStore = (*AgentExecutionPersistence)(nil)
)

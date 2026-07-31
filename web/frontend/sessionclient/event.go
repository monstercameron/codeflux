package sessionclient

import (
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

// DecodeEvent converts the public UI-safe wire event into the same validated
// domain event used by replay reducers. It rejects identity and payload-kind
// mismatches instead of partially applying an event.
func DecodeEvent(value *codefluxv1.SessionEvent) (events.SessionEvent, error) {
	if value == nil || value.GetSequence() == 0 || value.GetPayloadVersion() != 1 {
		return events.SessionEvent{}, ErrInvalidSessionEvent
	}
	sessionID, err := parseSessionIdentity(value.GetSessionId())
	if err != nil {
		return events.SessionEvent{}, err
	}
	threadID, err := parseThreadIdentity(value.GetThreadId())
	if err != nil {
		return events.SessionEvent{}, err
	}
	var taskID *domain.TaskID
	if value.GetTaskId() != nil {
		parsed, parseErr := parseTaskIdentity(value.GetTaskId())
		if parseErr != nil {
			return events.SessionEvent{}, parseErr
		}
		taskID = &parsed
	}
	var causationID, correlationID *domain.EventID
	if value.GetCausationId() != nil {
		parsed, parseErr := parseEventIdentity(value.GetCausationId())
		if parseErr != nil {
			return events.SessionEvent{}, parseErr
		}
		causationID = &parsed
	}
	if value.GetCorrelationId() != nil {
		parsed, parseErr := parseEventIdentity(value.GetCorrelationId())
		if parseErr != nil {
			return events.SessionEvent{}, parseErr
		}
		correlationID = &parsed
	}
	kind, payload, err := decodePayload(value)
	if err != nil {
		return events.SessionEvent{}, err
	}
	return (events.NewSessionEvent{
		SessionID: sessionID, ThreadID: threadID, TaskID: taskID, Kind: kind,
		Revision: value.GetRevision(), CausationID: causationID, CorrelationID: correlationID,
		PayloadVersion: value.GetPayloadVersion(), Payload: payload,
	}).Build(value.GetSequence(), time.UnixMicro(value.GetTimestampUnixMicros()).UTC())
}

func decodePayload(value *codefluxv1.SessionEvent) (events.Kind, events.Payload, error) {
	switch value.GetKind() {
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_MESSAGE_DELTA:
		payload := value.GetMessageDelta()
		if payload == nil {
			break
		}
		id, err := parseMessageIdentity(payload.GetMessageId())
		if err != nil {
			return "", events.Payload{}, err
		}
		return events.KindMessageDelta, events.Payload{MessageDelta: &events.MessageDelta{MessageID: id, RedactedDelta: payload.GetRedactedDelta()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_MESSAGE_FINAL:
		payload := value.GetMessageFinal()
		if payload == nil {
			break
		}
		id, err := parseMessageIdentity(payload.GetMessageId())
		if err != nil {
			return "", events.Payload{}, err
		}
		return events.KindMessageFinal, events.Payload{MessageFinal: &events.MessageFinal{MessageID: id, Role: payload.GetRole(), RedactedBody: payload.GetRedactedBody()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_CREATED:
		payload := value.GetThreadCreated()
		if payload == nil {
			break
		}
		var workspaceID *domain.WorkspaceID
		if payload.GetWorkspaceId() != nil {
			parsed, err := parseWorkspaceIdentity(payload.GetWorkspaceId())
			if err != nil {
				return "", events.Payload{}, err
			}
			workspaceID = &parsed
		}
		return events.KindThreadCreated, events.Payload{ThreadCreated: &events.ThreadCreated{WorkspaceID: workspaceID, Title: payload.GetTitle(), Archived: payload.GetArchived()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_RENAMED:
		payload := value.GetThreadRenamed()
		if payload == nil {
			break
		}
		return events.KindThreadRenamed, events.Payload{ThreadRenamed: &events.ThreadRenamed{PreviousTitle: payload.GetPreviousTitle(), Title: payload.GetTitle()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_ARCHIVED:
		payload := value.GetThreadArchived()
		if payload == nil {
			break
		}
		return events.KindThreadArchived, events.Payload{ThreadArchived: &events.ThreadArchived{Archived: payload.GetArchived()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_PLAN_CREATED, codefluxv1.SessionEventKind_SESSION_EVENT_KIND_PLAN_CHANGED:
		payload := value.GetPlan()
		if payload == nil {
			break
		}
		kind := events.KindPlanCreated
		if value.GetKind() == codefluxv1.SessionEventKind_SESSION_EVENT_KIND_PLAN_CHANGED {
			kind = events.KindPlanChanged
		}
		return kind, events.Payload{Plan: &events.Plan{Revision: payload.GetPlanRevision(), RedactedSummary: payload.GetRedactedSummary()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_STARTED, codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_PROGRESS, codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_COMPLETED:
		payload := value.GetTool()
		if payload == nil {
			break
		}
		kind := events.KindToolStarted
		if value.GetKind() == codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_PROGRESS {
			kind = events.KindToolProgress
		} else if value.GetKind() == codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_COMPLETED {
			kind = events.KindToolCompleted
		}
		return kind, events.Payload{Tool: &events.Tool{ExecutionID: payload.GetExecutionId(), CommandName: payload.GetCommandName(), State: payload.GetState(), RedactedSummary: payload.GetRedactedSummary()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_APPROVAL_REQUESTED, codefluxv1.SessionEventKind_SESSION_EVENT_KIND_APPROVAL_RESOLVED:
		payload := value.GetApproval()
		if payload == nil {
			break
		}
		id, err := parseApprovalIdentity(payload.GetApprovalId())
		if err != nil {
			return "", events.Payload{}, err
		}
		kind := events.KindApprovalRequested
		if value.GetKind() == codefluxv1.SessionEventKind_SESSION_EVENT_KIND_APPROVAL_RESOLVED {
			kind = events.KindApprovalResolved
		}
		return kind, events.Payload{Approval: &events.Approval{ApprovalID: id, State: domain.ApprovalRequestState(payload.GetState()), Scope: payload.GetScope(), RedactedReason: payload.GetRedactedReason()}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TASK_STATE_CHANGED:
		payload := value.GetTaskStateChanged()
		if payload == nil {
			break
		}
		return events.KindTaskStateChanged, events.Payload{TaskStateChanged: &events.TaskStateChanged{From: domain.TaskState(payload.GetFrom()), To: domain.TaskState(payload.GetTo()), Approval: domain.ApprovalRequestState(payload.GetApprovalState())}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_FORECAST_UPDATED:
		payload := value.GetForecast()
		if payload == nil {
			break
		}
		rangeValue := domain.ForecastRange{LatencyKnown: payload.GetLatencyKnown(), LatencyP50Millis: domain.Milliseconds(payload.GetLatencyP50Ms()), LatencyP90Millis: domain.Milliseconds(payload.GetLatencyP90Ms()), TokensKnown: payload.GetTokensKnown(), TokensP50: domain.TokenCount(payload.GetTokensP50()), TokensP90: domain.TokenCount(payload.GetTokensP90()), CostKnown: payload.GetCostKnown()}
		if payload.GetCostKnown() {
			currency, err := domain.ParseCurrencyCode(payload.GetCurrency())
			if err != nil {
				return "", events.Payload{}, err
			}
			rangeValue.CostP50, err = domain.NewMoney(currency, payload.GetCostP50Minor())
			if err != nil {
				return "", events.Payload{}, err
			}
			rangeValue.CostP90, err = domain.NewMoney(currency, payload.GetCostP90Minor())
			if err != nil {
				return "", events.Payload{}, err
			}
		}
		return events.KindForecastUpdated, events.Payload{Forecast: &events.Forecast{Range: rangeValue}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_USAGE_UPDATED:
		payload := value.GetUsage()
		if payload == nil {
			break
		}
		return events.KindUsageUpdated, events.Payload{Usage: &events.Usage{Tokens: domain.TokenUsage{Known: payload.GetKnown(), Input: domain.TokenCount(payload.GetInputTokens()), CachedInput: domain.TokenCount(payload.GetCachedInputTokens()), Output: domain.TokenCount(payload.GetOutputTokens()), Reasoning: domain.TokenCount(payload.GetReasoningTokens())}}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_COST_UPDATED:
		payload := value.GetCost()
		if payload == nil {
			break
		}
		cost := events.Cost{Known: payload.GetKnown()}
		if payload.GetKnown() {
			currency, err := domain.ParseCurrencyCode(payload.GetCurrency())
			if err != nil {
				return "", events.Payload{}, err
			}
			cost.Value, err = domain.NewMoney(currency, payload.GetMinorUnits())
			if err != nil {
				return "", events.Payload{}, err
			}
		}
		return events.KindCostUpdated, events.Payload{Cost: &cost}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_BUDGET_UPDATED:
		payload := value.GetBudget()
		if payload == nil {
			break
		}
		currency, err := domain.ParseCurrencyCode(payload.GetCurrency())
		if err != nil {
			return "", events.Payload{}, err
		}
		hard, err := domain.NewMoney(currency, payload.GetHardLimitMinor())
		if err != nil {
			return "", events.Payload{}, err
		}
		reserved, err := domain.NewMoney(currency, payload.GetReservedMinor())
		if err != nil {
			return "", events.Payload{}, err
		}
		actual, err := domain.NewMoney(currency, payload.GetActualMinor())
		if err != nil {
			return "", events.Payload{}, err
		}
		return events.KindBudgetUpdated, events.Payload{Budget: &events.Budget{HardLimit: hard, Reserved: reserved, Actual: actual}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_VALIDATION_UPDATED:
		payload := value.GetValidation()
		if payload == nil {
			break
		}
		id, err := parseValidationIdentity(payload.GetValidationId())
		if err != nil {
			return "", events.Payload{}, err
		}
		return events.KindValidationUpdated, events.Payload{Validation: &events.Validation{
			ValidationID: id, State: domain.ValidationState(payload.GetState()),
			RedactedSummary: payload.GetRedactedSummary(), Required: payload.GetRequired(),
			Acknowledged: payload.GetAcknowledged(), DiffRevision: payload.GetDiffRevision(),
		}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_GRAPH_SNAPSHOT, codefluxv1.SessionEventKind_SESSION_EVENT_KIND_GRAPH_PATCH:
		payload := value.GetGraph()
		if payload == nil {
			break
		}
		id, err := parseGraphRevisionIdentity(payload.GetGraphRevisionId())
		if err != nil {
			return "", events.Payload{}, err
		}
		kind := events.KindGraphSnapshot
		if value.GetKind() == codefluxv1.SessionEventKind_SESSION_EVENT_KIND_GRAPH_PATCH {
			kind = events.KindGraphPatch
		}
		return kind, events.Payload{Graph: &events.Graph{RevisionID: id, EncodedChange: append([]byte(nil), payload.GetEncodedChange()...)}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_CHECKPOINT_CREATED:
		payload := value.GetCheckpoint()
		if payload == nil {
			break
		}
		id, err := parseCheckpointIdentity(payload.GetCheckpointId())
		if err != nil {
			return "", events.Payload{}, err
		}
		return events.KindCheckpointCreated, events.Payload{Checkpoint: &events.Checkpoint{
			CheckpointID: id, TaskRevision: payload.GetTaskRevision(), PlanStep: payload.GetPlanStep(),
		}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_RECOVERY_REQUIRED:
		payload := value.GetRecoveryRequired()
		if payload == nil {
			break
		}
		var checkpointID *domain.CheckpointID
		if payload.GetCheckpointId() != nil {
			parsed, err := parseCheckpointIdentity(payload.GetCheckpointId())
			if err != nil {
				return "", events.Payload{}, err
			}
			checkpointID = &parsed
		}
		relatedEventIDs := make([]domain.EventID, 0, len(payload.GetRelatedEventIds()))
		for _, identity := range payload.GetRelatedEventIds() {
			parsed, err := parseEventIdentity(identity)
			if err != nil {
				return "", events.Payload{}, err
			}
			relatedEventIDs = append(relatedEventIDs, parsed)
		}
		return events.KindRecoveryRequired, events.Payload{RecoveryRequired: &events.RecoveryRequired{
			CheckpointID: checkpointID, RedactedReason: payload.GetRedactedReason(),
			Classification:           events.RecoveryClassification(payload.GetClassification()),
			DivergenceSummary:        payload.GetDivergenceSummary(),
			ExternalOutcomeAmbiguous: payload.GetExternalOutcomeAmbiguous(),
			SafeResumeVerified:       payload.GetSafeResumeVerified(), ReconcileAvailable: payload.GetReconcileAvailable(),
			PreservePatchAvailable: payload.GetPreservePatchAvailable(),
			Bindings: events.RevisionBindings{
				Diff: payload.GetDiffRevision(), Plan: payload.GetPlanRevision(),
				Validation: payload.GetValidationRevision(), Evidence: payload.GetEvidenceRevision(),
				Graph: payload.GetGraphRevision(),
			},
			RelatedEventIDs: relatedEventIDs, RelatedFiles: append([]string(nil), payload.GetRelatedFiles()...),
		}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_CHANGE_ACCEPTANCE_UPDATED:
		payload := value.GetChangeAcceptance()
		if payload == nil {
			break
		}
		return events.KindChangeAcceptanceUpdated, events.Payload{ChangeAcceptance: &events.ChangeAcceptance{
			State: domain.ChangeAcceptanceState(payload.GetState()),
			Bindings: events.RevisionBindings{
				Diff: payload.GetDiffRevision(), Plan: payload.GetPlanRevision(),
				Validation: payload.GetValidationRevision(), Evidence: payload.GetEvidenceRevision(),
				Graph: payload.GetGraphRevision(),
			},
		}}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TASK_PROJECTION_INVALIDATED:
		payload := value.GetTaskProjectionInvalidated()
		if payload == nil {
			break
		}
		return events.KindTaskProjectionInvalidated, events.Payload{
			TaskProjectionInvalidated: &events.TaskProjectionInvalidated{
				Entity: payload.GetEntity(), Revision: payload.GetEntityRevision(),
			},
		}, nil
	case codefluxv1.SessionEventKind_SESSION_EVENT_KIND_ERROR:
		payload := value.GetError()
		if payload == nil {
			break
		}
		return events.KindError, events.Payload{Error: &events.UserError{Code: events.ErrorCode(payload.GetCode()), RedactedMessage: payload.GetRedactedMessage(), Retryable: payload.GetRetryable()}}, nil
	}
	return "", events.Payload{}, ErrInvalidSessionEvent
}

func parseIdentity(identity *codefluxv1.StableIdentity, kind codefluxv1.StableIdentityKind) (string, error) {
	if identity == nil || identity.GetKind() != kind || identity.GetValue() == "" {
		return "", ErrInvalidSessionEvent
	}
	return identity.GetValue(), nil
}
func parseSessionIdentity(value *codefluxv1.StableIdentity) (domain.SessionID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION)
	if err != nil {
		return domain.SessionID{}, err
	}
	return domain.ParseSessionID(raw)
}
func parseThreadIdentity(value *codefluxv1.StableIdentity) (domain.ThreadID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD)
	if err != nil {
		return domain.ThreadID{}, err
	}
	return domain.ParseThreadID(raw)
}
func parseTaskIdentity(value *codefluxv1.StableIdentity) (domain.TaskID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK)
	if err != nil {
		return domain.TaskID{}, err
	}
	return domain.ParseTaskID(raw)
}
func parseEventIdentity(value *codefluxv1.StableIdentity) (domain.EventID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT)
	if err != nil {
		return domain.EventID{}, err
	}
	return domain.ParseEventID(raw)
}
func parseWorkspaceIdentity(value *codefluxv1.StableIdentity) (domain.WorkspaceID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE)
	if err != nil {
		return domain.WorkspaceID{}, err
	}
	return domain.ParseWorkspaceID(raw)
}
func parseMessageIdentity(value *codefluxv1.StableIdentity) (domain.MessageID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE)
	if err != nil {
		return domain.MessageID{}, err
	}
	return domain.ParseMessageID(raw)
}
func parseApprovalIdentity(value *codefluxv1.StableIdentity) (domain.ApprovalID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL)
	if err != nil {
		return domain.ApprovalID{}, err
	}
	return domain.ParseApprovalID(raw)
}
func parseValidationIdentity(value *codefluxv1.StableIdentity) (domain.ValidationID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION)
	if err != nil {
		return domain.ValidationID{}, err
	}
	return domain.ParseValidationID(raw)
}
func parseGraphRevisionIdentity(value *codefluxv1.StableIdentity) (domain.GraphRevisionID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION)
	if err != nil {
		return domain.GraphRevisionID{}, err
	}
	return domain.ParseGraphRevisionID(raw)
}
func parseCheckpointIdentity(value *codefluxv1.StableIdentity) (domain.CheckpointID, error) {
	raw, err := parseIdentity(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT)
	if err != nil {
		return domain.CheckpointID{}, err
	}
	return domain.ParseCheckpointID(raw)
}

package transport

import (
	"context"
	"errors"
	"io"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/events"
	"google.golang.org/grpc"
)

type sessionEventSource interface {
	Subscribe(context.Context, events.SubscriptionQuery) (*events.Subscription, error)
}

// SessionService exposes committed replay joined to bounded live delivery.
// Authentication and request validation remain owned by the stream boundary.
type SessionService struct {
	codefluxv1.UnimplementedSessionServiceServer
	source sessionEventSource
}

func NewSessionService(source sessionEventSource) (*SessionService, error) {
	if source == nil {
		return nil, errors.New("session event source is required")
	}
	return &SessionService{source: source}, nil
}

func (service *SessionService) SubscribeSession(
	request *codefluxv1.SubscribeSessionRequest,
	stream grpc.ServerStreamingServer[codefluxv1.SubscribeSessionResponse],
) error {
	sessionID, err := SessionIDFromProto(request.GetSessionId())
	if err != nil {
		return requestIdentityError("session_id", err)
	}
	subscription, err := service.source.Subscribe(stream.Context(), events.SubscriptionQuery{
		SessionID: sessionID, AfterSequence: request.GetAfterSequence(),
	})
	if err != nil {
		return err
	}
	defer subscription.Close()
	for {
		event, nextErr := subscription.Next(stream.Context())
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		converted, convertErr := sessionEventToProto(event)
		if convertErr != nil {
			return convertErr
		}
		if sendErr := stream.Send(&codefluxv1.SubscribeSessionResponse{Event: converted}); sendErr != nil {
			return sendErr
		}
	}
}

func sessionEventToProto(event events.SessionEvent) (*codefluxv1.SessionEvent, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	sessionID, err := SessionIDToProto(event.SessionID)
	if err != nil {
		return nil, err
	}
	threadID, err := ThreadIDToProto(event.ThreadID)
	if err != nil {
		return nil, err
	}
	result := &codefluxv1.SessionEvent{
		Sequence: event.Sequence, SessionId: sessionID, ThreadId: threadID,
		TimestampUnixMicros: event.Timestamp.UnixMicro(), Revision: event.Revision,
		PayloadVersion: event.PayloadVersion,
	}
	if event.TaskID != nil {
		result.TaskId, err = TaskIDToProto(*event.TaskID)
		if err != nil {
			return nil, err
		}
	}
	if event.CausationID != nil {
		result.CausationId, err = EventIDToProto(*event.CausationID)
		if err != nil {
			return nil, err
		}
	}
	if event.CorrelationID != nil {
		result.CorrelationId, err = EventIDToProto(*event.CorrelationID)
		if err != nil {
			return nil, err
		}
	}
	if err := setSessionEventPayload(result, event); err != nil {
		return nil, err
	}
	return result, nil
}

func setSessionEventPayload(result *codefluxv1.SessionEvent, event events.SessionEvent) error {
	switch event.Kind {
	case events.KindMessageDelta:
		value := event.Payload.MessageDelta
		identity, err := MessageIDToProto(value.MessageID)
		if err != nil {
			return err
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_MESSAGE_DELTA
		result.Payload = &codefluxv1.SessionEvent_MessageDelta{MessageDelta: &codefluxv1.MessageDeltaEvent{MessageId: identity, RedactedDelta: value.RedactedDelta}}
	case events.KindMessageFinal:
		value := event.Payload.MessageFinal
		identity, err := MessageIDToProto(value.MessageID)
		if err != nil {
			return err
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_MESSAGE_FINAL
		result.Payload = &codefluxv1.SessionEvent_MessageFinal{MessageFinal: &codefluxv1.MessageFinalEvent{MessageId: identity, Role: value.Role, RedactedBody: value.RedactedBody}}
	case events.KindThreadCreated:
		value := event.Payload.ThreadCreated
		payload := &codefluxv1.ThreadCreatedEvent{Title: value.Title, Archived: value.Archived}
		if value.WorkspaceID != nil {
			identity, err := WorkspaceIDToProto(*value.WorkspaceID)
			if err != nil {
				return err
			}
			payload.WorkspaceId = identity
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_CREATED
		result.Payload = &codefluxv1.SessionEvent_ThreadCreated{ThreadCreated: payload}
	case events.KindThreadRenamed:
		value := event.Payload.ThreadRenamed
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_RENAMED
		result.Payload = &codefluxv1.SessionEvent_ThreadRenamed{ThreadRenamed: &codefluxv1.ThreadRenamedEvent{PreviousTitle: value.PreviousTitle, Title: value.Title}}
	case events.KindThreadArchived:
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_ARCHIVED
		result.Payload = &codefluxv1.SessionEvent_ThreadArchived{ThreadArchived: &codefluxv1.ThreadArchivedEvent{Archived: event.Payload.ThreadArchived.Archived}}
	case events.KindPlanCreated, events.KindPlanChanged:
		value := event.Payload.Plan
		if event.Kind == events.KindPlanCreated {
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_PLAN_CREATED
		} else {
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_PLAN_CHANGED
		}
		result.Payload = &codefluxv1.SessionEvent_Plan{Plan: &codefluxv1.PlanEvent{PlanRevision: value.Revision, RedactedSummary: value.RedactedSummary}}
	case events.KindToolStarted, events.KindToolProgress, events.KindToolCompleted:
		value := event.Payload.Tool
		switch event.Kind {
		case events.KindToolStarted:
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_STARTED
		case events.KindToolProgress:
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_PROGRESS
		default:
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TOOL_COMPLETED
		}
		result.Payload = &codefluxv1.SessionEvent_Tool{Tool: &codefluxv1.ToolEvent{ExecutionId: value.ExecutionID, CommandName: value.CommandName, State: value.State, RedactedSummary: value.RedactedSummary}}
	case events.KindApprovalRequested, events.KindApprovalResolved:
		value := event.Payload.Approval
		identity, err := ApprovalIDToProto(value.ApprovalID)
		if err != nil {
			return err
		}
		if event.Kind == events.KindApprovalRequested {
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_APPROVAL_REQUESTED
		} else {
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_APPROVAL_RESOLVED
		}
		result.Payload = &codefluxv1.SessionEvent_Approval{Approval: &codefluxv1.ApprovalEvent{ApprovalId: identity, State: string(value.State), Scope: value.Scope, RedactedReason: value.RedactedReason}}
	case events.KindTaskStateChanged:
		value := event.Payload.TaskStateChanged
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TASK_STATE_CHANGED
		result.Payload = &codefluxv1.SessionEvent_TaskStateChanged{TaskStateChanged: &codefluxv1.TaskStateChangedEvent{From: string(value.From), To: string(value.To), ApprovalState: string(value.Approval)}}
	case events.KindForecastUpdated:
		value := event.Payload.Forecast.Range
		payload := &codefluxv1.ForecastEvent{CostKnown: value.CostKnown, TokensKnown: value.TokensKnown, TokensP50: uint64(value.TokensP50), TokensP90: uint64(value.TokensP90), LatencyKnown: value.LatencyKnown, LatencyP50Ms: int64(value.LatencyP50Millis), LatencyP90Ms: int64(value.LatencyP90Millis)}
		if value.CostKnown {
			payload.CostP50Minor = value.CostP50.MinorUnits
			payload.CostP90Minor = value.CostP90.MinorUnits
			payload.Currency = string(value.CostP50.Currency)
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_FORECAST_UPDATED
		result.Payload = &codefluxv1.SessionEvent_Forecast{Forecast: payload}
	case events.KindUsageUpdated:
		value := event.Payload.Usage.Tokens
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_USAGE_UPDATED
		result.Payload = &codefluxv1.SessionEvent_Usage{Usage: &codefluxv1.UsageEvent{Known: value.Known, InputTokens: uint64(value.Input), CachedInputTokens: uint64(value.CachedInput), OutputTokens: uint64(value.Output), ReasoningTokens: uint64(value.Reasoning)}}
	case events.KindCostUpdated:
		value := event.Payload.Cost
		payload := &codefluxv1.CostEvent{Known: value.Known}
		if value.Known {
			payload.MinorUnits = value.Value.MinorUnits
			payload.Currency = string(value.Value.Currency)
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_COST_UPDATED
		result.Payload = &codefluxv1.SessionEvent_Cost{Cost: payload}
	case events.KindBudgetUpdated:
		value := event.Payload.Budget
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_BUDGET_UPDATED
		result.Payload = &codefluxv1.SessionEvent_Budget{Budget: &codefluxv1.BudgetEvent{HardLimitMinor: value.HardLimit.MinorUnits, ReservedMinor: value.Reserved.MinorUnits, ActualMinor: value.Actual.MinorUnits, Currency: string(value.HardLimit.Currency)}}
	case events.KindValidationUpdated:
		value := event.Payload.Validation
		identity, err := ValidationIDToProto(value.ValidationID)
		if err != nil {
			return err
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_VALIDATION_UPDATED
		result.Payload = &codefluxv1.SessionEvent_Validation{Validation: &codefluxv1.ValidationEvent{ValidationId: identity, State: string(value.State), RedactedSummary: value.RedactedSummary}}
	case events.KindGraphSnapshot, events.KindGraphPatch:
		value := event.Payload.Graph
		identity, err := GraphRevisionIDToProto(value.RevisionID)
		if err != nil {
			return err
		}
		if event.Kind == events.KindGraphSnapshot {
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_GRAPH_SNAPSHOT
		} else {
			result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_GRAPH_PATCH
		}
		result.Payload = &codefluxv1.SessionEvent_Graph{Graph: &codefluxv1.GraphEvent{GraphRevisionId: identity, EncodedChange: append([]byte(nil), value.EncodedChange...)}}
	case events.KindCheckpointCreated:
		value := event.Payload.Checkpoint
		identity, err := CheckpointIDToProto(value.CheckpointID)
		if err != nil {
			return err
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_CHECKPOINT_CREATED
		result.Payload = &codefluxv1.SessionEvent_Checkpoint{Checkpoint: &codefluxv1.CheckpointEvent{CheckpointId: identity, TaskRevision: value.TaskRevision}}
	case events.KindRecoveryRequired:
		value := event.Payload.RecoveryRequired
		payload := &codefluxv1.RecoveryRequiredEvent{RedactedReason: value.RedactedReason}
		if value.CheckpointID != nil {
			identity, err := CheckpointIDToProto(*value.CheckpointID)
			if err != nil {
				return err
			}
			payload.CheckpointId = identity
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_RECOVERY_REQUIRED
		result.Payload = &codefluxv1.SessionEvent_RecoveryRequired{RecoveryRequired: payload}
	case events.KindError:
		value := event.Payload.Error
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_ERROR
		result.Payload = &codefluxv1.SessionEvent_Error{Error: &codefluxv1.UserErrorEvent{Code: string(value.Code), RedactedMessage: value.RedactedMessage, Retryable: value.Retryable}}
	default:
		return errors.New("unsupported session event kind")
	}
	return nil
}

var _ codefluxv1.SessionServiceServer = (*SessionService)(nil)

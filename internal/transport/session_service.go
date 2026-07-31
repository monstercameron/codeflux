package transport

import (
	"context"
	"errors"
	"io"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sessionEventSource interface {
	Subscribe(context.Context, events.SubscriptionQuery) (*events.Subscription, error)
}

// SessionProjectionSnapshotView is the transport-independent complete client
// reducer base observed through one committed session sequence.
type SessionProjectionSnapshotView struct {
	SessionID              domain.SessionID
	ThreadID               domain.ThreadID
	ThroughSequence        uint64
	ObservedAt             time.Time
	TaskID                 *domain.TaskID
	TaskState              domain.TaskState
	TaskRevision           uint64
	Plan                   *events.Plan
	PlanApproval           domain.ApprovalRequestState
	PendingApproval        *events.Approval
	ApprovalRevision       uint64
	Budget                 *events.Budget
	BudgetRevision         uint64
	Tool                   *events.Tool
	ToolRevision           uint64
	Validation             *events.Validation
	ValidationRevision     uint64
	Checkpoint             *events.Checkpoint
	CheckpointRevision     uint64
	CheckpointAt           time.Time
	Recovery               *events.RecoveryRequired
	RecoveryRevision       uint64
	Acceptance             *events.ChangeAcceptance
	AcceptanceRevision     uint64
	ReviewBindings         *events.RevisionBindings
	ReviewRevision         uint64
	GraphRevision          uint64
	DeniedTaskActions      []string
	TaskActionPolicyReason string
}

type sessionProjectionSnapshotApplication interface {
	GetSessionProjectionSnapshot(context.Context, domain.SessionID) (SessionProjectionSnapshotView, error)
}

// SessionService exposes committed replay joined to bounded live delivery.
// Authentication and request validation remain owned by the stream boundary.
type SessionService struct {
	codefluxv1.UnimplementedSessionServiceServer
	source    sessionEventSource
	snapshots sessionProjectionSnapshotApplication
}

func NewSessionService(
	source sessionEventSource,
	snapshotApplications ...sessionProjectionSnapshotApplication,
) (*SessionService, error) {
	if source == nil {
		return nil, errors.New("session event source is required")
	}
	if len(snapshotApplications) > 1 {
		return nil, errors.New("at most one session snapshot application is permitted")
	}
	var snapshots sessionProjectionSnapshotApplication
	if len(snapshotApplications) == 1 {
		if snapshotApplications[0] == nil {
			return nil, errors.New("session snapshot application must not be nil")
		}
		snapshots = snapshotApplications[0]
	}
	return &SessionService{source: source, snapshots: snapshots}, nil
}

func (service *SessionService) GetSessionSnapshot(
	ctx context.Context,
	request *codefluxv1.GetSessionSnapshotRequest,
) (*codefluxv1.GetSessionSnapshotResponse, error) {
	if service.snapshots == nil {
		return nil, errors.New("session snapshot application is unavailable")
	}
	sessionID, err := SessionIDFromProto(request.GetSessionId())
	if err != nil {
		return nil, requestIdentityError("session_id", err)
	}
	view, err := service.snapshots.GetSessionProjectionSnapshot(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	snapshot, err := sessionProjectionSnapshotToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.GetSessionSnapshotResponse{Snapshot: snapshot}, nil
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
	replayBoundary := subscription.ReplayBoundary()
	replayComplete := false
	sendReplayBoundary := func() error {
		if replayComplete {
			return nil
		}
		if err := stream.Send(&codefluxv1.SubscribeSessionResponse{
			ReplayBoundary: &codefluxv1.SessionReplayBoundary{ThroughSequence: replayBoundary},
		}); err != nil {
			return err
		}
		replayComplete = true
		return nil
	}
	if request.GetAfterSequence() == replayBoundary {
		if err := sendReplayBoundary(); err != nil {
			return err
		}
	}
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
		if event.Sequence == replayBoundary {
			if err := sendReplayBoundary(); err != nil {
				return err
			}
		}
	}
}

func sessionProjectionSnapshotToProto(
	view SessionProjectionSnapshotView,
) (*codefluxv1.SessionProjectionSnapshot, error) {
	sessionID, err := SessionIDToProto(view.SessionID)
	if err != nil {
		return nil, err
	}
	threadID, err := ThreadIDToProto(view.ThreadID)
	if err != nil {
		return nil, err
	}
	observedAt := timestamppb.New(view.ObservedAt.UTC())
	if err := observedAt.CheckValid(); err != nil {
		return nil, err
	}
	result := &codefluxv1.SessionProjectionSnapshot{
		SessionId: sessionID, ThreadId: threadID,
		ThroughSequence: view.ThroughSequence, ObservedAt: observedAt,
	}
	if view.TaskID == nil {
		if view.TaskState != "" || view.TaskRevision != 0 || view.Plan != nil ||
			view.PendingApproval != nil || view.Budget != nil || view.Validation != nil ||
			view.Checkpoint != nil || view.Recovery != nil || view.Acceptance != nil ||
			view.Tool != nil || view.ReviewBindings != nil || view.ReviewRevision != 0 ||
			view.GraphRevision != 0 || len(view.DeniedTaskActions) != 0 ||
			view.TaskActionPolicyReason != "" {
			return nil, errors.New("session snapshot task projections require a task identity")
		}
		return result, nil
	}
	result.TaskId, err = TaskIDToProto(*view.TaskID)
	if err != nil {
		return nil, err
	}
	if !view.TaskState.IsValid() || view.TaskRevision == 0 && view.TaskState != domain.TaskStateDraft {
		return nil, errors.New("session snapshot task state or revision is invalid")
	}
	result.TaskState, result.TaskRevision = string(view.TaskState), view.TaskRevision
	if view.Plan != nil {
		result.Plan = &codefluxv1.PlanEvent{PlanRevision: view.Plan.Revision, RedactedSummary: view.Plan.RedactedSummary}
		result.PlanApprovalState = string(view.PlanApproval)
	}
	if view.PendingApproval != nil {
		identity, identityErr := ApprovalIDToProto(view.PendingApproval.ApprovalID)
		if identityErr != nil {
			return nil, identityErr
		}
		result.PendingApproval = &codefluxv1.ApprovalEvent{
			ApprovalId: identity, State: string(view.PendingApproval.State), Scope: view.PendingApproval.Scope,
			RedactedReason: view.PendingApproval.RedactedReason,
		}
		result.ApprovalRevision = view.ApprovalRevision
	}
	if view.Budget != nil {
		result.Budget = &codefluxv1.BudgetEvent{
			HardLimitMinor: view.Budget.HardLimit.MinorUnits,
			ReservedMinor:  view.Budget.Reserved.MinorUnits,
			ActualMinor:    view.Budget.Actual.MinorUnits,
			Currency:       string(view.Budget.HardLimit.Currency),
		}
		result.BudgetRevision = view.BudgetRevision
	}
	if view.Tool != nil {
		result.Tool = &codefluxv1.ToolEvent{
			ExecutionId: view.Tool.ExecutionID, CommandName: view.Tool.CommandName,
			State: view.Tool.State, RedactedSummary: view.Tool.RedactedSummary,
		}
		result.ToolRevision = view.ToolRevision
	}
	if view.Validation != nil {
		identity, identityErr := ValidationIDToProto(view.Validation.ValidationID)
		if identityErr != nil {
			return nil, identityErr
		}
		result.Validation = &codefluxv1.ValidationEvent{
			ValidationId: identity, State: string(view.Validation.State),
			RedactedSummary: view.Validation.RedactedSummary, Required: view.Validation.Required,
			Acknowledged: view.Validation.Acknowledged, DiffRevision: view.Validation.DiffRevision,
		}
		result.ValidationRevision = view.ValidationRevision
	}
	if view.Checkpoint != nil {
		identity, identityErr := CheckpointIDToProto(view.Checkpoint.CheckpointID)
		if identityErr != nil {
			return nil, identityErr
		}
		result.Checkpoint = &codefluxv1.CheckpointEvent{
			CheckpointId: identity, TaskRevision: view.Checkpoint.TaskRevision,
			PlanStep: view.Checkpoint.PlanStep,
		}
		result.CheckpointRevision = view.CheckpointRevision
		result.CheckpointCreatedAt = timestamppb.New(view.CheckpointAt.UTC())
		if err := result.CheckpointCreatedAt.CheckValid(); err != nil {
			return nil, err
		}
	}
	if view.Recovery != nil {
		value := view.Recovery
		result.Recovery = &codefluxv1.RecoveryRequiredEvent{
			RedactedReason: value.RedactedReason, Classification: string(value.Classification),
			DivergenceSummary: value.DivergenceSummary, ExternalOutcomeAmbiguous: value.ExternalOutcomeAmbiguous,
			SafeResumeVerified: value.SafeResumeVerified, ReconcileAvailable: value.ReconcileAvailable,
			PreservePatchAvailable: value.PreservePatchAvailable,
			DiffRevision:           value.Bindings.Diff, PlanRevision: value.Bindings.Plan,
			ValidationRevision: value.Bindings.Validation, EvidenceRevision: value.Bindings.Evidence,
			GraphRevision: value.Bindings.Graph, RelatedFiles: append([]string(nil), value.RelatedFiles...),
		}
		if value.CheckpointID != nil {
			result.Recovery.CheckpointId, err = CheckpointIDToProto(*value.CheckpointID)
			if err != nil {
				return nil, err
			}
		}
		for _, eventID := range value.RelatedEventIDs {
			identity, identityErr := EventIDToProto(eventID)
			if identityErr != nil {
				return nil, identityErr
			}
			result.Recovery.RelatedEventIds = append(result.Recovery.RelatedEventIds, identity)
		}
		result.RecoveryRevision = view.RecoveryRevision
	}
	if view.Acceptance != nil {
		value := view.Acceptance
		result.ChangeAcceptance = &codefluxv1.ChangeAcceptanceEvent{
			State: string(value.State), DiffRevision: value.Bindings.Diff,
			PlanRevision: value.Bindings.Plan, ValidationRevision: value.Bindings.Validation,
			EvidenceRevision: value.Bindings.Evidence, GraphRevision: value.Bindings.Graph,
		}
		result.ChangeAcceptanceRevision = view.AcceptanceRevision
	}
	if view.ReviewBindings != nil {
		if view.ReviewRevision == 0 || !view.ReviewBindings.Complete() {
			return nil, errors.New("session snapshot review bindings are incomplete")
		}
		result.ReviewBindings = &codefluxv1.SessionRevisionBindings{
			DiffRevision:       view.ReviewBindings.Diff,
			PlanRevision:       view.ReviewBindings.Plan,
			ValidationRevision: view.ReviewBindings.Validation,
			EvidenceRevision:   view.ReviewBindings.Evidence,
			GraphRevision:      view.ReviewBindings.Graph,
		}
		result.ReviewRevision = view.ReviewRevision
	} else if view.ReviewRevision != 0 {
		return nil, errors.New("session snapshot review revision has no bindings")
	}
	result.GraphRevision = view.GraphRevision
	if len(view.DeniedTaskActions) > 0 && view.TaskActionPolicyReason == "" ||
		len(view.DeniedTaskActions) == 0 && view.TaskActionPolicyReason != "" {
		return nil, errors.New("session snapshot task-action policy is incomplete")
	}
	result.DeniedTaskActions = append([]string(nil), view.DeniedTaskActions...)
	result.TaskActionPolicyReason = view.TaskActionPolicyReason
	return result, nil
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
		result.Payload = &codefluxv1.SessionEvent_Validation{Validation: &codefluxv1.ValidationEvent{
			ValidationId: identity, State: string(value.State), RedactedSummary: value.RedactedSummary,
			Required: value.Required, Acknowledged: value.Acknowledged, DiffRevision: value.DiffRevision,
		}}
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
		result.Payload = &codefluxv1.SessionEvent_Checkpoint{Checkpoint: &codefluxv1.CheckpointEvent{
			CheckpointId: identity, TaskRevision: value.TaskRevision, PlanStep: value.PlanStep,
		}}
	case events.KindRecoveryRequired:
		value := event.Payload.RecoveryRequired
		payload := &codefluxv1.RecoveryRequiredEvent{
			RedactedReason: value.RedactedReason, Classification: string(value.Classification),
			DivergenceSummary:        value.DivergenceSummary,
			ExternalOutcomeAmbiguous: value.ExternalOutcomeAmbiguous,
			SafeResumeVerified:       value.SafeResumeVerified, ReconcileAvailable: value.ReconcileAvailable,
			PreservePatchAvailable: value.PreservePatchAvailable,
			DiffRevision:           value.Bindings.Diff, PlanRevision: value.Bindings.Plan,
			ValidationRevision: value.Bindings.Validation, EvidenceRevision: value.Bindings.Evidence,
			GraphRevision: value.Bindings.Graph, RelatedFiles: append([]string(nil), value.RelatedFiles...),
		}
		if value.CheckpointID != nil {
			identity, err := CheckpointIDToProto(*value.CheckpointID)
			if err != nil {
				return err
			}
			payload.CheckpointId = identity
		}
		for _, eventID := range value.RelatedEventIDs {
			identity, err := EventIDToProto(eventID)
			if err != nil {
				return err
			}
			payload.RelatedEventIds = append(payload.RelatedEventIds, identity)
		}
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_RECOVERY_REQUIRED
		result.Payload = &codefluxv1.SessionEvent_RecoveryRequired{RecoveryRequired: payload}
	case events.KindChangeAcceptanceUpdated:
		value := event.Payload.ChangeAcceptance
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_CHANGE_ACCEPTANCE_UPDATED
		result.Payload = &codefluxv1.SessionEvent_ChangeAcceptance{ChangeAcceptance: &codefluxv1.ChangeAcceptanceEvent{
			State: string(value.State), DiffRevision: value.Bindings.Diff,
			PlanRevision: value.Bindings.Plan, ValidationRevision: value.Bindings.Validation,
			EvidenceRevision: value.Bindings.Evidence, GraphRevision: value.Bindings.Graph,
		}}
	case events.KindTaskProjectionInvalidated:
		value := event.Payload.TaskProjectionInvalidated
		result.Kind = codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TASK_PROJECTION_INVALIDATED
		result.Payload = &codefluxv1.SessionEvent_TaskProjectionInvalidated{
			TaskProjectionInvalidated: &codefluxv1.TaskProjectionInvalidatedEvent{
				Entity: value.Entity, EntityRevision: value.Revision,
			},
		}
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

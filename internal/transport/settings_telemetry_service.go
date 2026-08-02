package transport

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/frontendtelemetry"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type FrontendTelemetryApplication interface {
	RecordFrontendTelemetry(context.Context, frontendtelemetry.Event) (frontendtelemetry.Event, error)
	ListFrontendTelemetry(context.Context, frontendtelemetry.Query) (frontendtelemetry.Page, error)
	DeleteFrontendTelemetry(context.Context, frontendtelemetry.DeleteRequest) (frontendtelemetry.DeleteResult, error)
}

// SettingsService serves the settings surfaces: the execution policy, the
// budget default, the provider and model lists, and the local telemetry
// controls.
type SettingsService struct {
	codefluxv1.UnimplementedSettingsServiceServer
	telemetry     FrontendTelemetryApplication
	configuration SettingsConfigurationApplication
}

// NewSettingsService builds the service.
//
// Both dependencies are required rather than optional. A nil configuration
// application would leave the policy, provider, and model calls answering
// Unimplemented while the service reported itself as constructed, which is how
// a settings page comes to be served by a coordinator that cannot answer it.
func NewSettingsService(
	telemetry FrontendTelemetryApplication,
	configuration SettingsConfigurationApplication,
) (*SettingsService, error) {
	if telemetry == nil {
		return nil, errors.New("frontend telemetry application is required")
	}
	if configuration == nil {
		return nil, errors.New("settings configuration application is required")
	}
	return &SettingsService{telemetry: telemetry, configuration: configuration}, nil
}

func (service *SettingsService) RecordFrontendTelemetry(
	ctx context.Context,
	request *codefluxv1.RecordFrontendTelemetryRequest,
) (*codefluxv1.RecordFrontendTelemetryResponse, error) {
	if err := requireTelemetryMutationControl(request.GetControl()); err != nil {
		return nil, err
	}
	event, err := frontendTelemetryFromProto(request.GetEvent())
	if err != nil {
		return nil, err
	}
	recorded, err := service.telemetry.RecordFrontendTelemetry(ctx, event)
	if err != nil {
		return nil, err
	}
	converted, err := frontendTelemetryToProto(recorded)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.RecordFrontendTelemetryResponse{Event: converted}, nil
}

func (service *SettingsService) ListFrontendTelemetry(
	ctx context.Context,
	request *codefluxv1.ListFrontendTelemetryRequest,
) (*codefluxv1.ListFrontendTelemetryResponse, error) {
	query, err := frontendTelemetryQueryFromProto(request)
	if err != nil {
		return nil, err
	}
	page, err := service.telemetry.ListFrontendTelemetry(ctx, query)
	if err != nil {
		return nil, err
	}
	events := make([]*codefluxv1.FrontendTelemetryEvent, 0, len(page.Events))
	for _, event := range page.Events {
		converted, convertErr := frontendTelemetryToProto(event)
		if convertErr != nil {
			return nil, convertErr
		}
		events = append(events, converted)
	}
	pageInfo := &codefluxv1.PageInfo{}
	if page.NextBeforeID != 0 {
		pageInfo.NextCursor = encodeTelemetryCursor(page.NextBeforeID)
		pageInfo.HasMore = true
	}
	return &codefluxv1.ListFrontendTelemetryResponse{Events: events, Page: pageInfo}, nil
}

func (service *SettingsService) DeleteFrontendTelemetry(
	ctx context.Context,
	request *codefluxv1.DeleteFrontendTelemetryRequest,
) (*codefluxv1.DeleteFrontendTelemetryResponse, error) {
	if err := requireTelemetryMutationControl(request.GetControl()); err != nil {
		return nil, err
	}
	deleteRequest, err := frontendTelemetryDeleteFromProto(request)
	if err != nil {
		return nil, err
	}
	result, err := service.telemetry.DeleteFrontendTelemetry(ctx, deleteRequest)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.DeleteFrontendTelemetryResponse{Deleted: result.Deleted, Remaining: result.Remaining}, nil
}

func requireTelemetryMutationControl(control *codefluxv1.MutationControl) error {
	if control == nil || control.GetIdempotencyKey() == "" {
		return &RequestValidationError{Field: "control.idempotency_key", Reason: "is required"}
	}
	return nil
}

func frontendTelemetryFromProto(value *codefluxv1.FrontendTelemetryEvent) (frontendtelemetry.Event, error) {
	if value == nil {
		return frontendtelemetry.Event{}, &RequestValidationError{Field: "event", Reason: "is required"}
	}
	event := frontendtelemetry.Event{
		ID: value.GetLocalId(), Kind: telemetryKindFromProto(value.GetKind()),
		Outcome:      telemetryOutcomeFromProto(value.GetOutcome()),
		Component:    telemetryComponentFromProto(value.GetComponent()),
		GraphMode:    telemetryGraphModeFromProto(value.GetGraphMode()),
		FailureClass: telemetryFailureFromProto(value.GetFailureClass()),
		Sequence:     value.GetSequence(), Revision: value.GetRevision(),
	}
	if value.GetOccurredAt() != nil {
		if err := value.GetOccurredAt().CheckValid(); err != nil {
			return frontendtelemetry.Event{}, &RequestValidationError{Field: "event.occurred_at", Reason: "is invalid"}
		}
		event.OccurredAt = value.GetOccurredAt().AsTime()
	}
	if value.GetDuration() != nil {
		if err := value.GetDuration().CheckValid(); err != nil {
			return frontendtelemetry.Event{}, &RequestValidationError{Field: "event.duration", Reason: "is invalid"}
		}
		event.Duration = value.GetDuration().AsDuration()
	}
	var err error
	if value.GetTaskId() != nil {
		event.TaskID, err = TaskIDFromProto(value.GetTaskId())
		if err != nil {
			return frontendtelemetry.Event{}, requestIdentityError("event.task_id", err)
		}
	}
	if value.GetThreadId() != nil {
		event.ThreadID, err = ThreadIDFromProto(value.GetThreadId())
		if err != nil {
			return frontendtelemetry.Event{}, requestIdentityError("event.thread_id", err)
		}
	}
	if value.GetSessionId() != nil {
		event.SessionID, err = SessionIDFromProto(value.GetSessionId())
		if err != nil {
			return frontendtelemetry.Event{}, requestIdentityError("event.session_id", err)
		}
	}
	if err := event.ValidateForRecord(); err != nil {
		return frontendtelemetry.Event{}, &RequestValidationError{Field: "event", Reason: err.Error()}
	}
	return event, nil
}

func frontendTelemetryQueryFromProto(request *codefluxv1.ListFrontendTelemetryRequest) (frontendtelemetry.Query, error) {
	query := frontendtelemetry.Query{}
	if request.GetPage() != nil {
		query.Limit = int(request.GetPage().GetLimit())
		if request.GetPage().GetCursor() != "" {
			cursor, err := decodeTelemetryCursor(request.GetPage().GetCursor())
			if err != nil {
				return query, &RequestValidationError{Field: "page.cursor", Reason: "is invalid"}
			}
			query.BeforeID = cursor
		}
	}
	for _, kind := range request.GetKinds() {
		converted := telemetryKindFromProto(kind)
		if !converted.IsValid() {
			return query, &RequestValidationError{Field: "kinds", Reason: "contains an unsupported kind"}
		}
		query.Kinds = append(query.Kinds, converted)
	}
	var err error
	query.Since, err = optionalProtoTime(request.GetSince(), "since")
	if err != nil {
		return query, err
	}
	query.Until, err = optionalProtoTime(request.GetUntil(), "until")
	if err != nil {
		return query, err
	}
	if err := query.Validate(); err != nil {
		return query, &RequestValidationError{Field: "query", Reason: err.Error()}
	}
	return query, nil
}

func frontendTelemetryDeleteFromProto(request *codefluxv1.DeleteFrontendTelemetryRequest) (frontendtelemetry.DeleteRequest, error) {
	result := frontendtelemetry.DeleteRequest{}
	switch request.GetScope() {
	case codefluxv1.FrontendTelemetryDeleteScope_FRONTEND_TELEMETRY_DELETE_SCOPE_ALL:
		result.Scope = frontendtelemetry.DeleteAll
	case codefluxv1.FrontendTelemetryDeleteScope_FRONTEND_TELEMETRY_DELETE_SCOPE_BEFORE:
		result.Scope = frontendtelemetry.DeleteBefore
	}
	if request.GetConfirmation() == codefluxv1.FrontendTelemetryDeleteConfirmation_FRONTEND_TELEMETRY_DELETE_CONFIRMATION_CONFIRMED {
		result.Confirmation = frontendtelemetry.ConfirmTelemetryDeletion
	}
	var err error
	result.Before, err = optionalProtoTime(request.GetBefore(), "before")
	if err != nil {
		return result, err
	}
	if err := result.Validate(); err != nil {
		return result, &RequestValidationError{Field: "delete", Reason: err.Error()}
	}
	return result, nil
}

func optionalProtoTime(value *timestamppb.Timestamp, field string) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	if err := value.CheckValid(); err != nil {
		return time.Time{}, &RequestValidationError{Field: field, Reason: "is invalid"}
	}
	return value.AsTime(), nil
}

func frontendTelemetryToProto(event frontendtelemetry.Event) (*codefluxv1.FrontendTelemetryEvent, error) {
	result := &codefluxv1.FrontendTelemetryEvent{
		LocalId: event.ID, Kind: telemetryKindToProto(event.Kind),
		OccurredAt:   timestamppb.New(event.OccurredAt.UTC()),
		Outcome:      telemetryOutcomeToProto(event.Outcome),
		Component:    telemetryComponentToProto(event.Component),
		GraphMode:    telemetryGraphModeToProto(event.GraphMode),
		FailureClass: telemetryFailureToProto(event.FailureClass),
		Sequence:     event.Sequence, Revision: event.Revision,
	}
	if event.Duration != 0 {
		result.Duration = durationpb.New(event.Duration)
	}
	var err error
	if !event.TaskID.IsZero() {
		result.TaskId, err = TaskIDToProto(event.TaskID)
		if err != nil {
			return nil, err
		}
	}
	if !event.ThreadID.IsZero() {
		result.ThreadId, err = ThreadIDToProto(event.ThreadID)
		if err != nil {
			return nil, err
		}
	}
	if !event.SessionID.IsZero() {
		result.SessionId, err = SessionIDToProto(event.SessionID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func encodeTelemetryCursor(value uint64) string {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func decodeTelemetryCursor(value string) (uint64, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) != 8 {
		return 0, errors.New("invalid telemetry cursor")
	}
	result := binary.BigEndian.Uint64(data)
	if result == 0 {
		return 0, errors.New("invalid telemetry cursor")
	}
	return result, nil
}

var telemetryKindsFromProto = map[codefluxv1.FrontendTelemetryKind]frontendtelemetry.Kind{
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FIRST_RUN_STEP:     frontendtelemetry.KindFirstRunStep,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_THREAD:     frontendtelemetry.KindTimeToThread,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_MESSAGE:    frontendtelemetry.KindTimeToMessage,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_PLAN:       frontendtelemetry.KindTimeToPlan,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_DIFF:       frontendtelemetry.KindTimeToDiff,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_PLAN_DECISION:      frontendtelemetry.KindPlanDecision,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_APPROVAL_DECISION:  frontendtelemetry.KindApprovalDecision,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL:       frontendtelemetry.KindTaskControl,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_REVIEW_DECISION:    frontendtelemetry.KindReviewDecision,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_GRAPH_INTERACTION:  frontendtelemetry.KindGraphInteraction,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_MEMORY_INTERACTION: frontendtelemetry.KindMemoryInteraction,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECONNECT:          frontendtelemetry.KindReconnect,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECOVERY_ACTION:    frontendtelemetry.KindRecoveryAction,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FRONTEND_ERROR:     frontendtelemetry.KindFrontendError,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_LONG_TASK:          frontendtelemetry.KindLongTask,
	codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_SLOW_RENDER:        frontendtelemetry.KindSlowRender,
}

func telemetryKindFromProto(value codefluxv1.FrontendTelemetryKind) frontendtelemetry.Kind {
	return telemetryKindsFromProto[value]
}
func telemetryKindToProto(value frontendtelemetry.Kind) codefluxv1.FrontendTelemetryKind {
	for protoValue, domainValue := range telemetryKindsFromProto {
		if domainValue == value {
			return protoValue
		}
	}
	return codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_UNSPECIFIED
}

var telemetryOutcomesFromProto = map[codefluxv1.FrontendTelemetryOutcome]frontendtelemetry.Outcome{
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED:          frontendtelemetry.OutcomeSucceeded,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_FAILED:             frontendtelemetry.OutcomeFailed,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_CANCELLED:          frontendtelemetry.OutcomeCancelled,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_APPROVED:           frontendtelemetry.OutcomeApproved,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_DENIED:             frontendtelemetry.OutcomeDenied,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_EXPIRED:            frontendtelemetry.OutcomeExpired,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REVISION_REQUESTED: frontendtelemetry.OutcomeRevisionRequested,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_PAUSED:             frontendtelemetry.OutcomePaused,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_STOPPED:            frontendtelemetry.OutcomeStopped,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_OPENED:             frontendtelemetry.OutcomeOpened,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_NAVIGATED:          frontendtelemetry.OutcomeNavigated,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_INSPECTED:          frontendtelemetry.OutcomeInspected,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_CORRECTED:          frontendtelemetry.OutcomeCorrected,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ACCEPTED:           frontendtelemetry.OutcomeAccepted,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REPAIR_REQUESTED:   frontendtelemetry.OutcomeRepairRequested,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REJECTED:           frontendtelemetry.OutcomeRejected,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ROLLED_BACK:        frontendtelemetry.OutcomeRolledBack,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_RECONNECTED:        frontendtelemetry.OutcomeReconnected,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SAFE_RESUMED:       frontendtelemetry.OutcomeSafeResumed,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_RECONCILED:         frontendtelemetry.OutcomeReconciled,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_PATCH_PRESERVED:    frontendtelemetry.OutcomePatchPreserved,
	codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ABANDONED:          frontendtelemetry.OutcomeAbandoned,
}

func telemetryOutcomeFromProto(value codefluxv1.FrontendTelemetryOutcome) frontendtelemetry.Outcome {
	return telemetryOutcomesFromProto[value]
}
func telemetryOutcomeToProto(value frontendtelemetry.Outcome) codefluxv1.FrontendTelemetryOutcome {
	for protoValue, domainValue := range telemetryOutcomesFromProto {
		if domainValue == value {
			return protoValue
		}
	}
	return codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_UNSPECIFIED
}

var telemetryComponentsFromProto = map[codefluxv1.FrontendTelemetryComponent]frontendtelemetry.Component{
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_FIRST_RUN: frontendtelemetry.ComponentFirstRun,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_THREAD:    frontendtelemetry.ComponentThread,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_COMPOSER:  frontendtelemetry.ComponentComposer,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_PLAN:      frontendtelemetry.ComponentPlan,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_DIFF:      frontendtelemetry.ComponentDiff,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_APPROVAL:  frontendtelemetry.ComponentApproval,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TOP_BAR:   frontendtelemetry.ComponentTopBar,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_REVIEW:    frontendtelemetry.ComponentReview,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_GRAPH:     frontendtelemetry.ComponentGraph,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_MEMORY:    frontendtelemetry.ComponentMemory,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_SESSION:   frontendtelemetry.ComponentSession,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_RECOVERY:  frontendtelemetry.ComponentRecovery,
	codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TIMELINE:  frontendtelemetry.ComponentTimeline,
}

func telemetryComponentFromProto(value codefluxv1.FrontendTelemetryComponent) frontendtelemetry.Component {
	return telemetryComponentsFromProto[value]
}
func telemetryComponentToProto(value frontendtelemetry.Component) codefluxv1.FrontendTelemetryComponent {
	for protoValue, domainValue := range telemetryComponentsFromProto {
		if domainValue == value {
			return protoValue
		}
	}
	return codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_UNSPECIFIED
}

func telemetryGraphModeFromProto(value codefluxv1.FrontendTelemetryGraphMode) frontendtelemetry.GraphMode {
	switch value {
	case codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_PROGRAM:
		return frontendtelemetry.GraphModeProgram
	case codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_EXECUTION:
		return frontendtelemetry.GraphModeExecution
	case codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_EVIDENCE:
		return frontendtelemetry.GraphModeEvidence
	default:
		return ""
	}
}

func telemetryGraphModeToProto(value frontendtelemetry.GraphMode) codefluxv1.FrontendTelemetryGraphMode {
	switch value {
	case frontendtelemetry.GraphModeProgram:
		return codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_PROGRAM
	case frontendtelemetry.GraphModeExecution:
		return codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_EXECUTION
	case frontendtelemetry.GraphModeEvidence:
		return codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_EVIDENCE
	default:
		return codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_UNSPECIFIED
	}
}

var telemetryFailuresFromProto = map[codefluxv1.FrontendTelemetryFailureClass]frontendtelemetry.FailureClass{
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NONE:          frontendtelemetry.FailureNone,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_INPUT:         frontendtelemetry.FailureInput,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_CONFIGURATION: frontendtelemetry.FailureConfiguration,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_DATABASE:      frontendtelemetry.FailureDatabase,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NETWORK:       frontendtelemetry.FailureNetwork,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_AUTHORIZATION: frontendtelemetry.FailureAuthorization,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_INCOMPATIBLE:  frontendtelemetry.FailureIncompatible,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_PROJECTION:    frontendtelemetry.FailureProjection,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_RENDER:        frontendtelemetry.FailureRender,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_TIMEOUT:       frontendtelemetry.FailureTimeout,
	codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_UNKNOWN:       frontendtelemetry.FailureUnknown,
}

func telemetryFailureFromProto(value codefluxv1.FrontendTelemetryFailureClass) frontendtelemetry.FailureClass {
	return telemetryFailuresFromProto[value]
}
func telemetryFailureToProto(value frontendtelemetry.FailureClass) codefluxv1.FrontendTelemetryFailureClass {
	for protoValue, domainValue := range telemetryFailuresFromProto {
		if domainValue == value {
			return protoValue
		}
	}
	return codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_UNSPECIFIED
}

var _ codefluxv1.SettingsServiceServer = (*SettingsService)(nil)

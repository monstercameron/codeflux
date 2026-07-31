package main

import (
	"context"
	"errors"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/telemetryview"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

const mountedTelemetryPageLimit = 50

type frontendTelemetryListClient interface {
	ListFrontendTelemetry(context.Context, *codefluxv1.ListFrontendTelemetryRequest, ...grpc.CallOption) (*codefluxv1.ListFrontendTelemetryResponse, error)
}

type frontendTelemetryDeleteClient interface {
	DeleteFrontendTelemetry(context.Context, *codefluxv1.DeleteFrontendTelemetryRequest, ...grpc.CallOption) (*codefluxv1.DeleteFrontendTelemetryResponse, error)
}

type frontendTelemetryRecordClient interface {
	RecordFrontendTelemetry(context.Context, *codefluxv1.RecordFrontendTelemetryRequest, ...grpc.CallOption) (*codefluxv1.RecordFrontendTelemetryResponse, error)
}

type mountedTelemetryPage struct {
	Rows       []telemetryview.Row
	NextCursor string
	HasMore    bool
}

func listMountedFrontendTelemetry(
	ctx context.Context,
	client frontendTelemetryListClient,
	cursor string,
) (mountedTelemetryPage, error) {
	if client == nil {
		return mountedTelemetryPage{}, errors.New("telemetry client is required")
	}
	response, err := client.ListFrontendTelemetry(ctx, &codefluxv1.ListFrontendTelemetryRequest{
		Page: &codefluxv1.PageRequest{Cursor: cursor, Limit: mountedTelemetryPageLimit},
	})
	if err != nil {
		return mountedTelemetryPage{}, err
	}
	page := mountedTelemetryPage{Rows: make([]telemetryview.Row, 0, len(response.GetEvents()))}
	for _, event := range response.GetEvents() {
		if event == nil || event.GetOccurredAt() == nil || event.GetOccurredAt().CheckValid() != nil {
			return mountedTelemetryPage{}, errors.New("telemetry response contains an invalid event")
		}
		duration := time.Duration(0)
		if event.GetDuration() != nil {
			if err := event.GetDuration().CheckValid(); err != nil {
				return mountedTelemetryPage{}, errors.New("telemetry response contains an invalid duration")
			}
			duration = event.GetDuration().AsDuration()
		}
		page.Rows = append(page.Rows, telemetryview.Row{
			LocalID: event.GetLocalId(), Kind: telemetryKindLabel(event.GetKind()),
			Outcome: telemetryOutcomeLabel(event.GetOutcome()), Component: telemetryComponentLabel(event.GetComponent()),
			Occurred: event.GetOccurredAt().AsTime(), Duration: duration,
		})
	}
	if response.GetPage() != nil {
		page.NextCursor = response.GetPage().GetNextCursor()
		page.HasMore = response.GetPage().GetHasMore()
	}
	if page.HasMore && page.NextCursor == "" {
		return mountedTelemetryPage{}, errors.New("telemetry response omitted its continuation cursor")
	}
	return page, nil
}

func appendMountedTelemetry(current, next mountedTelemetryPage) mountedTelemetryPage {
	rows := make([]telemetryview.Row, 0, len(current.Rows)+len(next.Rows))
	rows = append(rows, current.Rows...)
	rows = append(rows, next.Rows...)
	return mountedTelemetryPage{Rows: rows, NextCursor: next.NextCursor, HasMore: next.HasMore}
}

func deleteAllMountedFrontendTelemetry(ctx context.Context, client frontendTelemetryDeleteClient, key string) error {
	if client == nil || key == "" {
		return errors.New("telemetry deletion requires a client and delivery identity")
	}
	_, err := client.DeleteFrontendTelemetry(ctx, &codefluxv1.DeleteFrontendTelemetryRequest{
		Control:      &codefluxv1.MutationControl{IdempotencyKey: key},
		Scope:        codefluxv1.FrontendTelemetryDeleteScope_FRONTEND_TELEMETRY_DELETE_SCOPE_ALL,
		Confirmation: codefluxv1.FrontendTelemetryDeleteConfirmation_FRONTEND_TELEMETRY_DELETE_CONFIRMATION_CONFIRMED,
	})
	return err
}

func recordMountedFrontendTelemetry(
	ctx context.Context,
	client frontendTelemetryRecordClient,
	key string,
	event *codefluxv1.FrontendTelemetryEvent,
) error {
	if client == nil || key == "" || event == nil {
		return errors.New("telemetry record requires a client, delivery identity, and event")
	}
	_, err := client.RecordFrontendTelemetry(ctx, &codefluxv1.RecordFrontendTelemetryRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: key}, Event: event,
	})
	return err
}

func telemetryKindLabel(value codefluxv1.FrontendTelemetryKind) string {
	switch value {
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FIRST_RUN_STEP:
		return "first-run step"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_THREAD:
		return "time to thread"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_MESSAGE:
		return "time to message"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_PLAN:
		return "time to plan"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_DIFF:
		return "time to diff"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_PLAN_DECISION:
		return "plan decision"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_APPROVAL_DECISION:
		return "approval decision"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL:
		return "task control"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_REVIEW_DECISION:
		return "review decision"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_GRAPH_INTERACTION:
		return "graph interaction"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_MEMORY_INTERACTION:
		return "memory interaction"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECONNECT:
		return "reconnect"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECOVERY_ACTION:
		return "recovery action"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FRONTEND_ERROR:
		return "frontend error"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_LONG_TASK:
		return "long task"
	case codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_SLOW_RENDER:
		return "slow render"
	default:
		return "unknown event"
	}
}

func telemetryOutcomeLabel(value codefluxv1.FrontendTelemetryOutcome) string {
	labels := map[codefluxv1.FrontendTelemetryOutcome]string{
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED:          "succeeded",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_FAILED:             "failed",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_CANCELLED:          "cancelled",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_APPROVED:           "approved",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_DENIED:             "denied",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_EXPIRED:            "expired",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REVISION_REQUESTED: "revision requested",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_PAUSED:             "paused",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_STOPPED:            "stopped",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_OPENED:             "opened",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_NAVIGATED:          "navigated",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_INSPECTED:          "inspected",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_CORRECTED:          "corrected",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ACCEPTED:           "accepted",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REPAIR_REQUESTED:   "repair requested",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REJECTED:           "rejected",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ROLLED_BACK:        "rolled back",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_RECONNECTED:        "reconnected",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SAFE_RESUMED:       "safe resumed",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_RECONCILED:         "reconciled",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_PATCH_PRESERVED:    "patch preserved",
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ABANDONED:          "abandoned",
	}
	if label := labels[value]; label != "" {
		return label
	}
	return "unknown outcome"
}

func telemetryComponentLabel(value codefluxv1.FrontendTelemetryComponent) string {
	labels := map[codefluxv1.FrontendTelemetryComponent]string{
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_FIRST_RUN: "first run",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_THREAD:    "thread",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_COMPOSER:  "composer",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_PLAN:      "plan",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_DIFF:      "diff",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_APPROVAL:  "approval",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TOP_BAR:   "top bar",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_REVIEW:    "review",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_GRAPH:     "graph",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_MEMORY:    "memory",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_SESSION:   "session",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_RECOVERY:  "recovery",
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TIMELINE:  "timeline",
	}
	if label := labels[value]; label != "" {
		return label
	}
	return "unknown component"
}

type frontendTelemetrySink func(*codefluxv1.FrontendTelemetryEvent)

func taskTelemetryIdentity(taskID domain.TaskID) *codefluxv1.StableIdentity {
	if taskID.IsZero() {
		return nil
	}
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: taskID.String()}
}

func contentFreeTelemetryEvent(
	kind codefluxv1.FrontendTelemetryKind,
	outcome codefluxv1.FrontendTelemetryOutcome,
	component codefluxv1.FrontendTelemetryComponent,
	taskID domain.TaskID,
) *codefluxv1.FrontendTelemetryEvent {
	return &codefluxv1.FrontendTelemetryEvent{
		Kind: kind, Outcome: outcome, Component: component,
		FailureClass: codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NONE,
		TaskId:       taskTelemetryIdentity(taskID),
	}
}

// firstRunFailureTelemetry classifies startup and typed first-run action
// failures into the closed telemetry vocabulary. It records no path, message,
// repository identity, or user content.
func firstRunFailureTelemetry(err error) *codefluxv1.FrontendTelemetryEvent {
	if err == nil {
		return nil
	}
	failureClass := codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_UNKNOWN
	var failure *startupFailure
	switch {
	case errors.Is(err, routes.ErrInvalidRoute):
		failureClass = codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_INPUT
	case errors.As(err, &failure):
		switch failure.Kind {
		case startupDatabase:
			failureClass = codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_DATABASE
		case startupMigration:
			failureClass = codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_INCOMPATIBLE
		case startupCoordinator:
			if errors.Is(failure.Cause, context.DeadlineExceeded) || errors.Is(failure.Cause, context.Canceled) {
				failureClass = codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_TIMEOUT
			} else {
				failureClass = codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NETWORK
			}
		}
	}
	event := contentFreeTelemetryEvent(
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FIRST_RUN_STEP,
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_FAILED,
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_FIRST_RUN,
		domain.TaskID{},
	)
	event.FailureClass = failureClass
	return event
}

func instrumentTaskControlTelemetry(props *taskcontrols.Props, sink frontendTelemetrySink) {
	if props == nil || sink == nil {
		return
	}
	wrap := func(callback func(), outcome codefluxv1.FrontendTelemetryOutcome, kind codefluxv1.FrontendTelemetryKind, component codefluxv1.FrontendTelemetryComponent) func() {
		if callback == nil {
			return nil
		}
		return func() { sink(contentFreeTelemetryEvent(kind, outcome, component, props.TaskID)); callback() }
	}
	props.OnPause = wrap(props.OnPause, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_PAUSED, codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TOP_BAR)
	props.OnResume = wrap(props.OnResume, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED, codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TOP_BAR)
	props.OnStop = wrap(props.OnStop, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_STOPPED, codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TOP_BAR)
	props.OnSafeResume = wrap(props.OnSafeResume, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SAFE_RESUMED, codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECOVERY_ACTION, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_RECOVERY)
	props.OnReconcile = wrap(props.OnReconcile, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_RECONCILED, codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECOVERY_ACTION, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_RECOVERY)
	props.OnPreservePatch = wrap(props.OnPreservePatch, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_PATCH_PRESERVED, codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECOVERY_ACTION, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_RECOVERY)
}

func instrumentTimelineTelemetry(props *shell.TimelineControlProps, taskID domain.TaskID, sink frontendTelemetrySink) {
	if props == nil || sink == nil {
		return
	}
	if original := props.OnOpenReview; original != nil {
		props.OnOpenReview = func() {
			sink(contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_REVIEW_DECISION, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_OPENED, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_REVIEW, taskID))
			original()
		}
	}
	actions := &props.Actions
	if original := actions.OnApprovePlan; original != nil {
		actions.OnApprovePlan = func(revision uint64) {
			event := contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_PLAN_DECISION, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_APPROVED, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_PLAN, taskID)
			event.Revision = revision
			sink(event)
			original(revision)
		}
	}
	if original := actions.OnRequestPlanChange; original != nil {
		actions.OnRequestPlanChange = func(revision uint64) {
			event := contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_PLAN_DECISION, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REVISION_REQUESTED, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_PLAN, taskID)
			event.Revision = revision
			sink(event)
			original(revision)
		}
	}
	if original := actions.OnApproval; original != nil {
		actions.OnApproval = func(id string, action timelinecard.ApprovalAction) {
			outcome := codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_APPROVED
			if action == timelinecard.ApprovalDeny {
				outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_DENIED
			}
			sink(contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_APPROVAL_DECISION, outcome, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_APPROVAL, taskID))
			original(id, action)
		}
	}
	if original := actions.OnSelectNode; original != nil {
		actions.OnSelectNode = func(id string) {
			event := contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_GRAPH_INTERACTION, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_NAVIGATED, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_GRAPH, taskID)
			event.GraphMode = codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_PROGRAM
			sink(event)
			original(id)
		}
	}
}

func telemetryForSessionEvent(event events.SessionEvent, elapsed time.Duration) []*codefluxv1.FrontendTelemetryEvent {
	if event.TaskID == nil || event.TaskID.IsZero() {
		return nil
	}
	if elapsed <= 0 {
		elapsed = time.Microsecond
	}
	withTiming := func(value *codefluxv1.FrontendTelemetryEvent) *codefluxv1.FrontendTelemetryEvent {
		value.Duration = durationpb.New(elapsed)
		value.Sequence = event.Sequence
		value.Revision = event.Revision
		return value
	}
	switch event.Kind {
	case events.KindPlanCreated:
		return []*codefluxv1.FrontendTelemetryEvent{withTiming(contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_PLAN, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_PLAN, *event.TaskID))}
	case events.KindApprovalResolved:
		if event.Payload.Approval == nil {
			return nil
		}
		var outcome codefluxv1.FrontendTelemetryOutcome
		switch event.Payload.Approval.State {
		case domain.ApprovalRequestStateGranted:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_APPROVED
		case domain.ApprovalRequestStateDenied:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_DENIED
		case domain.ApprovalRequestStateExpired:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_EXPIRED
		case domain.ApprovalRequestStateCancelled:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_CANCELLED
		default:
			return nil
		}
		decision := contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_APPROVAL_DECISION, outcome, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_APPROVAL, *event.TaskID)
		decision.Sequence, decision.Revision = event.Sequence, event.Revision
		return []*codefluxv1.FrontendTelemetryEvent{decision}
	case events.KindChangeAcceptanceUpdated:
		if event.Payload.ChangeAcceptance == nil {
			return nil
		}
		outcome := codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_OPENED
		switch event.Payload.ChangeAcceptance.State {
		case domain.ChangeAcceptanceStateAccepted:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ACCEPTED
		case domain.ChangeAcceptanceStateRepairRequested:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REPAIR_REQUESTED
		case domain.ChangeAcceptanceStateRejected:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_REJECTED
		case domain.ChangeAcceptanceStateRolledBack:
			outcome = codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_ROLLED_BACK
		}
		review := contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_REVIEW_DECISION, outcome, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_REVIEW, *event.TaskID)
		review.Sequence, review.Revision = event.Sequence, event.Revision
		if event.Payload.ChangeAcceptance.State == domain.ChangeAcceptanceStatePending {
			return []*codefluxv1.FrontendTelemetryEvent{
				withTiming(contentFreeTelemetryEvent(codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_DIFF, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED, codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_DIFF, *event.TaskID)),
				review,
			}
		}
		return []*codefluxv1.FrontendTelemetryEvent{review}
	}
	return nil
}

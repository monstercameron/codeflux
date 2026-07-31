package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFirstRunFailureTelemetryUsesClosedContentFreeClassification(t *testing.T) {
	invalidRouteErr := func() error {
		_, err := routes.TaskSelectionPath(routes.Route{Name: routes.Settings})
		return err
	}()
	tests := []struct {
		name string
		err  error
		want codefluxv1.FrontendTelemetryFailureClass
	}{
		{"input", invalidRouteErr, codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_INPUT},
		{"database", &startupFailure{Kind: startupDatabase}, codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_DATABASE},
		{"migration", &startupFailure{Kind: startupMigration}, codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_INCOMPATIBLE},
		{"timeout", &startupFailure{Kind: startupCoordinator, Cause: context.DeadlineExceeded}, codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_TIMEOUT},
		{"network", &startupFailure{Kind: startupCoordinator, Cause: errors.New("unavailable")}, codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NETWORK},
		{"unknown", errors.New("unclassified"), codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_UNKNOWN},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := firstRunFailureTelemetry(test.err)
			if event == nil ||
				event.GetKind() != codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FIRST_RUN_STEP ||
				event.GetOutcome() != codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_FAILED ||
				event.GetComponent() != codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_FIRST_RUN ||
				event.GetFailureClass() != test.want {
				t.Fatalf("first-run failure telemetry = %+v", event)
			}
			if event.GetTaskId() != nil || event.GetThreadId() != nil || event.GetSessionId() != nil ||
				event.GetSequence() != 0 || event.GetRevision() != 0 || event.GetDuration() != nil {
				t.Fatalf("first-run failure telemetry leaked contextual identity or timing: %+v", event)
			}
		})
	}
	if event := firstRunFailureTelemetry(nil); event != nil {
		t.Fatalf("nil failure emitted telemetry: %+v", event)
	}
}

type telemetryClientFake struct {
	listRequest   *codefluxv1.ListFrontendTelemetryRequest
	deleteRequest *codefluxv1.DeleteFrontendTelemetryRequest
	recordRequest *codefluxv1.RecordFrontendTelemetryRequest
}

func (fake *telemetryClientFake) ListFrontendTelemetry(_ context.Context, request *codefluxv1.ListFrontendTelemetryRequest, _ ...grpc.CallOption) (*codefluxv1.ListFrontendTelemetryResponse, error) {
	fake.listRequest = request
	return &codefluxv1.ListFrontendTelemetryResponse{
		Events: []*codefluxv1.FrontendTelemetryEvent{{
			LocalId: 9, Kind: codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_SLOW_RENDER,
			Outcome:    codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED,
			Component:  codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_GRAPH,
			OccurredAt: timestamppb.New(time.Unix(20, 0)), Duration: durationpb.New(75 * time.Millisecond),
		}}, Page: &codefluxv1.PageInfo{HasMore: true, NextCursor: "opaque"},
	}, nil
}

func (fake *telemetryClientFake) DeleteFrontendTelemetry(_ context.Context, request *codefluxv1.DeleteFrontendTelemetryRequest, _ ...grpc.CallOption) (*codefluxv1.DeleteFrontendTelemetryResponse, error) {
	fake.deleteRequest = request
	return &codefluxv1.DeleteFrontendTelemetryResponse{Deleted: 1}, nil
}

func (fake *telemetryClientFake) RecordFrontendTelemetry(_ context.Context, request *codefluxv1.RecordFrontendTelemetryRequest, _ ...grpc.CallOption) (*codefluxv1.RecordFrontendTelemetryResponse, error) {
	fake.recordRequest = request
	return &codefluxv1.RecordFrontendTelemetryResponse{Event: request.GetEvent()}, nil
}

func TestMountedTelemetryListDeleteAndRecordUseExactTypedRequests(t *testing.T) {
	client := &telemetryClientFake{}
	page, err := listMountedFrontendTelemetry(t.Context(), client, "previous")
	if err != nil {
		t.Fatal(err)
	}
	if client.listRequest.GetPage().GetCursor() != "previous" || client.listRequest.GetPage().GetLimit() != mountedTelemetryPageLimit ||
		len(page.Rows) != 1 || page.Rows[0].Kind != "slow render" || page.Rows[0].Duration != 75*time.Millisecond || !page.HasMore {
		t.Fatalf("list request/page = %#v / %#v", client.listRequest, page)
	}
	if err := deleteAllMountedFrontendTelemetry(t.Context(), client, "delete-key"); err != nil {
		t.Fatal(err)
	}
	if client.deleteRequest.GetConfirmation() != codefluxv1.FrontendTelemetryDeleteConfirmation_FRONTEND_TELEMETRY_DELETE_CONFIRMATION_CONFIRMED ||
		client.deleteRequest.GetScope() != codefluxv1.FrontendTelemetryDeleteScope_FRONTEND_TELEMETRY_DELETE_SCOPE_ALL {
		t.Fatalf("delete request = %#v", client.deleteRequest)
	}
	event := &codefluxv1.FrontendTelemetryEvent{
		Kind:         codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_GRAPH_INTERACTION,
		Outcome:      codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_OPENED,
		Component:    codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_GRAPH,
		GraphMode:    codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_PROGRAM,
		FailureClass: codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NONE,
	}
	if err := recordMountedFrontendTelemetry(t.Context(), client, "record-key", event); err != nil {
		t.Fatal(err)
	}
	if client.recordRequest.GetEvent() != event || client.recordRequest.GetControl().GetIdempotencyKey() != "record-key" {
		t.Fatalf("record request = %#v", client.recordRequest)
	}
}

func TestAppendMountedTelemetryPreservesOrderAndCursor(t *testing.T) {
	first := mountedTelemetryPage{Rows: nil, NextCursor: "one", HasMore: true}
	second := mountedTelemetryPage{NextCursor: "two", HasMore: false}
	got := appendMountedTelemetry(first, second)
	if got.NextCursor != "two" || got.HasMore {
		t.Fatalf("appended page = %#v", got)
	}
}

func TestTelemetryInstrumentationCoversNamedInteractionFamiliesWithoutContent(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	var recorded []*codefluxv1.FrontendTelemetryEvent
	sink := func(event *codefluxv1.FrontendTelemetryEvent) { recorded = append(recorded, event) }
	controls := taskcontrols.Props{
		TaskID:  taskID,
		OnPause: func() {}, OnResume: func() {}, OnStop: func() {}, OnStopConfirm: func() {}, OnSafeResume: func() {},
		OnReconcile: func() {}, OnPreservePatch: func() {},
	}
	instrumentTaskControlTelemetry(&controls, sink)
	controls.OnPause()
	controls.OnResume()
	controls.OnStop()
	controls.OnStopConfirm()
	controls.OnSafeResume()
	controls.OnReconcile()
	controls.OnPreservePatch()
	timeline := shell.TimelineControlProps{
		OnOpenReview: func() {},
		Actions:      timelineview.Actions{},
	}
	// Assign through the named action fields to keep the test independent of
	// presentation text and captured identifiers.
	timeline.Actions.OnApprovePlan = func(uint64) {}
	timeline.Actions.OnRequestPlanChange = func(uint64) {}
	timeline.Actions.OnApproval = func(string, timelinecard.ApprovalAction) {}
	timeline.Actions.OnSelectNode = func(string) {}
	instrumentTimelineTelemetry(&timeline, taskID, sink)
	timeline.OnOpenReview()
	timeline.Actions.OnApprovePlan(3)
	timeline.Actions.OnRequestPlanChange(4)
	timeline.Actions.OnApproval("not-recorded", timelinecard.ApprovalDeny)
	timeline.Actions.OnSelectNode("not-recorded")

	plan := events.SessionEvent{Kind: events.KindPlanCreated, TaskID: &taskID, Sequence: 10, Revision: 2}
	recorded = append(recorded, telemetryForSessionEvent(plan, 40*time.Millisecond)...)
	change := events.SessionEvent{Kind: events.KindChangeAcceptanceUpdated, TaskID: &taskID, Sequence: 11, Revision: 3,
		Payload: events.Payload{ChangeAcceptance: &events.ChangeAcceptance{State: domain.ChangeAcceptanceStatePending}}}
	recorded = append(recorded, telemetryForSessionEvent(change, 60*time.Millisecond)...)

	wantKinds := map[codefluxv1.FrontendTelemetryKind]bool{
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL:      false,
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECOVERY_ACTION:   false,
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_PLAN_DECISION:     false,
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_APPROVAL_DECISION: false,
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_REVIEW_DECISION:   false,
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_GRAPH_INTERACTION: false,
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_PLAN:      false,
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TIME_TO_DIFF:      false,
	}
	for _, event := range recorded {
		wantKinds[event.GetKind()] = true
	}
	for kind, observed := range wantKinds {
		if !observed {
			t.Errorf("named telemetry kind %s was not instrumented", kind)
		}
	}
}

func TestApprovalResolutionTelemetryUsesOnlyTerminalStateAndTaskAttribution(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	approvalID, _ := domain.NewApprovalID()
	const (
		scopeSecret  = "scope-must-not-cross-telemetry-boundary"
		reasonSecret = "reason-must-not-cross-telemetry-boundary"
	)
	tests := []struct {
		state domain.ApprovalRequestState
		want  codefluxv1.FrontendTelemetryOutcome
	}{
		{domain.ApprovalRequestStateGranted, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_APPROVED},
		{domain.ApprovalRequestStateDenied, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_DENIED},
		{domain.ApprovalRequestStateExpired, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_EXPIRED},
		{domain.ApprovalRequestStateCancelled, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_CANCELLED},
	}
	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			source := events.SessionEvent{
				Kind: events.KindApprovalResolved, TaskID: &taskID, Sequence: 41, Revision: 7,
				Payload: events.Payload{Approval: &events.Approval{
					ApprovalID: approvalID, State: test.state, Scope: scopeSecret, RedactedReason: reasonSecret,
				}},
			}
			got := telemetryForSessionEvent(source, time.Second)
			if len(got) != 1 {
				t.Fatalf("approval telemetry count = %d, want 1", len(got))
			}
			event := got[0]
			if event.GetKind() != codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_APPROVAL_DECISION ||
				event.GetOutcome() != test.want ||
				event.GetComponent() != codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_APPROVAL ||
				event.GetFailureClass() != codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NONE ||
				event.GetTaskId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK ||
				event.GetTaskId().GetValue() != taskID.String() || event.GetSequence() != 41 || event.GetRevision() != 7 {
				t.Fatalf("approval telemetry = %+v", event)
			}
			if event.GetOccurredAt() != nil || event.GetDuration() != nil || event.GetThreadId() != nil ||
				event.GetSessionId() != nil || event.GetGraphMode() != codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_UNSPECIFIED ||
				event.GetLocalId() != 0 {
				t.Fatalf("approval telemetry copied non-attribution context: %+v", event)
			}
			encoded, err := protojson.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{approvalID.String(), scopeSecret, reasonSecret} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("approval telemetry copied approval content %q: %s", forbidden, encoded)
				}
			}
		})
	}

	for _, source := range []events.SessionEvent{
		{Kind: events.KindApprovalResolved, TaskID: &taskID},
		{Kind: events.KindApprovalResolved, TaskID: &taskID, Payload: events.Payload{Approval: &events.Approval{State: domain.ApprovalRequestStatePending}}},
		{Kind: events.KindApprovalRequested, TaskID: &taskID, Payload: events.Payload{Approval: &events.Approval{State: domain.ApprovalRequestStateGranted}}},
	} {
		if got := telemetryForSessionEvent(source, time.Second); got != nil {
			t.Fatalf("non-terminal or non-resolution event emitted approval telemetry: %+v", got)
		}
	}
}

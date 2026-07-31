package frontendtelemetry_test

import (
	"reflect"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/frontendtelemetry"
)

const telemetryUUID = "019fb8c8-670d-796c-8569-7d3252348b52"

func TestTelemetryShapesCoverEveryDeclaredKind(t *testing.T) {
	task, _ := domain.ParseTaskID("tsk_" + telemetryUUID)
	session, _ := domain.ParseSessionID("ses_" + telemetryUUID)
	valid := map[frontendtelemetry.Kind]frontendtelemetry.Event{
		frontendtelemetry.KindFirstRunStep:      {Component: frontendtelemetry.ComponentFirstRun, Outcome: frontendtelemetry.OutcomeSucceeded},
		frontendtelemetry.KindTimeToThread:      {Component: frontendtelemetry.ComponentThread, Outcome: frontendtelemetry.OutcomeSucceeded, Duration: time.Second},
		frontendtelemetry.KindTimeToMessage:     {Component: frontendtelemetry.ComponentComposer, Outcome: frontendtelemetry.OutcomeSucceeded, Duration: time.Second},
		frontendtelemetry.KindTimeToPlan:        {TaskID: task, Component: frontendtelemetry.ComponentPlan, Outcome: frontendtelemetry.OutcomeSucceeded, Duration: time.Second},
		frontendtelemetry.KindTimeToDiff:        {TaskID: task, Component: frontendtelemetry.ComponentDiff, Outcome: frontendtelemetry.OutcomeSucceeded, Duration: time.Second},
		frontendtelemetry.KindPlanDecision:      {TaskID: task, Component: frontendtelemetry.ComponentPlan, Outcome: frontendtelemetry.OutcomeApproved},
		frontendtelemetry.KindApprovalDecision:  {TaskID: task, Component: frontendtelemetry.ComponentApproval, Outcome: frontendtelemetry.OutcomeDenied},
		frontendtelemetry.KindTaskControl:       {TaskID: task, Component: frontendtelemetry.ComponentTopBar, Outcome: frontendtelemetry.OutcomePaused},
		frontendtelemetry.KindReviewDecision:    {TaskID: task, Component: frontendtelemetry.ComponentReview, Outcome: frontendtelemetry.OutcomeAccepted},
		frontendtelemetry.KindGraphInteraction:  {TaskID: task, Component: frontendtelemetry.ComponentGraph, Outcome: frontendtelemetry.OutcomeNavigated, GraphMode: frontendtelemetry.GraphModeEvidence},
		frontendtelemetry.KindMemoryInteraction: {Component: frontendtelemetry.ComponentMemory, Outcome: frontendtelemetry.OutcomeInspected},
		frontendtelemetry.KindReconnect:         {SessionID: session, Component: frontendtelemetry.ComponentSession, Outcome: frontendtelemetry.OutcomeReconnected, Duration: time.Second},
		frontendtelemetry.KindRecoveryAction:    {TaskID: task, Component: frontendtelemetry.ComponentRecovery, Outcome: frontendtelemetry.OutcomePatchPreserved},
		frontendtelemetry.KindFrontendError:     {Component: frontendtelemetry.ComponentTimeline, Outcome: frontendtelemetry.OutcomeFailed, FailureClass: frontendtelemetry.FailureProjection},
		frontendtelemetry.KindLongTask:          {TaskID: task, Component: frontendtelemetry.ComponentTimeline, Outcome: frontendtelemetry.OutcomeSucceeded, Duration: time.Minute},
		frontendtelemetry.KindSlowRender:        {Component: frontendtelemetry.ComponentGraph, Outcome: frontendtelemetry.OutcomeSucceeded, Duration: 50 * time.Millisecond},
	}
	if len(valid) != len(frontendtelemetry.AllKinds()) {
		t.Fatalf("fixtures=%d declared=%d", len(valid), len(frontendtelemetry.AllKinds()))
	}
	for _, kind := range frontendtelemetry.AllKinds() {
		event, ok := valid[kind]
		if !ok {
			t.Fatalf("missing fixture for %s", kind)
		}
		event.Kind = kind
		if err := event.ValidateForRecord(); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}

func TestTelemetryRejectsContentLikeAndInvalidShapesStructurally(t *testing.T) {
	typeOfEvent := reflect.TypeOf(frontendtelemetry.Event{})
	for _, forbidden := range []string{"Body", "Content", "Prompt", "Keystroke", "Output", "Metadata", "Message"} {
		if _, exists := typeOfEvent.FieldByName(forbidden); exists {
			t.Fatalf("telemetry exposes forbidden field %s", forbidden)
		}
	}
	tests := []frontendtelemetry.Event{
		{Kind: frontendtelemetry.KindSlowRender, Component: frontendtelemetry.ComponentGraph, Outcome: frontendtelemetry.OutcomeSucceeded, Duration: 49 * time.Millisecond},
		{Kind: frontendtelemetry.KindFrontendError, Component: frontendtelemetry.ComponentGraph, Outcome: frontendtelemetry.OutcomeFailed},
		{Kind: frontendtelemetry.KindGraphInteraction, Component: frontendtelemetry.ComponentGraph, Outcome: frontendtelemetry.OutcomeOpened},
		{Kind: frontendtelemetry.KindTaskControl, Component: frontendtelemetry.ComponentTopBar, Outcome: frontendtelemetry.OutcomeStopped},
	}
	for index, event := range tests {
		if err := event.ValidateForRecord(); err == nil {
			t.Errorf("invalid fixture %d was accepted: %#v", index, event)
		}
	}
}

func TestRedactedDiagnosticOmitsStableIdentities(t *testing.T) {
	task, _ := domain.ParseTaskID("tsk_" + telemetryUUID)
	thread, _ := domain.ParseThreadID("thr_" + telemetryUUID)
	session, _ := domain.ParseSessionID("ses_" + telemetryUUID)
	diagnostic := (frontendtelemetry.Event{
		Kind: frontendtelemetry.KindFrontendError, Component: frontendtelemetry.ComponentSession,
		Outcome: frontendtelemetry.OutcomeFailed, FailureClass: frontendtelemetry.FailureNetwork,
		TaskID: task, ThreadID: thread, SessionID: session, Sequence: 44, Revision: 3,
	}).RedactedDiagnostic()
	if !diagnostic.HasTask || !diagnostic.HasThread || !diagnostic.HasSession ||
		!diagnostic.HasSequence || !diagnostic.HasRevision {
		t.Fatalf("diagnostic lost safe presence flags: %#v", diagnostic)
	}
	typeOfDiagnostic := reflect.TypeOf(diagnostic)
	for _, forbidden := range []string{"TaskID", "ThreadID", "SessionID", "Sequence", "Revision"} {
		if _, exists := typeOfDiagnostic.FieldByName(forbidden); exists {
			t.Fatalf("redacted diagnostic exposes %s", forbidden)
		}
	}
}

func TestQueryAndDeletionValidationAreBoundedAndExplicit(t *testing.T) {
	if err := (frontendtelemetry.Query{Limit: frontendtelemetry.MaxQueryLimit + 1}).Validate(); err == nil {
		t.Fatal("unbounded query was accepted")
	}
	if err := (frontendtelemetry.Query{Kinds: []frontendtelemetry.Kind{frontendtelemetry.KindSlowRender, frontendtelemetry.KindSlowRender}}).Validate(); err == nil {
		t.Fatal("duplicate kind filters were accepted")
	}
	if err := (frontendtelemetry.DeleteRequest{Scope: frontendtelemetry.DeleteAll}).Validate(); err == nil {
		t.Fatal("unconfirmed deletion was accepted")
	}
	if err := (frontendtelemetry.DeleteRequest{
		Scope: frontendtelemetry.DeleteAll, Confirmation: frontendtelemetry.ConfirmTelemetryDeletion,
	}).Validate(); err != nil {
		t.Fatal(err)
	}
}

package timelinecard

import (
	"fmt"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

var fixedCardTime = time.Date(2026, 7, 31, 14, 30, 0, 0, time.UTC)

func fixedEventFixture(t *testing.T) []events.SessionEvent {
	t.Helper()
	messageID := mustMessageID(t, 1)
	workspaceID := mustWorkspaceID(t)
	approvalID := mustApprovalID(t, 1)
	validationID := mustValidationID(t, 1)
	graphID := mustGraphRevisionID(t, 1)
	checkpointID := mustCheckpointID(t, 1)
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money := func(value int64) domain.Money {
		result, moneyErr := domain.NewMoney(currency, value)
		if moneyErr != nil {
			t.Fatal(moneyErr)
		}
		return result
	}
	payloads := []struct {
		kind    events.Kind
		payload events.Payload
	}{
		{events.KindMessageDelta, events.Payload{MessageDelta: &events.MessageDelta{MessageID: messageID, RedactedDelta: "Working"}}},
		{events.KindMessageFinal, events.Payload{MessageFinal: &events.MessageFinal{MessageID: messageID, Role: "assistant", RedactedBody: "Done"}}},
		{events.KindThreadCreated, events.Payload{ThreadCreated: &events.ThreadCreated{WorkspaceID: &workspaceID, Title: "Thread"}}},
		{events.KindThreadRenamed, events.Payload{ThreadRenamed: &events.ThreadRenamed{PreviousTitle: "Thread", Title: "Renamed"}}},
		{events.KindThreadArchived, events.Payload{ThreadArchived: &events.ThreadArchived{Archived: true}}},
		{events.KindPlanCreated, events.Payload{Plan: &events.Plan{Revision: 1, RedactedSummary: "Initial plan"}}},
		{events.KindPlanChanged, events.Payload{Plan: &events.Plan{Revision: 2, RedactedSummary: "Revised plan"}}},
		{events.KindToolStarted, events.Payload{Tool: &events.Tool{ExecutionID: "exec-1", CommandName: "go-test", State: "running", RedactedSummary: "started"}}},
		{events.KindToolProgress, events.Payload{Tool: &events.Tool{ExecutionID: "exec-1", CommandName: "go-test", State: "running", RedactedSummary: "halfway"}}},
		{events.KindToolCompleted, events.Payload{Tool: &events.Tool{ExecutionID: "exec-1", CommandName: "go-test", State: "passed", RedactedSummary: "passed"}}},
		{events.KindApprovalRequested, events.Payload{Approval: &events.Approval{ApprovalID: approvalID, State: domain.ApprovalRequestStatePending, Scope: "repository", RedactedReason: "write"}}},
		{events.KindApprovalResolved, events.Payload{Approval: &events.Approval{ApprovalID: approvalID, State: domain.ApprovalRequestStateGranted, Scope: "repository", RedactedReason: "allowed once"}}},
		{events.KindTaskStateChanged, events.Payload{TaskStateChanged: &events.TaskStateChanged{From: domain.TaskStateDraft, To: domain.TaskStateForecasting, Approval: domain.ApprovalRequestStatePending}}},
		{events.KindForecastUpdated, events.Payload{Forecast: &events.Forecast{Range: domain.ForecastRange{CostKnown: true, CostP50: money(100), CostP90: money(200)}}}},
		{events.KindUsageUpdated, events.Payload{Usage: &events.Usage{Tokens: domain.TokenUsage{Known: true, Input: 100, Output: 20}}}},
		{events.KindCostUpdated, events.Payload{Cost: &events.Cost{Known: true, Value: money(42)}}},
		{events.KindBudgetUpdated, events.Payload{Budget: &events.Budget{HardLimit: money(1000), Reserved: money(100), Actual: money(200)}}},
		{events.KindValidationUpdated, events.Payload{Validation: &events.Validation{ValidationID: validationID, State: domain.ValidationStatePassed, RedactedSummary: "all checks passed"}}},
		{events.KindGraphSnapshot, events.Payload{Graph: &events.Graph{RevisionID: graphID, EncodedChange: []byte{1}}}},
		{events.KindGraphPatch, events.Payload{Graph: &events.Graph{RevisionID: graphID, EncodedChange: []byte{2}}}},
		{events.KindCheckpointCreated, events.Payload{Checkpoint: &events.Checkpoint{CheckpointID: checkpointID, TaskRevision: 3}}},
		{events.KindRecoveryRequired, events.Payload{RecoveryRequired: &events.RecoveryRequired{CheckpointID: &checkpointID, RedactedReason: "worktree changed"}}},
		{events.KindError, events.Payload{Error: &events.UserError{Code: events.ErrorCodeConflict, RedactedMessage: "state changed", Retryable: true}}},
	}
	result := make([]events.SessionEvent, len(payloads))
	for index, fixture := range payloads {
		built, buildErr := (events.NewSessionEvent{
			SessionID: mustSessionID(t), ThreadID: mustThreadID(t), Kind: fixture.kind,
			PayloadVersion: 1, Payload: fixture.payload,
		}).Build(uint64(index+1), fixedCardTime.Add(time.Duration(index)*time.Microsecond))
		if buildErr != nil {
			t.Fatalf("build %s fixture: %v", fixture.kind, buildErr)
		}
		result[index] = built
	}
	return result
}

func mustSessionID(t *testing.T) domain.SessionID {
	t.Helper()
	value, err := domain.ParseSessionID("ses_018f0123-4567-789a-8bcd-ef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustThreadID(t *testing.T) domain.ThreadID {
	t.Helper()
	value, err := domain.ParseThreadID("thr_018f0123-4567-789a-8bcd-ef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustWorkspaceID(t *testing.T) domain.WorkspaceID {
	t.Helper()
	value, err := domain.ParseWorkspaceID("wsp_018f0123-4567-789a-8bcd-ef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustMessageID(t *testing.T, ordinal uint64) domain.MessageID {
	t.Helper()
	value, err := domain.ParseMessageID(fmt.Sprintf("msg_018f0123-4567-789a-8bcd-%012d", ordinal))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustApprovalID(t *testing.T, ordinal uint64) domain.ApprovalID {
	t.Helper()
	value, err := domain.ParseApprovalID(fmt.Sprintf("apr_018f0123-4567-789a-8bcd-%012d", ordinal))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustValidationID(t *testing.T, ordinal uint64) domain.ValidationID {
	t.Helper()
	value, err := domain.ParseValidationID(fmt.Sprintf("val_018f0123-4567-789a-8bcd-%012d", ordinal))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustGraphRevisionID(t *testing.T, ordinal uint64) domain.GraphRevisionID {
	t.Helper()
	value, err := domain.ParseGraphRevisionID(fmt.Sprintf("grv_018f0123-4567-789a-8bcd-%012d", ordinal))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustCheckpointID(t *testing.T, ordinal uint64) domain.CheckpointID {
	t.Helper()
	value, err := domain.ParseCheckpointID(fmt.Sprintf("ckp_018f0123-4567-789a-8bcd-%012d", ordinal))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

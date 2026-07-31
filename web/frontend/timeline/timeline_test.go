package timeline

import (
	"fmt"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

var fixedTimelineTime = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func TestMergeThreadPageJoinsPaginationAndReplayAtEveryBoundary(t *testing.T) {
	all := make([]events.SessionEvent, 12)
	for index := range all {
		sequence := uint64(index + 1)
		all[index] = finalEvent(t, sequence, messageID(t, sequence), fmt.Sprintf("message %d", sequence))
	}
	for boundary := 1; boundary < len(all); boundary++ {
		t.Run(fmt.Sprintf("boundary-%d", boundary), func(t *testing.T) {
			state, err := MergeThreadPage(State{}, Page{Events: all[boundary:], HasOlder: true})
			if err != nil {
				t.Fatal(err)
			}
			// Replay deliberately overlaps both pages at the join.
			state, err = MergeThreadPage(state, Page{Events: all[boundary-1 : boundary+1], HasOlder: true})
			if err != nil {
				t.Fatal(err)
			}
			state, err = MergeThreadPage(state, Page{Events: all[:boundary], ReachedBeginning: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Events) != len(all) || len(state.Items) != len(all) {
				t.Fatalf("events=%d items=%d want=%d", len(state.Events), len(state.Items), len(all))
			}
			for index, event := range state.Events {
				if event.Sequence != uint64(index+1) {
					t.Fatalf("sequence[%d]=%d", index, event.Sequence)
				}
			}
			if !state.BeginningVisible() || len(state.Gaps) != 0 {
				t.Fatalf("beginning=%v gaps=%v", state.BeginningVisible(), state.Gaps)
			}
		})
	}
}

func TestMergeThreadPageOrdersEqualTimestampsBySequenceAndReportsGaps(t *testing.T) {
	state, err := MergeThreadPage(State{}, Page{Events: []events.SessionEvent{
		finalEvent(t, 4, messageID(t, 4), "four"),
		finalEvent(t, 2, messageID(t, 2), "two"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if state.Events[0].Sequence != 2 || state.Events[1].Sequence != 4 {
		t.Fatalf("durable order = %v", state.Events)
	}
	if len(state.Gaps) != 1 || state.Gaps[0] != (SequenceGap{After: 2, Before: 4}) {
		t.Fatalf("gaps = %#v", state.Gaps)
	}
}

func TestMergeThreadPageRejectsConflictingDuplicate(t *testing.T) {
	first := finalEvent(t, 1, messageID(t, 1), "first")
	state, err := MergeThreadPage(State{}, Page{Events: []events.SessionEvent{first}})
	if err != nil {
		t.Fatal(err)
	}
	conflict := finalEvent(t, 1, messageID(t, 1), "changed")
	if _, err := MergeThreadPage(state, Page{Events: []events.SessionEvent{conflict}}); err == nil {
		t.Fatal("conflicting replay duplicate succeeded")
	}
}

func TestReplayDoesNotClearOlderPaginationAndThreadBeginningNeedNotBeSequenceOne(t *testing.T) {
	state, err := MergeThreadPage(State{}, Page{
		Events:   []events.SessionEvent{finalEvent(t, 44, messageID(t, 44), "newest")},
		HasOlder: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = MergeThreadPage(state, Page{
		Events: []events.SessionEvent{finalEvent(t, 45, messageID(t, 45), "replay")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.HasOlder {
		t.Fatal("cursor-less replay cleared the selected thread's older-page affordance")
	}
	state, err = MergeThreadPage(state, Page{
		Events:           []events.SessionEvent{finalEvent(t, 41, messageID(t, 41), "thread beginning")},
		ReachedBeginning: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.HasOlder || !state.BeginningVisible() || state.Events[0].Sequence != 41 {
		t.Fatalf("thread beginning state = %#v", state)
	}
}

func TestMessageDeltasRemainProvisionalUntilDurableFinal(t *testing.T) {
	id := messageID(t, 20)
	state, err := ApplyMessageDelta(State{}, deltaEvent(t, 1, id, "hel"))
	if err != nil {
		t.Fatal(err)
	}
	state, err = ApplyMessageDelta(state, deltaEvent(t, 2, id, "lo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 1 || state.Items[0].Message.Body != "hello" || !state.Items[0].Message.Provisional {
		t.Fatalf("provisional projection = %#v", state.Items)
	}
	state, err = FinalizeMessage(state, finalEvent(t, 3, id, "Hello durable"))
	if err != nil {
		t.Fatal(err)
	}
	message := state.Items[0].Message
	if message.Body != "Hello durable" || message.Provisional || !message.Final || len(state.Items[0].Sequences) != 3 {
		t.Fatalf("final projection = %#v item=%#v", message, state.Items[0])
	}
}

func TestGroupEventsUpdatesToolAndApprovalInPlace(t *testing.T) {
	toolStart := timelineEvent(t, 1, events.KindToolStarted, events.Payload{Tool: &events.Tool{
		ExecutionID: "exec-1", CommandName: "go-test", State: "running", RedactedSummary: "started",
	}})
	toolDone := timelineEvent(t, 2, events.KindToolCompleted, events.Payload{Tool: &events.Tool{
		ExecutionID: "exec-1", CommandName: "go-test", State: "passed", RedactedSummary: "12 passed",
	}})
	approvalID := mustApprovalID(t, 1)
	requested := timelineEvent(t, 3, events.KindApprovalRequested, events.Payload{Approval: &events.Approval{
		ApprovalID: approvalID, State: domain.ApprovalRequestStatePending, Scope: "repository", RedactedReason: "write files",
	}})
	resolved := timelineEvent(t, 4, events.KindApprovalResolved, events.Payload{Approval: &events.Approval{
		ApprovalID: approvalID, State: domain.ApprovalRequestStateGranted, Scope: "repository", RedactedReason: "allowed once",
	}})
	items, err := GroupEvents([]events.SessionEvent{toolStart, toolDone, requested, resolved})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Tool.State != "passed" || items[1].Approval.State != "granted" {
		t.Fatalf("grouped items = %#v", items)
	}
}

func TestGroupEventsUsesStableKeysForReplaceableProjections(t *testing.T) {
	validationID := mustValidationID(t, 1)
	eventsFixture := []events.SessionEvent{
		timelineEvent(t, 1, events.KindForecastUpdated, events.Payload{Forecast: &events.Forecast{}}),
		timelineEvent(t, 2, events.KindForecastUpdated, events.Payload{Forecast: &events.Forecast{}}),
		timelineEvent(t, 3, events.KindValidationUpdated, events.Payload{Validation: &events.Validation{
			ValidationID: validationID, State: domain.ValidationStateRunning, RedactedSummary: "running",
		}}),
		timelineEvent(t, 4, events.KindValidationUpdated, events.Payload{Validation: &events.Validation{
			ValidationID: validationID, State: domain.ValidationStatePassed, RedactedSummary: "passed",
		}}),
	}
	items, err := GroupEvents(eventsFixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Key != "forecast:current" || items[0].LastSequence != 2 ||
		items[1].Key != ItemKey("validation:"+validationID.String()) || items[1].LastSequence != 4 {
		t.Fatalf("replaceable projections = %#v", items)
	}
}

func TestAnchorPreservationWithVariableHeightCards(t *testing.T) {
	before := []Measurement{{Key: "sequence:8", Top: 120, Height: 80}}
	after := []Measurement{
		{Key: "sequence:1", Top: 0, Height: 91},
		{Key: "sequence:2", Top: 91, Height: 143},
		{Key: "sequence:8", Top: 354, Height: 80},
	}
	got, err := PreserveAnchor(160, "sequence:8", before, after)
	if err != nil {
		t.Fatal(err)
	}
	if got != 394 {
		t.Fatalf("scroll top = %d, want 394", got)
	}
}

func TestPaginationAndAutoFollowPolicies(t *testing.T) {
	if !ShouldLoadOlder(ScrollMetrics{ScrollTop: 20, ClientHeight: 600, ScrollHeight: 1800}, 40, true, false) {
		t.Fatal("near-top older page was not requested")
	}
	nearBottom := ScrollMetrics{ScrollTop: 1165, ClientHeight: 600, ScrollHeight: 1800}
	if !ShouldAutoFollow(nearBottom, 40) {
		t.Fatal("near-bottom viewport did not auto-follow")
	}
	away := ScrollMetrics{ScrollTop: 700, ClientHeight: 600, ScrollHeight: 1800}
	state := ObserveNewEvents(FollowState{AtLiveEdge: true}, away, 40, 3)
	if !state.ShowNewEvents() || state.NewEvents != 3 {
		t.Fatalf("new-events state = %#v", state)
	}
	if returned := ReturnToLive(state); !returned.AtLiveEdge || returned.NewEvents != 0 {
		t.Fatalf("return-to-live state = %#v", returned)
	}
}

func deltaEvent(t *testing.T, sequence uint64, id domain.MessageID, body string) events.SessionEvent {
	t.Helper()
	return timelineEvent(t, sequence, events.KindMessageDelta, events.Payload{MessageDelta: &events.MessageDelta{MessageID: id, RedactedDelta: body}})
}

func finalEvent(t *testing.T, sequence uint64, id domain.MessageID, body string) events.SessionEvent {
	t.Helper()
	return timelineEvent(t, sequence, events.KindMessageFinal, events.Payload{MessageFinal: &events.MessageFinal{MessageID: id, Role: "assistant", RedactedBody: body}})
}

func timelineEvent(t *testing.T, sequence uint64, kind events.Kind, payload events.Payload) events.SessionEvent {
	t.Helper()
	event, err := (events.NewSessionEvent{
		SessionID: mustSessionID(t), ThreadID: mustThreadID(t), Kind: kind,
		PayloadVersion: 1, Payload: payload,
	}).Build(sequence, fixedTimelineTime)
	if err != nil {
		t.Fatal(err)
	}
	return event
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

func messageID(t *testing.T, ordinal uint64) domain.MessageID {
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

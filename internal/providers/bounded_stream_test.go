package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func TestBoundedModelStreamSequencesAndRetainsFullBufferTerminalError(
	t *testing.T,
) {
	owner, cancel := context.WithCancel(t.Context())
	stream, err := NewBoundedModelStream(
		owner,
		cancel,
		func() time.Time {
			return time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
		},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Emit(t.Context(), StreamEvent{
		Kind: StreamEventTextDelta, Text: "first",
	}); err != nil {
		t.Fatal(err)
	}
	terminal := errors.New("synthetic terminal failure")
	stream.Finish(terminal)
	event, err := stream.Recv(t.Context())
	if err != nil || event.Sequence != 1 || event.Text != "first" ||
		event.ObservedAt.IsZero() {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if _, err := stream.Recv(t.Context()); !errors.Is(err, terminal) {
		t.Fatalf("terminal error = %v", err)
	}
	if _, err := stream.Recv(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatalf("post-terminal error = %v", err)
	}
}

func TestBoundedModelStreamCancellationDiscardsBufferedToolEvents(t *testing.T) {
	owner, cancel := context.WithCancel(t.Context())
	stream, err := NewBoundedModelStream(owner, cancel, time.Now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Emit(t.Context(), StreamEvent{
		Kind: StreamEventToolCall,
		ToolCall: &ToolCall{
			ID: "call-1", Name: "lookup",
			Arguments: json.RawMessage(`{"id":1}`),
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	event, err := stream.Recv(t.Context())
	var failure *Failure
	if !errors.As(err, &failure) ||
		failure.Kind != FailureCanceled ||
		event.ToolCall != nil {
		t.Fatalf("late event=%#v err=%v", event, err)
	}
}

func TestBoundedModelStreamOwnerCancellationAfterFinishDiscardsBufferedEvents(
	t *testing.T,
) {
	owner, cancel := context.WithCancel(t.Context())
	stream, err := NewBoundedModelStream(owner, cancel, time.Now, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Emit(t.Context(), StreamEvent{
		Kind: StreamEventToolCall,
		ToolCall: &ToolCall{
			ID: "call-1", Name: "lookup",
			Arguments: json.RawMessage(`{"id":1}`),
		},
	}); err != nil {
		t.Fatal(err)
	}
	stream.Finish(nil)
	cancel()
	event, err := stream.Recv(t.Context())
	var failure *Failure
	if !errors.As(err, &failure) ||
		failure.Kind != FailureCanceled ||
		event.ToolCall != nil {
		t.Fatalf("late finished event=%#v err=%v", event, err)
	}
}

func TestBoundedModelStreamClassifiesOwnerDeadlineAsTimeout(t *testing.T) {
	owner, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	stream, err := NewBoundedModelStream(owner, cancel, time.Now, 1)
	if err != nil {
		t.Fatal(err)
	}
	<-owner.Done()
	_, err = stream.Recv(t.Context())
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureTimeout {
		t.Fatalf("deadline failure = %T %v", err, err)
	}
}

func TestToolCallDeltaFragmentCanBeSerializedBeforeJSONIsComplete(t *testing.T) {
	event := StreamEvent{
		Kind: StreamEventToolCallDelta,
		ToolCallDelta: &ToolCallDelta{
			Index: 0, ID: "call-1", Name: "lookup",
			ArgumentsFragment: `{"id":`,
		},
	}
	if _, err := json.Marshal(event); err != nil {
		t.Fatalf("partial tool fragment is not transport-safe: %v", err)
	}
}

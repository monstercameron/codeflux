package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWorkerClientHeartbeatsHandlesCheckpointAndStops(t *testing.T) {
	var (
		mu        sync.Mutex
		sequences []uint64
	)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		var message Message
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		sequences = append(sequences, message.Sequence)
		count := len(sequences)
		mu.Unlock()
		var control *Control
		if count == 1 {
			control = &Control{Kind: ControlCheckpoint}
		} else if count == 2 {
			if message.Heartbeat.LastCheckpoint != "checkpoint-one" {
				t.Errorf("checkpoint was not reported: %#v", message.Heartbeat)
			}
			control = &Control{Kind: ControlShutdown}
		}
		_ = json.NewEncoder(writer).Encode(struct {
			Control *Control `json:"control,omitempty"`
		}{Control: control})
	}))
	defer server.Close()
	startup := workerStartupFixture(t, t.TempDir())
	startup.CoordinatorEndpoint = server.URL
	checkpointer := &memoryCheckpointer{}
	err := Run(t.Context(), startup, ClientOptions{
		HTTPClient: server.Client(), HeartbeatInterval: 100 * time.Millisecond,
		Checkpointer: checkpointer, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sequences) != 2 || sequences[0] != 1 || sequences[1] != 2 ||
		checkpointer.calls != 1 {
		t.Fatalf("sequences=%v checkpoint-calls=%d", sequences, checkpointer.calls)
	}
}

func TestWorkerClientBoundsReconnectAndDoesNotRetryDenial(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		requests++
		http.Error(writer, "denied", http.StatusForbidden)
	}))
	defer server.Close()
	startup := workerStartupFixture(t, t.TempDir())
	startup.CoordinatorEndpoint = server.URL
	err := Run(context.Background(), startup, ClientOptions{
		HTTPClient: server.Client(), HeartbeatInterval: 100 * time.Millisecond,
		Reconnect: ReconnectPolicy{
			MaximumAttempts: 3, InitialDelay: 10 * time.Millisecond,
			MaximumDelay: 20 * time.Millisecond,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "status 403") || requests != 1 {
		t.Fatalf("denial result requests=%d err=%v", requests, err)
	}
}

func TestWorkerClientReportsOrderedStatusAndRejectsTrailingResponse(t *testing.T) {
	reports := make(chan Report, 1)
	reports <- Report{Status: &Status{
		Kind: StatusCheckpointed, Summary: "redacted checkpoint status",
		OccurredAt: time.Now(),
	}}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		var message Message
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			t.Error(err)
			return
		}
		if message.Sequence == 2 {
			if message.Status == nil ||
				message.Status.Kind != StatusCheckpointed {
				t.Errorf("worker status message = %#v", message)
			}
			_ = json.NewEncoder(writer).Encode(struct {
				Control *Control `json:"control,omitempty"`
			}{Control: &Control{Kind: ControlShutdown}})
			return
		}
		_ = json.NewEncoder(writer).Encode(struct {
			Control *Control `json:"control,omitempty"`
		}{})
	}))
	startup := workerStartupFixture(t, t.TempDir())
	startup.CoordinatorEndpoint = server.URL
	err := Run(t.Context(), startup, ClientOptions{
		HTTPClient: server.Client(), HeartbeatInterval: 10 * time.Second,
		Reports: reports,
	})
	server.Close()
	if err != nil {
		t.Fatal(err)
	}

	trailing := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = writer.Write([]byte(`{"control":null} {}`))
	}))
	defer trailing.Close()
	startup.CoordinatorEndpoint = trailing.URL
	err = Run(t.Context(), startup, ClientOptions{
		HTTPClient: trailing.Client(), HeartbeatInterval: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("trailing coordinator response was accepted")
	}
}

func TestWorkerClientPauseResumeCancelControlsExecutionGate(t *testing.T) {
	var (
		mu     sync.Mutex
		states []StatusKind
	)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		var message Message
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		states = append(states, message.Heartbeat.State)
		count := len(states)
		mu.Unlock()
		controls := map[int]ControlKind{
			1: ControlPause,
			2: ControlResume,
			3: ControlCancel,
		}
		var control *Control
		if kind := controls[count]; kind != "" {
			control = &Control{Kind: kind, Reason: "integration fixture"}
		}
		_ = json.NewEncoder(writer).Encode(struct {
			Control *Control `json:"control,omitempty"`
		}{Control: control})
	}))
	defer server.Close()
	startup := workerStartupFixture(t, t.TempDir())
	startup.CoordinatorEndpoint = server.URL
	gate := NewExecutionGate()
	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(t.Context(), startup, ClientOptions{
			HTTPClient: server.Client(), HeartbeatInterval: 150 * time.Millisecond,
			ExecutionGate: gate,
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !gate.Paused() {
		if time.Now().After(deadline) {
			t.Fatal("pause control did not close the execution gate")
		}
		time.Sleep(time.Millisecond)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- gate.Wait(t.Context()) }()
	select {
	case err := <-waitDone:
		t.Fatalf("paused execution gate returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("resumed execution gate error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resume control did not release the execution gate")
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel control did not terminate the worker client")
	}
	if err := gate.Wait(context.Background()); !errors.Is(err, ErrExecutionCancelled) {
		t.Fatalf("cancelled execution gate error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []StatusKind{StatusRunning, StatusPaused, StatusRunning}
	if len(states) != len(want) {
		t.Fatalf("heartbeat states = %v", states)
	}
	for index := range want {
		if states[index] != want[index] {
			t.Fatalf("heartbeat states = %v", states)
		}
	}
}

type memoryCheckpointer struct {
	calls int
}

func (checkpointer *memoryCheckpointer) Checkpoint(
	context.Context,
) (string, error) {
	checkpointer.calls++
	return "checkpoint-one", nil
}

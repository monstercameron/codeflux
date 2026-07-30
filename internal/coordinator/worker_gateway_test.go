package coordinator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/worker"
)

func TestWorkerGatewayAuthenticatesOrdersPersistsAndControls(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	token := "0123456789abcdef0123456789abcdef"
	digest := sha256.Sum256([]byte(token))
	lease := storage.WorkerLease{
		ID: "lease-gateway", TaskID: taskID, RunID: runID,
		State:              storage.WorkerLeaseStarting,
		SessionTokenSHA256: hex.EncodeToString(digest[:]),
		StartedAt:          time.Now().Add(-time.Second),
	}
	store := &memoryHeartbeatStore{current: lease}
	gateway, err := NewWorkerGateway(store)
	if err != nil {
		t.Fatal(err)
	}
	receivedAt := time.Now().UTC()
	gateway.now = func() time.Time { return receivedAt }
	if err := gateway.Register(lease, token); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Register(lease, token); err == nil {
		t.Fatal("duplicate live gateway session was accepted")
	}
	started, err := gateway.RecordProcessStarted(context.Background(), runID, 1234)
	if err != nil {
		t.Fatal(err)
	}
	if started.ProcessID == nil || *started.ProcessID != 1234 {
		t.Fatalf("started lease = %#v", started)
	}
	if err := gateway.QueueControl(runID, worker.Control{
		Kind: worker.ControlPause, Reason: "user requested pause",
	}); err != nil {
		t.Fatal(err)
	}
	message := worker.Message{
		ProtocolVersion: worker.ProtocolVersion, TaskID: taskID, RunID: runID,
		Sequence: 1, SessionToken: token,
		Heartbeat: &worker.Heartbeat{
			WorkerPID: 1234, State: worker.StatusRunning,
			ObservedAt: receivedAt.Add(24 * time.Hour),
		},
	}
	control, err := gateway.Receive(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	if control == nil || control.Kind != worker.ControlPause ||
		store.last.Sequence != 1 || store.last.ProcessID != 1234 ||
		!store.last.ObservedAt.Equal(receivedAt) {
		t.Fatalf("heartbeat/control = %#v / %#v", store.last, control)
	}
	if _, err := gateway.Receive(context.Background(), message); err == nil {
		t.Fatal("replayed worker sequence was accepted")
	}
	message.Sequence = 2
	message.SessionToken = "fedcba9876543210fedcba9876543210"
	if _, err := gateway.Receive(context.Background(), message); err == nil {
		t.Fatal("invalid worker token was accepted")
	}
	message.SessionToken = token
	message.Sequence = 2
	message.Heartbeat = nil
	message.Status = &worker.Status{
		Kind:       worker.StatusCheckpointed,
		Summary:    `checkpoint failed: OPENAI_API_KEY="sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX"`,
		OccurredAt: time.Now(),
	}
	if _, err := gateway.Receive(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	message.Sequence = 3
	message.Status = nil
	message.ToolEvent = &worker.ToolEvent{
		RequestID: "tool-1", State: "completed",
		Summary: "Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345",
	}
	if _, err := gateway.Receive(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(store.reports) != 2 ||
		store.reports[0].Kind != "status" ||
		store.reports[1].Kind != "tool-event" {
		t.Fatalf("worker reports = %#v", store.reports)
	}
	for _, report := range store.reports {
		if !strings.Contains(report.PayloadJSON, redact.Marker) ||
			strings.Contains(report.PayloadJSON, "ABCDEFGHIJKLMNOPQRSTUVWX") ||
			strings.Contains(report.PayloadJSON, "abcdefghijklmnopqrstuvwxyz012345") {
			t.Fatalf("worker report was persisted without redaction: %s", report.PayloadJSON)
		}
	}
}

func TestWorkerGatewayHandlerRejectsTrailingAndOversizedMessages(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	token := "0123456789abcdef0123456789abcdef"
	digest := sha256.Sum256([]byte(token))
	lease := storage.WorkerLease{
		ID: "lease-handler", TaskID: taskID, RunID: runID,
		State: storage.WorkerLeaseStarting, Revision: 0,
		SessionTokenSHA256: hex.EncodeToString(digest[:]),
		StartedAt:          time.Now().Add(-time.Second),
	}
	gateway, err := NewWorkerGateway(&memoryHeartbeatStore{current: lease})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Register(lease, token); err != nil {
		t.Fatal(err)
	}
	message := worker.Message{
		ProtocolVersion: worker.ProtocolVersion, TaskID: taskID, RunID: runID,
		Sequence: 1, SessionToken: token,
		Heartbeat: &worker.Heartbeat{
			WorkerPID: 42, State: worker.StatusRunning, ObservedAt: time.Now(),
		},
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"trailing":  append(append([]byte(nil), encoded...), []byte(` {}`)...),
		"oversized": append(append([]byte(nil), encoded...), bytes.Repeat([]byte(" "), 64<<10)...),
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost, "/internal/worker/heartbeat", bytes.NewReader(body),
			)
			response := httptest.NewRecorder()
			gateway.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", response.Code)
			}
		})
	}
}

type memoryHeartbeatStore struct {
	last    storage.RecordWorkerHeartbeat
	current storage.WorkerLease
	reports []storage.RecordWorkerReport
}

func (store *memoryHeartbeatStore) RecordWorkerReport(
	_ context.Context,
	input storage.RecordWorkerReport,
) (storage.WorkerLease, error) {
	store.reports = append(store.reports, input)
	store.current.LastSequence = input.Sequence
	store.current.Revision = input.ExpectedRevision + 1
	return store.current, nil
}

func (store *memoryHeartbeatStore) RecordWorkerProcessStarted(
	_ context.Context,
	input storage.RecordWorkerProcessStarted,
) (storage.WorkerLease, error) {
	store.current.ProcessID = &input.ProcessID
	store.current.Revision = input.ExpectedRevision + 1
	return store.current, nil
}

func (store *memoryHeartbeatStore) RecordWorkerHeartbeat(
	_ context.Context,
	input storage.RecordWorkerHeartbeat,
) (storage.WorkerLease, error) {
	store.last = input
	store.current.State = input.State
	store.current.ProcessID = &input.ProcessID
	store.current.LastSequence = input.Sequence
	store.current.LastHeartbeatAt = &input.ObservedAt
	store.current.Revision = input.ExpectedRevision + 1
	return store.current, nil
}

package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/worker"
)

type WorkerMessageStore interface {
	RecordWorkerProcessStarted(
		context.Context,
		storage.RecordWorkerProcessStarted,
	) (storage.WorkerLease, error)
	RecordWorkerHeartbeat(
		context.Context,
		storage.RecordWorkerHeartbeat,
	) (storage.WorkerLease, error)
	RecordWorkerReport(
		context.Context,
		storage.RecordWorkerReport,
	) (storage.WorkerLease, error)
}

type WorkerReportRedactor interface {
	Redact(redact.Boundary, string) (redact.Result, error)
}

type workerSession struct {
	lease    storage.WorkerLease
	token    string
	controls []worker.Control
}

// WorkerGateway authenticates ordered worker reports and returns at most one
// coordinator control request with each heartbeat.
type WorkerGateway struct {
	mu       sync.Mutex
	store    WorkerMessageStore
	redactor WorkerReportRedactor
	sessions map[domain.RunID]*workerSession
	now      func() time.Time
}

func NewWorkerGateway(
	store WorkerMessageStore,
	reportRedactors ...WorkerReportRedactor,
) (*WorkerGateway, error) {
	if store == nil {
		return nil, errors.New("worker heartbeat store is required")
	}
	if len(reportRedactors) > 1 {
		return nil, errors.New("only one worker report redactor is supported")
	}
	var reportRedactor WorkerReportRedactor
	if len(reportRedactors) == 1 {
		reportRedactor = reportRedactors[0]
		if reportRedactor == nil {
			return nil, errors.New("worker report redactor is required")
		}
	} else {
		pipeline, err := redact.NewPipeline(nil, redact.Limits{
			MaximumInputBytes:  16 << 10,
			MaximumOutputBytes: 8 << 10,
		})
		if err != nil {
			return nil, err
		}
		reportRedactor = pipeline
	}
	return &WorkerGateway{
		store: store, redactor: reportRedactor,
		sessions: make(map[domain.RunID]*workerSession), now: time.Now,
	}, nil
}

func (gateway *WorkerGateway) Register(
	lease storage.WorkerLease,
	token string,
) error {
	if lease.RunID.IsZero() || len(token) < 32 {
		return errors.New("worker session identity is invalid")
	}
	digest := sha256.Sum256([]byte(token))
	if hex.EncodeToString(digest[:]) != lease.SessionTokenSHA256 {
		return errors.New("worker session token does not match durable lease")
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if _, exists := gateway.sessions[lease.RunID]; exists {
		return errors.New("worker run already has a live gateway session")
	}
	gateway.sessions[lease.RunID] = &workerSession{lease: lease, token: token}
	return nil
}

func (gateway *WorkerGateway) QueueControl(
	runID domain.RunID,
	control worker.Control,
) error {
	if err := control.Validate(); err != nil {
		return err
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	session, exists := gateway.sessions[runID]
	if !exists {
		return errors.New("worker session is unavailable")
	}
	if len(session.controls) >= 8 {
		return errors.New("worker control queue is full")
	}
	session.controls = append(session.controls, control)
	return nil
}

// RecordProcessStarted persists coordinator-observed process metadata while
// serializing it with the first authenticated worker message.
func (gateway *WorkerGateway) RecordProcessStarted(
	ctx context.Context,
	runID domain.RunID,
	processID int,
) (storage.WorkerLease, error) {
	if processID < 1 {
		return storage.WorkerLease{}, errors.New("worker process ID is invalid")
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	session, exists := gateway.sessions[runID]
	if !exists {
		return storage.WorkerLease{}, errors.New("worker session is unavailable")
	}
	if session.lease.ProcessID != nil {
		if *session.lease.ProcessID == processID {
			return session.lease, nil
		}
		return storage.WorkerLease{}, errors.New("worker process identity changed")
	}
	updated, err := gateway.store.RecordWorkerProcessStarted(
		ctx,
		storage.RecordWorkerProcessStarted{
			ID: session.lease.ID, ExpectedRevision: session.lease.Revision,
			ProcessID: processID,
		},
	)
	if err != nil {
		return storage.WorkerLease{}, err
	}
	session.lease = updated
	return updated, nil
}

func (gateway *WorkerGateway) SnapshotLease(
	runID domain.RunID,
) (storage.WorkerLease, bool) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	session, exists := gateway.sessions[runID]
	if !exists {
		return storage.WorkerLease{}, false
	}
	return session.lease, true
}

func (gateway *WorkerGateway) Unregister(runID domain.RunID) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	delete(gateway.sessions, runID)
}

func (gateway *WorkerGateway) Receive(
	ctx context.Context,
	message worker.Message,
) (*worker.Control, error) {
	gateway.mu.Lock()
	session, exists := gateway.sessions[message.RunID]
	if !exists {
		gateway.mu.Unlock()
		return nil, errors.New("worker session is unavailable")
	}
	token := session.token
	lease := session.lease
	if message.Sequence <= lease.LastSequence {
		gateway.mu.Unlock()
		return nil, errors.New("worker message sequence is stale")
	}
	if err := message.Validate(token); err != nil {
		gateway.mu.Unlock()
		return nil, err
	}
	if message.TaskID != lease.TaskID || message.RunID != lease.RunID {
		gateway.mu.Unlock()
		return nil, errors.New("worker message escaped its lease identity")
	}
	gateway.mu.Unlock()

	updated, err := gateway.persistMessage(ctx, lease, message)
	if err != nil {
		return nil, err
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	session, exists = gateway.sessions[message.RunID]
	if !exists || session.lease.Revision != lease.Revision {
		return nil, errors.New("worker session changed during heartbeat")
	}
	session.lease = updated
	var control *worker.Control
	if len(session.controls) != 0 {
		next := session.controls[0]
		session.controls = session.controls[1:]
		control = &next
	}
	return control, nil
}

func (gateway *WorkerGateway) persistMessage(
	ctx context.Context,
	lease storage.WorkerLease,
	message worker.Message,
) (storage.WorkerLease, error) {
	if message.Heartbeat != nil {
		heartbeat := *message.Heartbeat
		state := storage.WorkerLeaseRunning
		switch heartbeat.State {
		case worker.StatusStarting, worker.StatusRunning:
		case worker.StatusPaused:
			state = storage.WorkerLeasePaused
		case worker.StatusStopping:
			state = storage.WorkerLeaseStopping
		default:
			return storage.WorkerLease{}, errors.New("worker heartbeat state is invalid")
		}
		if heartbeat.LeaseID != "" && heartbeat.LeaseID != lease.ID {
			return storage.WorkerLease{}, errors.New("worker heartbeat lease identity is invalid")
		}
		return gateway.store.RecordWorkerHeartbeat(
			ctx,
			storage.RecordWorkerHeartbeat{
				ID: lease.ID, ExpectedRevision: lease.Revision,
				Sequence: message.Sequence, State: state,
				ProcessID:    heartbeat.WorkerPID,
				CheckpointID: optionalCheckpoint(heartbeat.LastCheckpoint),
				ObservedAt:   gateway.now().UTC(),
			},
		)
	}
	kind := ""
	occurredAt := gateway.now().UTC()
	var payload any
	if message.Status != nil {
		if !validStatus(message.Status.Kind) ||
			message.Status.OccurredAt.IsZero() ||
			len(message.Status.Summary) > 8192 {
			return storage.WorkerLease{}, errors.New("worker status report is invalid")
		}
		kind = "status"
		occurredAt = message.Status.OccurredAt.UTC()
		status := *message.Status
		redactedSummary, err := gateway.redactReportText(status.Summary)
		if err != nil {
			return storage.WorkerLease{}, err
		}
		status.Summary = redactedSummary
		payload = &status
	} else if message.ToolEvent != nil {
		if strings.TrimSpace(message.ToolEvent.RequestID) == "" ||
			len(message.ToolEvent.RequestID) > 255 ||
			strings.TrimSpace(message.ToolEvent.State) == "" ||
			len(message.ToolEvent.State) > 255 ||
			len(message.ToolEvent.Summary) > 8192 {
			return storage.WorkerLease{}, errors.New("worker tool report is invalid")
		}
		kind = "tool-event"
		event := *message.ToolEvent
		redactedRequestID, err := gateway.redactReportText(event.RequestID)
		if err != nil {
			return storage.WorkerLease{}, err
		}
		redactedState, err := gateway.redactReportText(event.State)
		if err != nil {
			return storage.WorkerLease{}, err
		}
		redactedSummary, err := gateway.redactReportText(event.Summary)
		if err != nil {
			return storage.WorkerLease{}, err
		}
		event.RequestID = redactedRequestID
		event.State = redactedState
		event.Summary = redactedSummary
		payload = &event
	} else {
		return storage.WorkerLease{}, errors.New("worker report payload is unsupported")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return storage.WorkerLease{}, err
	}
	return gateway.store.RecordWorkerReport(
		ctx,
		storage.RecordWorkerReport{
			ID: lease.ID, ExpectedRevision: lease.Revision,
			Sequence: message.Sequence, TaskID: message.TaskID,
			RunID: message.RunID, Kind: kind,
			PayloadJSON: string(encoded), OccurredAt: occurredAt,
		},
	)
}

func (gateway *WorkerGateway) redactReportText(value string) (string, error) {
	result, err := gateway.redactor.Redact(redact.BoundaryLogPersistence, value)
	if err != nil {
		return "", errors.New("redact worker report")
	}
	return result.Text, nil
}

func validStatus(kind worker.StatusKind) bool {
	switch kind {
	case worker.StatusStarting, worker.StatusRunning, worker.StatusPaused,
		worker.StatusCheckpointed, worker.StatusStopping, worker.StatusExited,
		worker.StatusFailed:
		return true
	default:
		return false
	}
}

func (gateway *WorkerGateway) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var message worker.Message
		request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&message); err != nil {
			http.Error(writer, "invalid worker message", http.StatusBadRequest)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			http.Error(writer, "invalid worker message", http.StatusBadRequest)
			return
		}
		control, err := gateway.Receive(request.Context(), message)
		if err != nil {
			http.Error(writer, "worker message denied", http.StatusForbidden)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(struct {
			Control *worker.Control `json:"control,omitempty"`
		}{Control: control})
	})
}

func optionalCheckpoint(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Checkpointer interface {
	Checkpoint(context.Context) (string, error)
}

type ClientOptions struct {
	HTTPClient        *http.Client
	HeartbeatInterval time.Duration
	RequestTimeout    time.Duration
	Reconnect         ReconnectPolicy
	Checkpointer      Checkpointer
	ExecutionGate     *ExecutionGate
	Reports           <-chan Report
	Now               func() time.Time
}

// Report is one status or mediated-tool fact emitted between heartbeats.
type Report struct {
	Status    *Status
	ToolEvent *ToolEvent
}

func (report Report) validate() error {
	if (report.Status == nil) == (report.ToolEvent == nil) {
		return errors.New("worker report must contain exactly one payload")
	}
	if report.Status != nil {
		return report.Status.validate()
	}
	return report.ToolEvent.validate()
}

// Run maintains one authenticated heartbeat/control exchange until the
// coordinator cancels or shuts down the worker.
func Run(
	ctx context.Context,
	startup StartupParameters,
	options ClientOptions,
) error {
	if err := startup.Validate(); err != nil {
		return err
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{}
	} else {
		client := *options.HTTPClient
		options.HTTPClient = &client
	}
	options.HTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("worker coordinator redirects are forbidden")
	}
	if options.RequestTimeout == 0 {
		options.RequestTimeout = 5 * time.Second
	}
	if options.RequestTimeout < 100*time.Millisecond ||
		options.RequestTimeout > 30*time.Second {
		return errors.New("worker request timeout is outside supported bounds")
	}
	if options.HeartbeatInterval == 0 {
		options.HeartbeatInterval = 2 * time.Second
	}
	if options.HeartbeatInterval < 100*time.Millisecond ||
		options.HeartbeatInterval > 10*time.Second {
		return errors.New("worker heartbeat interval is outside supported bounds")
	}
	if options.Reconnect.MaximumAttempts == 0 {
		options.Reconnect = ReconnectPolicy{
			MaximumAttempts: 4, InitialDelay: 100 * time.Millisecond,
			MaximumDelay: 2 * time.Second,
		}
	}
	if err := options.Reconnect.Validate(); err != nil {
		return err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.ExecutionGate == nil {
		options.ExecutionGate = NewExecutionGate()
	}
	defer options.ExecutionGate.Cancel()
	state := StatusRunning
	var checkpoint string
	var sequence uint64
	ticker := time.NewTicker(options.HeartbeatInterval)
	defer ticker.Stop()
	for {
		sequence++
		control, err := exchangeHeartbeat(
			ctx, startup, options, sequence, state, checkpoint,
		)
		if err != nil {
			return err
		}
		stop, err := applyControl(
			ctx, control, options.Checkpointer, options.ExecutionGate,
			&state, &checkpoint,
		)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	waitForNextHeartbeat:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				break waitForNextHeartbeat
			case report, open := <-options.Reports:
				if !open {
					options.Reports = nil
					continue
				}
				if err := report.validate(); err != nil {
					return err
				}
				sequence++
				control, err = exchangeReport(
					ctx, startup, options, sequence, report,
				)
				if err != nil {
					return err
				}
				stop, err = applyControl(
					ctx, control, options.Checkpointer, options.ExecutionGate,
					&state, &checkpoint,
				)
				if err != nil {
					return err
				}
				if stop {
					return nil
				}
			}
		}
	}
}

func exchangeHeartbeat(
	ctx context.Context,
	startup StartupParameters,
	options ClientOptions,
	sequence uint64,
	state StatusKind,
	checkpoint string,
) (*Control, error) {
	message := Message{
		ProtocolVersion: ProtocolVersion,
		TaskID:          startup.TaskID, RunID: startup.RunID,
		Sequence: sequence, SessionToken: startup.SessionToken,
		Heartbeat: &Heartbeat{
			WorkerPID: os.Getpid(), State: state,
			ObservedAt: options.Now().UTC(), LastCheckpoint: checkpoint,
		},
	}
	return exchangeMessage(ctx, startup, options, message)
}

func exchangeReport(
	ctx context.Context,
	startup StartupParameters,
	options ClientOptions,
	sequence uint64,
	report Report,
) (*Control, error) {
	message := Message{
		ProtocolVersion: ProtocolVersion,
		TaskID:          startup.TaskID,
		RunID:           startup.RunID,
		Sequence:        sequence,
		SessionToken:    startup.SessionToken,
		Status:          report.Status,
		ToolEvent:       report.ToolEvent,
	}
	return exchangeMessage(ctx, startup, options, message)
}

func exchangeMessage(
	ctx context.Context,
	startup StartupParameters,
	options ClientOptions,
	message Message,
) (*Control, error) {
	body, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(startup.CoordinatorEndpoint, "/") +
		"/internal/worker/heartbeat"
	var lastErr error
	for attempt := 1; attempt <= options.Reconnect.MaximumAttempts; attempt++ {
		attemptContext, cancel := context.WithTimeout(ctx, options.RequestTimeout)
		request, err := http.NewRequestWithContext(
			attemptContext, http.MethodPost, endpoint, bytes.NewReader(body),
		)
		if err != nil {
			cancel()
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := options.HTTPClient.Do(request)
		if err == nil {
			if response.StatusCode != http.StatusOK {
				_ = response.Body.Close()
				cancel()
				return nil, fmt.Errorf("worker heartbeat denied with status %d", response.StatusCode)
			}
			var envelope struct {
				Control *Control `json:"control,omitempty"`
			}
			decodeErr := decodeSingleJSON(response.Body, 16<<10, &envelope)
			closeErr := response.Body.Close()
			cancel()
			if decodeErr != nil {
				return nil, decodeErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if envelope.Control != nil {
				if err := envelope.Control.Validate(); err != nil {
					return nil, err
				}
			}
			return envelope.Control, nil
		}
		cancel()
		lastErr = err
		if attempt == options.Reconnect.MaximumAttempts {
			break
		}
		timer := time.NewTimer(options.Reconnect.Delay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("worker reconnect budget exhausted: %w", lastErr)
}

func applyControl(
	ctx context.Context,
	control *Control,
	checkpointer Checkpointer,
	executionGate *ExecutionGate,
	state *StatusKind,
	checkpoint *string,
) (bool, error) {
	if control == nil {
		return false, nil
	}
	if err := control.Validate(); err != nil {
		return false, err
	}
	switch control.Kind {
	case ControlPause:
		executionGate.Pause()
		*state = StatusPaused
	case ControlResume:
		executionGate.Resume()
		*state = StatusRunning
	case ControlCheckpoint:
		if control.CheckpointID != "" {
			*checkpoint = control.CheckpointID
			break
		}
		if checkpointer == nil {
			return false, errors.New("checkpoint requested without a checkpointer")
		}
		value, err := checkpointer.Checkpoint(ctx)
		if err != nil {
			return false, err
		}
		if !validRequiredIdentifier(value) {
			return false, errors.New("worker checkpoint identity is invalid")
		}
		*checkpoint = value
	case ControlCancel, ControlShutdown:
		executionGate.Cancel()
		return true, nil
	}
	return false, nil
}

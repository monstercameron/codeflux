// Package sessionclient owns the browser session-stream lifecycle.
package sessionclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const BridgePath = "/grpc"

var (
	ErrAlreadyStarted                = errors.New("session client already started")
	ErrNotStarted                    = errors.New("session client not started")
	ErrBrowserTransportUnavailable   = errors.New("browser session transport is available only in a js/wasm build")
	ErrReconnectAttemptsExhausted    = errors.New("session reconnect attempts exhausted")
	ErrInvalidSessionEvent           = errors.New("invalid session event")
	ErrSessionEventSequenceGap       = errors.New("session event sequence gap")
	ErrSessionEventIdentityMismatch  = errors.New("session event identity mismatch")
	ErrSessionEventApplicationFailed = errors.New("session event application failed")
)

// State is the connection-certainty state exposed to the frontend.
type State string

const (
	StateIdle         State = "idle"
	StateConnecting   State = "connecting"
	StateReplaying    State = "replaying"
	StateLive         State = "live"
	StateReconnecting State = "reconnecting"
	StateGap          State = "gap"
	StateStopped      State = "stopped"
	StateFailed       State = "failed"
)

// FailureKind is a safe, presentation-ready failure category. It deliberately
// excludes raw transport text, URLs, cookies, and server diagnostics.
type FailureKind string

const (
	FailureNone           FailureKind = ""
	FailureAuthentication FailureKind = "authentication"
	FailureIncompatible   FailureKind = "incompatible"
	FailureProtocol       FailureKind = "protocol"
	FailureApplication    FailureKind = "application"
	FailureUnavailable    FailureKind = "unavailable"
)

// Status is an immutable snapshot of the session-stream lifecycle.
type Status struct {
	State           State
	LastSequence    uint64
	ReconnectCount  int
	ControlsAllowed bool
	Failure         FailureKind
}

// RetryPolicy bounds application-level resubscription. GoGRPCBridge may also
// reconnect its underlying websocket, but this policy owns stream recreation
// and after_sequence replay.
type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// TunnelReconnectPolicy configures GoGRPCBridge's lower-level websocket
// reconnect behavior without leaking a WASM-only dependency into native code.
type TunnelReconnectPolicy struct {
	InitialDelay      time.Duration
	MaxDelay          time.Duration
	Multiplier        float64
	Jitter            float64
	MinConnectTimeout time.Duration
}

// Stream is the smallest generated server-stream surface required by Client.
type Stream interface {
	Recv() (*codefluxv1.SubscribeSessionResponse, error)
}

// Connection represents one transport connection and its session service.
type Connection interface {
	SubscribeSession(context.Context, *codefluxv1.SubscribeSessionRequest) (Stream, error)
	Close() error
}

// Connector establishes the authenticated same-origin browser connection.
// BrowserConnector relies on the browser-managed HttpOnly launch cookie and
// never accepts credential material.
type Connector interface {
	Connect(context.Context) (Connection, error)
}

// Config defines one ordered session subscription.
type Config struct {
	Connector     Connector
	SessionID     *codefluxv1.StableIdentity
	AfterSequence uint64
	Retry         RetryPolicy
	Apply         func(context.Context, *codefluxv1.SessionEvent) error
	Observe       func(Status)
	WaitForRetry  func(context.Context, time.Duration) error
}

// Client owns one cancellable, reconnecting ordered session subscription.
type Client struct {
	config Config

	mu         sync.Mutex
	status     Status
	started    bool
	cancel     context.CancelFunc
	done       chan struct{}
	result     error
	connection Connection
}

// New validates and copies the lifecycle configuration.
func New(config Config) (*Client, error) {
	if config.Connector == nil {
		return nil, errors.New("session connector is required")
	}
	if config.SessionID == nil || config.SessionID.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION || config.SessionID.GetValue() == "" {
		return nil, errors.New("valid session identity is required")
	}
	if config.Apply == nil {
		return nil, errors.New("session event apply function is required")
	}
	config.SessionID = &codefluxv1.StableIdentity{Kind: config.SessionID.GetKind(), Value: config.SessionID.GetValue()}
	config.Retry = normalizeRetryPolicy(config.Retry)
	if config.WaitForRetry == nil {
		config.WaitForRetry = waitForRetry
	}
	return &Client{
		config: config,
		status: Status{State: StateIdle, LastSequence: config.AfterSequence},
	}, nil
}

// Start launches the owned subscription goroutine. A Client is single-use.
func (client *Client) Start(parent context.Context) error {
	if parent == nil {
		return errors.New("parent context is required")
	}
	client.mu.Lock()
	if client.started {
		client.mu.Unlock()
		return ErrAlreadyStarted
	}
	ctx, cancel := context.WithCancel(parent)
	client.started = true
	client.cancel = cancel
	client.done = make(chan struct{})
	done := client.done
	client.mu.Unlock()

	client.transition(StateConnecting, FailureNone, 0)
	go func() {
		result := client.run(ctx)
		client.mu.Lock()
		client.result = result
		client.mu.Unlock()
		close(done)
	}()
	return nil
}

// Wait blocks until the lifecycle stops and returns its terminal error.
func (client *Client) Wait() error {
	client.mu.Lock()
	if !client.started {
		client.mu.Unlock()
		return ErrNotStarted
	}
	done := client.done
	client.mu.Unlock()
	<-done
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.result
}

// Close cancels the subscription, closes the active transport to unblock Recv,
// and waits for the owned goroutine. It is safe to call repeatedly.
func (client *Client) Close() error {
	client.mu.Lock()
	if !client.started {
		client.mu.Unlock()
		return nil
	}
	cancel := client.cancel
	done := client.done
	connection := client.connection
	client.connection = nil
	client.mu.Unlock()

	cancel()
	var closeError error
	if connection != nil {
		closeError = connection.Close()
	}
	<-done
	return closeError
}

// Status returns a race-safe lifecycle snapshot.
func (client *Client) Status() Status {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.status
}

func (client *Client) run(ctx context.Context) error {
	failures := 0
	for {
		if ctx.Err() != nil {
			client.transition(StateStopped, FailureNone, failures)
			return nil
		}

		connection, err := client.config.Connector.Connect(ctx)
		if err != nil {
			if terminal := client.terminalTransportFailure(err); terminal != nil {
				return terminal
			}
			failures++
			if retryError := client.retry(ctx, failures, err); retryError != nil {
				return retryError
			}
			continue
		}
		if !client.installConnection(ctx, connection) {
			_ = connection.Close()
			client.transition(StateStopped, FailureNone, failures)
			return nil
		}

		request := &codefluxv1.SubscribeSessionRequest{
			SessionId:     &codefluxv1.StableIdentity{Kind: client.config.SessionID.Kind, Value: client.config.SessionID.Value},
			AfterSequence: client.Status().LastSequence,
		}
		stream, err := connection.SubscribeSession(ctx, request)
		if err != nil {
			client.releaseConnection(connection)
			if terminal := client.terminalTransportFailure(err); terminal != nil {
				return terminal
			}
			failures++
			if retryError := client.retry(ctx, failures, err); retryError != nil {
				return retryError
			}
			continue
		}

		client.transition(StateReplaying, FailureNone, failures)
		streamError, madeProgress := client.receive(ctx, stream)
		client.releaseConnection(connection)
		if madeProgress {
			failures = 0
		}
		if ctx.Err() != nil {
			client.transition(StateStopped, FailureNone, failures)
			return nil
		}
		if terminal := client.terminalStreamFailure(streamError); terminal != nil {
			return terminal
		}
		failures++
		if retryError := client.retry(ctx, failures, streamError); retryError != nil {
			return retryError
		}
	}
}

func (client *Client) receive(ctx context.Context, stream Stream) (error, bool) {
	madeProgress := false
	for {
		response, err := stream.Recv()
		if err != nil {
			return err, madeProgress
		}
		event := response.GetEvent()
		if event == nil || event.GetSequence() == 0 {
			return ErrInvalidSessionEvent, madeProgress
		}
		if event.GetSessionId() == nil || event.GetSessionId().GetKind() != client.config.SessionID.GetKind() || event.GetSessionId().GetValue() != client.config.SessionID.GetValue() {
			return ErrSessionEventIdentityMismatch, madeProgress
		}

		last := client.Status().LastSequence
		if event.GetSequence() <= last {
			continue
		}
		if event.GetSequence() != last+1 {
			client.transition(StateGap, FailureProtocol, client.Status().ReconnectCount)
			return ErrSessionEventSequenceGap, madeProgress
		}
		if err := client.config.Apply(ctx, event); err != nil {
			return fmt.Errorf("%w: %v", ErrSessionEventApplicationFailed, err), madeProgress
		}
		madeProgress = true
		client.advance(event.GetSequence())
	}
}

func (client *Client) retry(ctx context.Context, failures int, cause error) error {
	if failures >= client.config.Retry.MaxAttempts {
		client.transition(StateFailed, FailureUnavailable, failures)
		return fmt.Errorf("%w: %v", ErrReconnectAttemptsExhausted, cause)
	}
	client.transition(StateReconnecting, FailureUnavailable, failures)
	if err := client.config.WaitForRetry(ctx, retryDelay(client.config.Retry, failures)); err != nil {
		client.transition(StateStopped, FailureNone, failures)
		return nil
	}
	client.transition(StateConnecting, FailureNone, failures)
	return nil
}

func (client *Client) terminalTransportFailure(err error) error {
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		client.transition(StateFailed, FailureAuthentication, client.Status().ReconnectCount)
		return err
	case codes.FailedPrecondition, codes.Unimplemented:
		client.transition(StateFailed, FailureIncompatible, client.Status().ReconnectCount)
		return err
	case codes.InvalidArgument:
		client.transition(StateFailed, FailureProtocol, client.Status().ReconnectCount)
		return err
	default:
		return nil
	}
}

func (client *Client) terminalStreamFailure(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	if errors.Is(err, ErrSessionEventApplicationFailed) {
		client.transition(StateFailed, FailureApplication, client.Status().ReconnectCount)
		return err
	}
	if errors.Is(err, ErrInvalidSessionEvent) || errors.Is(err, ErrSessionEventIdentityMismatch) {
		client.transition(StateFailed, FailureProtocol, client.Status().ReconnectCount)
		return err
	}
	if errors.Is(err, ErrSessionEventSequenceGap) {
		return nil
	}
	return client.terminalTransportFailure(err)
}

func (client *Client) installConnection(ctx context.Context, connection Connection) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	client.connection = connection
	return true
}

func (client *Client) releaseConnection(connection Connection) {
	client.mu.Lock()
	if client.connection != connection {
		client.mu.Unlock()
		return
	}
	client.connection = nil
	client.mu.Unlock()
	_ = connection.Close()
}

func (client *Client) advance(sequence uint64) {
	client.mu.Lock()
	client.status.LastSequence = sequence
	client.status.State = StateLive
	client.status.ControlsAllowed = true
	client.status.Failure = FailureNone
	snapshot := client.status
	observer := client.config.Observe
	client.mu.Unlock()
	if observer != nil {
		observer(snapshot)
	}
}

func (client *Client) transition(state State, failure FailureKind, reconnectCount int) {
	client.mu.Lock()
	client.status.State = state
	client.status.ReconnectCount = reconnectCount
	client.status.ControlsAllowed = state == StateLive
	client.status.Failure = failure
	snapshot := client.status
	observer := client.config.Observe
	client.mu.Unlock()
	if observer != nil {
		observer(snapshot)
	}
}

func normalizeRetryPolicy(policy RetryPolicy) RetryPolicy {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 5
	}
	if policy.InitialDelay <= 0 {
		policy.InitialDelay = 250 * time.Millisecond
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 4 * time.Second
	}
	if policy.MaxDelay < policy.InitialDelay {
		policy.MaxDelay = policy.InitialDelay
	}
	if policy.Multiplier < 1 {
		policy.Multiplier = 2
	}
	return policy
}

func retryDelay(policy RetryPolicy, failures int) time.Duration {
	delay := policy.InitialDelay
	for attempt := 1; attempt < failures; attempt++ {
		next := time.Duration(float64(delay) * policy.Multiplier)
		if next >= policy.MaxDelay || next < delay {
			return policy.MaxDelay
		}
		delay = next
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

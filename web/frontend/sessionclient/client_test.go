package sessionclient

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClientReconnectsFromLastAppliedSequenceWithoutDuplicates(t *testing.T) {
	identity := testSessionIdentity()
	first := &fakeConnection{stream: &scriptedStream{items: []streamItem{
		{response: testResponse(identity, 1)},
		{response: testResponse(identity, 3)},
	}}}
	second := &fakeConnection{stream: &scriptedStream{items: []streamItem{
		{response: testResponse(identity, 1)},
		{response: testResponse(identity, 2)},
		{response: testResponse(identity, 3)},
		{response: testReplayBoundary(3)},
		{err: status.Error(codes.PermissionDenied, "test terminal")},
	}}}
	connector := &fakeConnector{connections: []Connection{first, second}}
	var applied []uint64
	var observed []Status
	client := newTestClient(t, Config{
		Connector: connector,
		SessionID: identity,
		Retry: RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: time.Millisecond,
		},
		Apply: func(_ context.Context, event *codefluxv1.SessionEvent) error {
			applied = append(applied, event.GetSequence())
			return nil
		},
		Observe: func(status Status) {
			observed = append(observed, status)
		},
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Wait() error = %v, want PermissionDenied", err)
	}
	if got, want := applied, []uint64{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("applied sequences = %v, want %v", got, want)
	}
	if got, want := connector.afterSequences(), []uint64{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after_sequence values = %v, want %v", got, want)
	}
	if !hasState(observed, StateGap) || !hasState(observed, StateReconnecting) || !hasState(observed, StateLive) {
		t.Fatalf("observed states = %v, want gap, reconnecting, and live", observed)
	}
	for _, snapshot := range observed {
		if snapshot.ControlsAllowed != (snapshot.State == StateLive) {
			t.Fatalf("controls allowed in state %q", snapshot.State)
		}
	}
}

func TestClientStartsAfterConfiguredSequenceAndIgnoresReplayDuplicate(t *testing.T) {
	identity := testSessionIdentity()
	connection := &fakeConnection{stream: &scriptedStream{items: []streamItem{
		{response: testResponse(identity, 5)},
		{response: testResponse(identity, 6)},
		{response: testReplayBoundary(6)},
		{err: status.Error(codes.Unauthenticated, "test terminal")},
	}}}
	connector := &fakeConnector{connections: []Connection{connection}}
	var applied []uint64
	client := newTestClient(t, Config{
		Connector:     connector,
		SessionID:     identity,
		AfterSequence: 5,
		Apply: func(_ context.Context, event *codefluxv1.SessionEvent) error {
			applied = append(applied, event.GetSequence())
			return nil
		},
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Wait() error = %v, want Unauthenticated", err)
	}
	if got, want := connector.afterSequences(), []uint64{5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after_sequence values = %v, want %v", got, want)
	}
	if got, want := applied, []uint64{6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("applied sequences = %v, want %v", got, want)
	}
	if got := client.Status(); got.LastSequence != 6 || got.Failure != FailureAuthentication || got.ControlsAllowed {
		t.Fatalf("terminal status = %+v", got)
	}
}

func TestClientKeepsControlsDisabledUntilExactReplayBoundary(t *testing.T) {
	identity := testSessionIdentity()
	connection := &fakeConnection{stream: &scriptedStream{items: []streamItem{
		{response: testResponse(identity, 1)},
		{response: testResponse(identity, 2)},
		{response: testReplayBoundary(2)},
		{err: status.Error(codes.PermissionDenied, "test terminal")},
	}}}
	var observed []Status
	client := newTestClient(t, Config{
		Connector:    &fakeConnector{connections: []Connection{connection}},
		SessionID:    identity,
		Apply:        func(context.Context, *codefluxv1.SessionEvent) error { return nil },
		Observe:      func(status Status) { observed = append(observed, status) },
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Wait() error = %v", err)
	}
	var sawBoundary bool
	for _, status := range observed {
		if status.State == StateLive {
			sawBoundary = true
			if status.LastSequence != 2 || !status.ControlsAllowed {
				t.Fatalf("live boundary status = %+v", status)
			}
			continue
		}
		if status.LastSequence > 0 && status.ControlsAllowed {
			t.Fatalf("controls enabled before replay boundary: %+v", status)
		}
	}
	if !sawBoundary {
		t.Fatalf("replay boundary never transitioned live: %+v", observed)
	}
}

func TestClientRejectsReplayBoundaryThatDoesNotMatchAppliedCursor(t *testing.T) {
	identity := testSessionIdentity()
	connection := &fakeConnection{stream: &scriptedStream{items: []streamItem{
		{response: testResponse(identity, 1)},
		{response: testReplayBoundary(2)},
	}}}
	client := newTestClient(t, Config{
		Connector:    &fakeConnector{connections: []Connection{connection}},
		SessionID:    identity,
		Apply:        func(context.Context, *codefluxv1.SessionEvent) error { return nil },
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); !errors.Is(err, ErrInvalidReplayBoundary) {
		t.Fatalf("Wait() error = %v, want invalid replay boundary", err)
	}
	if got := client.Status(); got.State != StateFailed || got.Failure != FailureProtocol || got.ControlsAllowed {
		t.Fatalf("invalid boundary status = %+v", got)
	}
}

func TestClientBoundsReconnectAttemptsAndBackoff(t *testing.T) {
	connector := &fakeConnector{connectError: io.EOF}
	var delays []time.Duration
	client := newTestClient(t, Config{
		Connector: connector,
		SessionID: testSessionIdentity(),
		Retry: RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     15 * time.Millisecond,
			Multiplier:   2,
		},
		Apply: func(context.Context, *codefluxv1.SessionEvent) error { return nil },
		WaitForRetry: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	err := client.Wait()
	if !errors.Is(err, ErrReconnectAttemptsExhausted) {
		t.Fatalf("Wait() error = %v, want reconnect exhaustion", err)
	}
	if connector.connectCount() != 3 {
		t.Fatalf("connect count = %d, want 3", connector.connectCount())
	}
	if want := []time.Duration{10 * time.Millisecond, 15 * time.Millisecond}; !reflect.DeepEqual(delays, want) {
		t.Fatalf("retry delays = %v, want %v", delays, want)
	}
	if got := client.Status(); got.State != StateFailed || got.Failure != FailureUnavailable || got.ReconnectCount != 3 {
		t.Fatalf("terminal status = %+v", got)
	}
}

func TestClientDoesNotAdvanceCursorWhenReducerFails(t *testing.T) {
	identity := testSessionIdentity()
	connection := &fakeConnection{stream: &scriptedStream{items: []streamItem{{response: testResponse(identity, 1)}}}}
	client := newTestClient(t, Config{
		Connector: &fakeConnector{connections: []Connection{connection}},
		SessionID: identity,
		Apply: func(context.Context, *codefluxv1.SessionEvent) error {
			return errors.New("synthetic reducer failure")
		},
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); !errors.Is(err, ErrSessionEventApplicationFailed) {
		t.Fatalf("Wait() error = %v, want application failure", err)
	}
	if got := client.Status(); got.LastSequence != 0 || got.State != StateFailed || got.Failure != FailureApplication || got.ControlsAllowed {
		t.Fatalf("terminal status = %+v", got)
	}
}

func TestClientRepairsProjectionFromFreshSnapshotBeforeResubscribing(t *testing.T) {
	identity := testSessionIdentity()
	first := &fakeConnection{stream: &scriptedStream{items: []streamItem{
		{response: testResponse(identity, 1)},
	}}}
	second := &fakeConnection{stream: &scriptedStream{items: []streamItem{
		{response: testReplayBoundary(5)},
		{response: testResponse(identity, 6)},
		{err: status.Error(codes.PermissionDenied, "test terminal")},
	}}}
	connector := &fakeConnector{connections: []Connection{first, second}}
	var applied []uint64
	repairs := 0
	client := newTestClient(t, Config{
		Connector: connector, SessionID: identity,
		Apply: func(_ context.Context, event *codefluxv1.SessionEvent) error {
			applied = append(applied, event.GetSequence())
			if event.GetSequence() == 1 {
				return errors.New("projection inconsistency")
			}
			return nil
		},
		Repair: func(context.Context) (uint64, error) {
			repairs++
			return 5, nil
		},
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Wait() error = %v", err)
	}
	if repairs != 1 || !reflect.DeepEqual(connector.afterSequences(), []uint64{0, 5}) ||
		!reflect.DeepEqual(applied, []uint64{1, 6}) {
		t.Fatalf("repairs=%d after=%v applied=%v", repairs, connector.afterSequences(), applied)
	}
}

func TestClientRejectsMismatchedSessionEvent(t *testing.T) {
	connection := &fakeConnection{stream: &scriptedStream{items: []streamItem{{response: testResponse(
		&codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, Value: "ses-other"},
		1,
	)}}}}
	client := newTestClient(t, Config{
		Connector:    &fakeConnector{connections: []Connection{connection}},
		SessionID:    testSessionIdentity(),
		Apply:        func(context.Context, *codefluxv1.SessionEvent) error { return nil },
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); !errors.Is(err, ErrSessionEventIdentityMismatch) {
		t.Fatalf("Wait() error = %v, want identity mismatch", err)
	}
	if got := client.Status(); got.State != StateFailed || got.Failure != FailureProtocol {
		t.Fatalf("terminal status = %+v", got)
	}
}

func TestClientCloseCancelsReceiveAndClosesConnectionOnce(t *testing.T) {
	subscribed := make(chan struct{})
	connection := &fakeConnection{
		subscribeSignal: subscribed,
		blocking:        true,
	}
	client := newTestClient(t, Config{
		Connector:    &fakeConnector{connections: []Connection{connection}},
		SessionID:    testSessionIdentity(),
		Apply:        func(context.Context, *codefluxv1.SessionEvent) error { return nil },
		WaitForRetry: noRetryWait,
	})
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-subscribed:
	case <-time.After(time.Second):
		t.Fatal("subscription did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if connection.closeCount() != 1 {
		t.Fatalf("connection close count = %d, want 1", connection.closeCount())
	}
	if got := client.Status(); got.State != StateStopped || got.ControlsAllowed {
		t.Fatalf("closed status = %+v", got)
	}
}

func TestClientStartAndNativeBrowserBoundaries(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() accepted an empty configuration")
	}
	client := newTestClient(t, Config{
		Connector:    &fakeConnector{connectError: status.Error(codes.PermissionDenied, "stop")},
		SessionID:    testSessionIdentity(),
		Apply:        func(context.Context, *codefluxv1.SessionEvent) error { return nil },
		WaitForRetry: noRetryWait,
	})
	if err := client.Wait(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Wait() error = %v, want not started", err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start() error = %v, want already started", err)
	}
	_ = client.Wait()
	if _, err := (BrowserConnector{}).Connect(t.Context()); !errors.Is(err, ErrBrowserTransportUnavailable) {
		t.Fatalf("native BrowserConnector.Connect() error = %v", err)
	}
}

func newTestClient(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func testSessionIdentity() *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION,
		Value: "ses-test",
	}
}

func testResponse(identity *codefluxv1.StableIdentity, sequence uint64) *codefluxv1.SubscribeSessionResponse {
	return &codefluxv1.SubscribeSessionResponse{Event: &codefluxv1.SessionEvent{
		Sequence:  sequence,
		SessionId: &codefluxv1.StableIdentity{Kind: identity.GetKind(), Value: identity.GetValue()},
	}}
}

func testReplayBoundary(sequence uint64) *codefluxv1.SubscribeSessionResponse {
	return &codefluxv1.SubscribeSessionResponse{
		ReplayBoundary: &codefluxv1.SessionReplayBoundary{ThroughSequence: sequence},
	}
}

func noRetryWait(context.Context, time.Duration) error { return nil }

func hasState(statuses []Status, want State) bool {
	for _, status := range statuses {
		if status.State == want {
			return true
		}
	}
	return false
}

type streamItem struct {
	response *codefluxv1.SubscribeSessionResponse
	err      error
}

type scriptedStream struct {
	items []streamItem
	next  int
}

func (stream *scriptedStream) Recv() (*codefluxv1.SubscribeSessionResponse, error) {
	if stream.next >= len(stream.items) {
		return nil, io.EOF
	}
	item := stream.items[stream.next]
	stream.next++
	return item.response, item.err
}

type contextStream struct {
	ctx context.Context
}

func (stream *contextStream) Recv() (*codefluxv1.SubscribeSessionResponse, error) {
	<-stream.ctx.Done()
	return nil, stream.ctx.Err()
}

type fakeConnection struct {
	mu              sync.Mutex
	stream          Stream
	blocking        bool
	subscribeSignal chan struct{}
	requests        []*codefluxv1.SubscribeSessionRequest
	closes          int
}

func (connection *fakeConnection) SubscribeSession(ctx context.Context, request *codefluxv1.SubscribeSessionRequest) (Stream, error) {
	connection.mu.Lock()
	connection.requests = append(connection.requests, &codefluxv1.SubscribeSessionRequest{
		SessionId: &codefluxv1.StableIdentity{
			Kind:  request.GetSessionId().GetKind(),
			Value: request.GetSessionId().GetValue(),
		},
		AfterSequence: request.GetAfterSequence(),
	})
	signal := connection.subscribeSignal
	if signal != nil {
		connection.subscribeSignal = nil
	}
	stream := connection.stream
	blocking := connection.blocking
	connection.mu.Unlock()
	if signal != nil {
		close(signal)
	}
	if blocking {
		return &contextStream{ctx: ctx}, nil
	}
	return stream, nil
}

func (connection *fakeConnection) Close() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.closes++
	return nil
}

func (connection *fakeConnection) closeCount() int {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.closes
}

type fakeConnector struct {
	mu           sync.Mutex
	connections  []Connection
	connectError error
	next         int
	requests     []*codefluxv1.SubscribeSessionRequest
}

func (connector *fakeConnector) Connect(context.Context) (Connection, error) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	if connector.next < len(connector.connections) {
		connection := connector.connections[connector.next]
		connector.next++
		return &requestRecordingConnection{Connection: connection, connector: connector}, nil
	}
	connector.next++
	if connector.connectError != nil {
		return nil, connector.connectError
	}
	return nil, io.EOF
}

func (connector *fakeConnector) connectCount() int {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	return connector.next
}

func (connector *fakeConnector) afterSequences() []uint64 {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	sequences := make([]uint64, 0, len(connector.requests))
	for _, request := range connector.requests {
		sequences = append(sequences, request.GetAfterSequence())
	}
	return sequences
}

type requestRecordingConnection struct {
	Connection
	connector *fakeConnector
}

func (connection *requestRecordingConnection) SubscribeSession(ctx context.Context, request *codefluxv1.SubscribeSessionRequest) (Stream, error) {
	connection.connector.mu.Lock()
	connection.connector.requests = append(connection.connector.requests, &codefluxv1.SubscribeSessionRequest{
		SessionId: &codefluxv1.StableIdentity{
			Kind:  request.GetSessionId().GetKind(),
			Value: request.GetSessionId().GetValue(),
		},
		AfterSequence: request.GetAfterSequence(),
	})
	connection.connector.mu.Unlock()
	return connection.Connection.SubscribeSession(ctx, request)
}

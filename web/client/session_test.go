package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

func TestStartAuthenticatedSessionDoesNotDialWithoutSelectedSession(t *testing.T) {
	connector := &recordingConnector{}
	client, err := startAuthenticatedSession(
		t.Context(),
		bootstrapEnvelope{},
		connector,
		func(sessionclient.Status) {},
		func(context.Context, *codefluxv1.SessionEvent) error { return nil },
	)
	if err != nil || client != nil {
		t.Fatalf("start without selected session = (%v, %v), want (nil, nil)", client, err)
	}
	if connector.calls() != 0 {
		t.Fatalf("connector calls = %d, want 0", connector.calls())
	}
}

func TestStartAuthenticatedSessionRejectsInvalidIdentityBeforeDial(t *testing.T) {
	for _, identity := range []*codefluxv1.StableIdentity{
		{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: "tsk-not-a-session"},
		{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, Value: "not-canonical"},
	} {
		connector := &recordingConnector{}
		client, err := startAuthenticatedSession(
			t.Context(),
			bootstrapEnvelope{SelectedSessionID: identity},
			connector,
			func(sessionclient.Status) {},
			func(context.Context, *codefluxv1.SessionEvent) error { return nil },
		)
		if !errors.Is(err, errInvalidSelectedSession) || client != nil {
			t.Fatalf("invalid selected session = (%v, %v)", client, err)
		}
		if connector.calls() != 0 {
			t.Fatalf("connector calls = %d, want 0", connector.calls())
		}
	}
}

func TestStartAuthenticatedSessionOwnsSelectedSessionLifecycle(t *testing.T) {
	connected := make(chan struct{})
	connection := &blockingConnection{connected: connected}
	connector := &recordingConnector{connection: connection}
	client, err := startAuthenticatedSession(
		context.Background(),
		bootstrapEnvelope{SelectedSessionID: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION,
			Value: "ses_018f0123-4567-789a-8bcd-ef0123456789",
		}},
		connector,
		func(sessionclient.Status) {},
		func(context.Context, *codefluxv1.SessionEvent) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("selected session did not connect")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if connector.calls() != 1 || connection.closes() != 1 {
		t.Fatalf("connect calls = %d, closes = %d", connector.calls(), connection.closes())
	}
}

func TestSessionViewForLifecycleKeepsUncertainControlsOfflineOrRecovering(t *testing.T) {
	tests := []struct {
		status sessionclient.Status
		want   frontendstate.ConnectionState
	}{
		{status: sessionclient.Status{State: sessionclient.StateConnecting}, want: frontendstate.ConnectionConnecting},
		{status: sessionclient.Status{State: sessionclient.StateReplaying}, want: frontendstate.ConnectionRecovering},
		{status: sessionclient.Status{State: sessionclient.StateGap}, want: frontendstate.ConnectionRecovering},
		{status: sessionclient.Status{State: sessionclient.StateLive}, want: frontendstate.ConnectionLive},
		{status: sessionclient.Status{State: sessionclient.StateFailed, Failure: sessionclient.FailureAuthentication}, want: frontendstate.ConnectionOffline},
	}
	for _, test := range tests {
		got := sessionViewForLifecycle(test.status)
		if got.Bootstrap != frontendstate.BootstrapReady || got.Connection != test.want {
			t.Fatalf("status %+v mapped to %+v, want %q", test.status, got, test.want)
		}
	}
}

type recordingConnector struct {
	mu         sync.Mutex
	count      int
	connection sessionclient.Connection
}

func (connector *recordingConnector) Connect(context.Context) (sessionclient.Connection, error) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	connector.count++
	if connector.connection == nil {
		return nil, errors.New("unexpected connection")
	}
	return connector.connection, nil
}

func (connector *recordingConnector) calls() int {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	return connector.count
}

type blockingConnection struct {
	mu         sync.Mutex
	connected  chan struct{}
	closeCount int
}

func (connection *blockingConnection) SubscribeSession(ctx context.Context, _ *codefluxv1.SubscribeSessionRequest) (sessionclient.Stream, error) {
	close(connection.connected)
	return blockingStream{ctx: ctx}, nil
}

func (connection *blockingConnection) Close() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.closeCount++
	return nil
}

func (connection *blockingConnection) closes() int {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.closeCount
}

type blockingStream struct{ ctx context.Context }

func (stream blockingStream) Recv() (*codefluxv1.SubscribeSessionResponse, error) {
	<-stream.ctx.Done()
	return nil, stream.ctx.Err()
}

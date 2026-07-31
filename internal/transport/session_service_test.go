package transport

import (
	"context"
	"net"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type fixedSessionJournal struct{ values []events.SessionEvent }

func (journal fixedSessionJournal) ReplayCommitted(_ context.Context, sessionID domain.SessionID, after uint64, limit int) ([]events.SessionEvent, error) {
	result := make([]events.SessionEvent, 0, limit)
	for _, event := range journal.values {
		if event.SessionID == sessionID && event.Sequence > after && len(result) < limit {
			result = append(result, event)
		}
	}
	return result, nil
}
func (journal fixedSessionJournal) CommittedSequence(_ context.Context, sessionID domain.SessionID) (uint64, error) {
	var sequence uint64
	for _, event := range journal.values {
		if event.SessionID == sessionID && event.Sequence > sequence {
			sequence = event.Sequence
		}
	}
	return sequence, nil
}

func TestSessionServiceReplaysTypedThreadLifecycleEvent(t *testing.T) {
	sessionID, _ := domain.ParseSessionID("ses_01890f3c-4a00-7abc-8def-0123456789ab")
	threadID, _ := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	workspaceID, _ := domain.ParseWorkspaceID("wsp_01890f3c-4a00-7abc-8def-0123456789ab")
	event, err := (events.NewSessionEvent{
		SessionID: sessionID, ThreadID: threadID, Kind: events.KindThreadCreated,
		PayloadVersion: 1, Revision: 1,
		Payload: events.Payload{ThreadCreated: &events.ThreadCreated{WorkspaceID: &workspaceID, Title: "Authoritative"}},
	}).Build(1, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	hub, err := events.NewHub(fixedSessionJournal{values: []events.SessionEvent{event}}, 8)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewSessionService(hub)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	codefluxv1.RegisterSessionServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := grpc.NewClient("passthrough:///session", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	stream, err := codefluxv1.NewSessionServiceClient(connection).SubscribeSession(ctx, &codefluxv1.SubscribeSessionRequest{SessionId: &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, Value: sessionID.String()}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if response.GetEvent().GetKind() != codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_CREATED || response.GetEvent().GetThreadCreated().GetTitle() != "Authoritative" || response.GetEvent().GetThreadCreated().GetWorkspaceId().GetValue() != workspaceID.String() {
		t.Fatalf("replayed event = %#v", response.GetEvent())
	}
	boundary, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if boundary.GetEvent() != nil || boundary.GetReplayBoundary().GetThroughSequence() != event.Sequence {
		t.Fatalf("replay boundary = %#v", boundary)
	}
}

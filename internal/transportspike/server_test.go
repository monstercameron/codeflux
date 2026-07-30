package transportspike

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const testLaunchSecret = "0123456789abcdef0123456789abcdef"

func TestEmbeddedBridgeStreamsTenThousandOrderedEvents(t *testing.T) {
	client, closeClient, service := startConformanceClient(t, testLaunchSecret)
	defer closeClient()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	health, err := client.CheckHealth(ctx, &codefluxv1.TransportSpikeServiceCheckHealthRequest{})
	if err != nil {
		t.Fatalf("check health: %v", err)
	}
	if health.GetStatus() != "ready" {
		t.Fatalf("health status = %q, want ready", health.GetStatus())
	}

	stream, err := client.SubscribeSession(ctx, &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
		AfterSequence: 41,
		EventCount:    10_000,
		PayloadBytes:  32,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	for want := uint64(42); want < 10_042; want++ {
		event, receiveErr := stream.Recv()
		if receiveErr != nil {
			t.Fatalf("receive sequence %d: %v", want, receiveErr)
		}
		if event.GetSequence() != want {
			t.Fatalf("sequence = %d, want %d", event.GetSequence(), want)
		}
		if len(event.GetPayload()) != 32 {
			t.Fatalf("payload length = %d, want 32", len(event.GetPayload()))
		}
	}
	if _, err = stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal receive error = %v, want EOF", err)
	}
	metrics := service.Metrics()
	if metrics.ActiveStreams != 0 || metrics.CompletedStreams != 1 || metrics.SentEvents != 10_000 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestSubscriptionCancelAndReconnectAfterSequence(t *testing.T) {
	client, closeClient, service := startConformanceClient(t, testLaunchSecret)
	defer closeClient()

	streamContext, cancelStream := context.WithCancel(context.Background())
	stream, err := client.SubscribeSession(streamContext, &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
		EventCount:        10_000,
		PayloadBytes:      8,
		BatchMilliseconds: 1,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	var last uint64
	for range 25 {
		event, receiveErr := stream.Recv()
		if receiveErr != nil {
			t.Fatalf("receive before cancel: %v", receiveErr)
		}
		last = event.GetSequence()
	}
	cancelStream()
	for buffered := 0; ; buffered++ {
		if buffered > 256 {
			t.Fatal("subscription continued beyond bounded buffered events after cancellation")
		}
		if _, err = stream.Recv(); err != nil {
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resumed, err := client.SubscribeSession(ctx, &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
		AfterSequence: last,
		EventCount:    75,
	})
	if err != nil {
		t.Fatalf("resume subscription: %v", err)
	}
	for want := last + 1; want <= last+75; want++ {
		event, receiveErr := resumed.Recv()
		if receiveErr != nil {
			t.Fatalf("receive resumed sequence %d: %v", want, receiveErr)
		}
		if event.GetSequence() != want {
			t.Fatalf("resumed sequence = %d, want %d", event.GetSequence(), want)
		}
	}
	if _, err = resumed.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("resumed terminal receive = %v, want EOF", err)
	}
	waitFor(t, time.Second, func() bool {
		metrics := service.Metrics()
		return metrics.CancelledStreams == 1 && metrics.ActiveStreams == 0
	})
}

func TestBridgeRejectsWrongOriginAndLaunchSecret(t *testing.T) {
	listener, server, _ := startConformanceServer(t, testLaunchSecret)
	defer server.Shutdown(context.Background())

	tests := []struct {
		name   string
		origin string
		cookie string
	}{
		{name: "cross origin", origin: "http://evil.invalid", cookie: CookieHeader(testLaunchSecret)},
		{name: "missing origin", cookie: CookieHeader(testLaunchSecret)},
		{name: "wrong secret", origin: OriginForListener(listener), cookie: CookieHeader("abcdef0123456789abcdef0123456789")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			connection, err := grpctunnel.DialContext(
				ctx,
				WebSocketURL(OriginForListener(listener)),
				grpctunnel.WithHeader("Origin", test.origin),
				grpctunnel.WithHeader("Cookie", test.cookie),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err == nil {
				defer connection.Close()
				client := codefluxv1.NewTransportSpikeServiceClient(connection)
				_, err = client.CheckHealth(ctx, &codefluxv1.TransportSpikeServiceCheckHealthRequest{})
			}
			if err == nil {
				t.Fatal("unauthorized bridge request succeeded")
			}
		})
	}
}

func TestBridgeEnforcesRequestBoundsAndMaximumPayload(t *testing.T) {
	client, closeClient, _ := startConformanceClient(t, testLaunchSecret)
	defer closeClient()

	tests := []struct {
		name    string
		request *codefluxv1.TransportSpikeServiceSubscribeSessionRequest
	}{
		{
			name:    "zero events",
			request: &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{},
		},
		{
			name: "too many events",
			request: &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
				EventCount: MaxSyntheticEvents + 1,
			},
		},
		{
			name: "oversize payload",
			request: &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
				EventCount:   1,
				PayloadBytes: MaxSyntheticPayloadSize + 1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream, err := client.SubscribeSession(context.Background(), test.request)
			if err == nil {
				_, err = stream.Recv()
			}
			if status.Code(err).String() == "OK" {
				t.Fatalf("request unexpectedly succeeded: %v", err)
			}
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.SubscribeSession(ctx, &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
		EventCount:   1,
		PayloadBytes: MaxSyntheticPayloadSize,
	})
	if err != nil {
		t.Fatalf("subscribe maximum payload: %v", err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive maximum payload: %v", err)
	}
	if len(event.GetPayload()) != MaxSyntheticPayloadSize {
		t.Fatalf("maximum payload length = %d", len(event.GetPayload()))
	}
}

func TestEditorCapabilityRemainsValidatedAndApprovalGated(t *testing.T) {
	client, closeClient, _ := startConformanceClient(t, testLaunchSecret)
	defer closeClient()

	response, err := client.CheckEditorCapability(
		context.Background(),
		&codefluxv1.TransportSpikeServiceCheckEditorCapabilityRequest{
			RelativePath: "internal/transportspike/server.go",
			Line:         1,
			Column:       1,
		},
	)
	if err != nil {
		t.Fatalf("check valid capability: %v", err)
	}
	if response.GetDecision() != "requires-explicit-approval" {
		t.Fatalf("capability decision = %q", response.GetDecision())
	}

	for _, path := range []string{"", "../outside.go", `C:\outside.go`, "/outside.go"} {
		_, err = client.CheckEditorCapability(
			context.Background(),
			&codefluxv1.TransportSpikeServiceCheckEditorCapabilityRequest{
				RelativePath: path,
				Line:         1,
				Column:       1,
			},
		)
		if status.Code(err).String() == "OK" {
			t.Fatalf("unsafe editor path %q succeeded", path)
		}
	}
}

func TestListenLoopbackUsesLoopbackAddress(t *testing.T) {
	listener, err := ListenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type = %T", listener.Addr())
	}
	if !address.IP.IsLoopback() {
		t.Fatalf("listener address = %s, want loopback", address.IP)
	}
}

func TestListenLoopbackRejectsNonLoopbackAddress(t *testing.T) {
	if _, err := ListenLoopbackAt("0.0.0.0:0"); err == nil {
		t.Fatal("wildcard listener address accepted")
	}
	if _, err := ListenLoopbackAt("localhost:0"); err == nil {
		t.Fatal("non-literal listener address accepted")
	}
}

func TestTunnelFramingAndSerializationOverhead(t *testing.T) {
	rawListener, err := ListenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	listener := &countingListener{Listener: rawListener}
	service := &Service{}
	bridge, err := NewHandler(service, testLaunchSecret)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/grpc", bridge)
	server := NewHTTPServer(mux)
	go func() { _ = server.Serve(listener) }()
	defer server.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := grpctunnel.DialContext(
		ctx,
		WebSocketURL(OriginForListener(listener)),
		grpctunnel.WithHeader("Origin", OriginForListener(listener)),
		grpctunnel.WithHeader("Cookie", CookieHeader(testLaunchSecret)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := codefluxv1.NewTransportSpikeServiceClient(connection)
	if _, err := client.CheckHealth(ctx, &codefluxv1.TransportSpikeServiceCheckHealthRequest{}); err != nil {
		t.Fatal(err)
	}
	baseline := listener.written.Load()

	const eventCount = 10_000
	stream, err := client.SubscribeSession(ctx, &codefluxv1.TransportSpikeServiceSubscribeSessionRequest{
		EventCount:   eventCount,
		PayloadBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	var protobufBytes uint64
	for range eventCount {
		event, receiveErr := stream.Recv()
		if receiveErr != nil {
			t.Fatal(receiveErr)
		}
		protobufBytes += uint64(proto.Size(event))
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal receive = %v, want EOF", err)
	}
	tunnelBytes := listener.written.Load() - baseline
	if tunnelBytes < protobufBytes {
		t.Fatalf("tunnel bytes %d smaller than protobuf bytes %d", tunnelBytes, protobufBytes)
	}
	ratio := float64(tunnelBytes) / float64(protobufBytes)
	t.Logf(
		"10,000 events: protobuf=%d bytes, tunnel=%d bytes, framing=%d bytes, ratio=%.3f",
		protobufBytes,
		tunnelBytes,
		tunnelBytes-protobufBytes,
		ratio,
	)
	if ratio > 3 {
		t.Fatalf("measured framing ratio %.3f exceeds bounded spike threshold 3.0", ratio)
	}
}

func TestHandlerRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewHandler(nil, testLaunchSecret); err == nil {
		t.Fatal("nil service accepted")
	}
	if _, err := NewHandler(&Service{}, "short"); err == nil {
		t.Fatal("short launch secret accepted")
	}
}

func BenchmarkTransportSpikeEventSerialization(b *testing.B) {
	event := &codefluxv1.TransportSpikeServiceSubscribeSessionResponse{Sequence: 10_000, Payload: make([]byte, 32)}
	b.ReportAllocs()
	b.SetBytes(int64(proto.Size(event)))
	for b.Loop() {
		if _, err := proto.Marshal(event); err != nil {
			b.Fatal(err)
		}
	}
}

func startConformanceClient(
	t *testing.T,
	secret string,
) (codefluxv1.TransportSpikeServiceClient, func(), *Service) {
	t.Helper()
	listener, server, service := startConformanceServer(t, secret)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	connection, err := grpctunnel.DialContext(
		ctx,
		WebSocketURL(OriginForListener(listener)),
		grpctunnel.WithHeader("Origin", OriginForListener(listener)),
		grpctunnel.WithHeader("Cookie", CookieHeader(secret)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGRPCMessageSize)),
	)
	cancel()
	if err != nil {
		server.Shutdown(context.Background())
		t.Fatalf("dial bridge: %v", err)
	}
	return codefluxv1.NewTransportSpikeServiceClient(connection), func() {
		listener.closeConnections()
		_ = server.Close()
		waitForClientTransportExit(connection)
		_ = connection.Close()
	}, service
}

func startConformanceServer(t *testing.T, secret string) (*trackedListener, *http.Server, *Service) {
	t.Helper()
	rawListener, err := ListenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	listener := &trackedListener{Listener: rawListener, connections: make(map[net.Conn]struct{})}
	service := &Service{}
	bridge, err := NewHandler(service, secret)
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/grpc", bridge)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
	})
	return listener, server, service
}

func waitForClientTransportExit(connection *grpc.ClientConn) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		state := connection.GetState()
		if state != connectivity.Ready && state != connectivity.Connecting {
			return
		}
		if !connection.WaitForStateChange(ctx, state) {
			return
		}
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}

type countingListener struct {
	net.Listener
	written atomic.Uint64
}

type trackedListener struct {
	net.Listener
	mu          sync.Mutex
	connections map[net.Conn]struct{}
}

func (listener *trackedListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	listener.mu.Lock()
	listener.connections[connection] = struct{}{}
	listener.mu.Unlock()
	return connection, nil
}

func (listener *trackedListener) closeConnections() {
	listener.mu.Lock()
	connections := make([]net.Conn, 0, len(listener.connections))
	for connection := range listener.connections {
		connections = append(connections, connection)
	}
	listener.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func (listener *countingListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &countingConnection{Conn: connection, written: &listener.written}, nil
}

type countingConnection struct {
	net.Conn
	written *atomic.Uint64
}

func (connection *countingConnection) Write(value []byte) (int, error) {
	count, err := connection.Conn.Write(value)
	connection.written.Add(uint64(count))
	return count, err
}

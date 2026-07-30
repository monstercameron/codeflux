// Package transportspike retains the bounded transport conformance fixture
// selected by Milestone 06. Product RPCs are defined by later milestones.
package transportspike

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// SessionCookieName is the browser cookie carrying the per-launch secret.
	SessionCookieName = "codeflux-launch"

	MaxSyntheticEvents      = 20_000
	MaxSyntheticPayloadSize = 4 << 20
	maxGRPCMessageSize      = MaxSyntheticPayloadSize + 1024
	maxBridgeReadSize       = 4 << 20
)

var errUnauthorizedLaunch = errors.New("invalid launch session")

// Service implements the retained unary and server-streaming transport
// conformance contract.
type Service struct {
	codefluxv1.UnimplementedTransportSpikeServiceServer

	activeStreams    atomic.Int64
	completedStreams atomic.Int64
	cancelledStreams atomic.Int64
	sentEvents       atomic.Uint64
}

// Metrics is a point-in-time view of transport-spike activity.
type Metrics struct {
	ActiveStreams    int64
	CompletedStreams int64
	CancelledStreams int64
	SentEvents       uint64
}

// CheckHealth verifies typed unary transport connectivity.
func (service *Service) CheckHealth(
	context.Context,
	*codefluxv1.TransportSpikeServiceCheckHealthRequest,
) (*codefluxv1.TransportSpikeServiceCheckHealthResponse, error) {
	return &codefluxv1.TransportSpikeServiceCheckHealthResponse{Status: "ready"}, nil
}

// CheckEditorCapability validates a workspace-relative editor target without
// launching a process or granting the browser ambient filesystem authority.
func (service *Service) CheckEditorCapability(
	_ context.Context,
	request *codefluxv1.TransportSpikeServiceCheckEditorCapabilityRequest,
) (*codefluxv1.TransportSpikeServiceCheckEditorCapabilityResponse, error) {
	path := strings.TrimSpace(request.GetRelativePath())
	clean := filepath.Clean(path)
	if path == "" ||
		strings.Contains(path, `\`) ||
		(len(path) >= 2 && path[1] == ':') ||
		filepath.IsAbs(path) ||
		strings.HasPrefix(path, "/") ||
		clean == "." ||
		clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, status.Error(codes.InvalidArgument, "editor path must remain workspace-relative")
	}
	if request.GetLine() == 0 || request.GetColumn() == 0 {
		return nil, status.Error(codes.InvalidArgument, "editor line and column must be positive")
	}
	return &codefluxv1.TransportSpikeServiceCheckEditorCapabilityResponse{
		Decision: "requires-explicit-approval",
	}, nil
}

// SubscribeSession emits a bounded, strictly ordered synthetic stream.
func (service *Service) SubscribeSession(
	request *codefluxv1.TransportSpikeServiceSubscribeSessionRequest,
	stream grpc.ServerStreamingServer[codefluxv1.TransportSpikeServiceSubscribeSessionResponse],
) error {
	if request.GetEventCount() == 0 || request.GetEventCount() > MaxSyntheticEvents {
		return status.Errorf(
			codes.InvalidArgument,
			"event_count must be between 1 and %d",
			MaxSyntheticEvents,
		)
	}
	if request.GetPayloadBytes() > MaxSyntheticPayloadSize {
		return status.Errorf(
			codes.ResourceExhausted,
			"payload_bytes exceeds %d",
			MaxSyntheticPayloadSize,
		)
	}
	if request.GetBatchMilliseconds() > 1000 {
		return status.Error(codes.InvalidArgument, "batch_milliseconds exceeds 1000")
	}

	service.activeStreams.Add(1)
	defer service.activeStreams.Add(-1)

	payload := make([]byte, request.GetPayloadBytes())
	for index := range payload {
		payload[index] = byte(index % 251)
	}
	delay := time.Duration(request.GetBatchMilliseconds()) * time.Millisecond
	for offset := uint64(1); offset <= uint64(request.GetEventCount()); offset++ {
		if delay > 0 && (offset-1)%128 == 0 {
			timer := time.NewTimer(delay)
			select {
			case <-stream.Context().Done():
				if !timer.Stop() {
					<-timer.C
				}
				service.cancelledStreams.Add(1)
				return status.FromContextError(stream.Context().Err()).Err()
			case <-timer.C:
			}
		}
		if err := stream.Context().Err(); err != nil {
			service.cancelledStreams.Add(1)
			return status.FromContextError(err).Err()
		}
		event := &codefluxv1.TransportSpikeServiceSubscribeSessionResponse{
			Sequence: request.GetAfterSequence() + offset,
			Payload:  payload,
		}
		if err := stream.Send(event); err != nil {
			if stream.Context().Err() != nil {
				service.cancelledStreams.Add(1)
			}
			return err
		}
		service.sentEvents.Add(1)
	}
	service.completedStreams.Add(1)
	return nil
}

// Metrics returns current stream lifecycle counters.
func (service *Service) Metrics() Metrics {
	return Metrics{
		ActiveStreams:    service.activeStreams.Load(),
		CompletedStreams: service.completedStreams.Load(),
		CancelledStreams: service.cancelledStreams.Load(),
		SentEvents:       service.sentEvents.Load(),
	}
}

// NewHandler creates the embedded WebSocket-to-gRPC bridge. The launch secret
// is intentionally supplied by the caller and never persisted by this package.
func NewHandler(service *Service, launchSecret string) (http.Handler, error) {
	if service == nil {
		return nil, errors.New("transport spike service is nil")
	}
	if len(launchSecret) < 32 {
		return nil, errors.New("launch secret must contain at least 32 characters")
	}

	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxGRPCMessageSize),
		grpc.MaxSendMsgSize(maxGRPCMessageSize),
	)
	codefluxv1.RegisterTransportSpikeServiceServer(grpcServer, service)
	return grpctunnel.Wrap(
		grpcServer,
		grpctunnel.WithOriginCheck(sameOrigin),
		grpctunnel.WithAuthorize(authorizeLaunch(launchSecret)),
		grpctunnel.WithReadLimitBytes(maxBridgeReadSize),
		grpctunnel.WithSessionMaxLifetime(30*time.Minute),
	), nil
}

// ListenLoopback opens a TCP listener that cannot accept non-loopback traffic.
func ListenLoopback() (net.Listener, error) {
	return ListenLoopbackAt("127.0.0.1:0")
}

// ListenLoopbackAt opens a loopback TCP listener at a caller-selected port.
func ListenLoopbackAt(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse loopback address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("listener address must use a literal loopback IP")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on loopback: %w", err)
	}
	return listener, nil
}

func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	wantScheme := "http"
	if request.TLS != nil {
		wantScheme = "https"
	}
	return parsed.Scheme == wantScheme && parsed.Host == request.Host
}

func authorizeLaunch(want string) func(*http.Request) error {
	return func(request *http.Request) error {
		cookie, err := request.Cookie(SessionCookieName)
		if err != nil {
			return errUnauthorizedLaunch
		}
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(want)) != 1 {
			return errUnauthorizedLaunch
		}
		return nil
	}
}

// SessionCookie creates the loopback-only browser authentication cookie.
func SessionCookie(launchSecret string) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    launchSecret,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false,
		MaxAge:   0,
	}
}

// CookieHeader returns the exact Cookie header used by native conformance
// clients. Browsers attach the same cookie automatically.
func CookieHeader(launchSecret string) string {
	return SessionCookieName + "=" + url.QueryEscape(launchSecret)
}

// OriginForListener returns the HTTP origin for a loopback listener.
func OriginForListener(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

// WebSocketURL returns the bridge URL rooted at an HTTP origin.
func WebSocketURL(origin string) string {
	return "ws" + origin[len("http"):] + "/grpc"
}

// SequenceLabel formats an event sequence for browser diagnostics.
func SequenceLabel(sequence uint64) string {
	return strconv.FormatUint(sequence, 10)
}

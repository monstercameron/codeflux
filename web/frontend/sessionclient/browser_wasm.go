//go:build js && wasm

package sessionclient

import (
	"context"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// BrowserConnector dials the fixed same-origin bridge path. Authentication is
// supplied only by the browser-managed HttpOnly launch cookie.
type BrowserConnector struct {
	Reconnect TunnelReconnectPolicy
}

// Connect establishes a GoGRPCBridge connection without accepting headers,
// tokens, origins, or persistent browser storage.
func (connector BrowserConnector) Connect(ctx context.Context) (Connection, error) {
	reconnect := connector.Reconnect
	connection, err := grpctunnel.DialContext(
		ctx,
		BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpctunnel.WithReconnectPolicy(grpctunnel.ReconnectConfig{
			InitialDelay:      reconnect.InitialDelay,
			MaxDelay:          reconnect.MaxDelay,
			Multiplier:        reconnect.Multiplier,
			Jitter:            reconnect.Jitter,
			MinConnectTimeout: reconnect.MinConnectTimeout,
		}),
	)
	if err != nil {
		return nil, err
	}
	return &grpcConnection{connection: connection, client: codefluxv1.NewSessionServiceClient(connection)}, nil
}

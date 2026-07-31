//go:build js && wasm

package sessionclient

import (
	"context"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/grpc"
)

type grpcConnection struct {
	connection *grpc.ClientConn
	client     codefluxv1.SessionServiceClient
}

func (connection *grpcConnection) SubscribeSession(ctx context.Context, request *codefluxv1.SubscribeSessionRequest) (Stream, error) {
	return connection.client.SubscribeSession(ctx, request)
}

func (connection *grpcConnection) Close() error {
	return connection.connection.Close()
}

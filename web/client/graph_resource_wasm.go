//go:build js && wasm

package main

import (
	"context"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func openBrowserGraphResourceClient(ctx context.Context) (graphResourceLease, error) {
	connection, err := grpctunnel.DialContext(ctx, sessionclient.BridgePath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return graphResourceLease{}, err
	}
	return graphResourceLease{client: codefluxv1.NewGraphServiceClient(connection), close: connection.Close}, nil
}

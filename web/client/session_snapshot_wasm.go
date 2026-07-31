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

type sessionProjectionSnapshotLease struct {
	client sessionProjectionSnapshotClient
	close  func() error
}

func openBrowserSessionProjectionSnapshotClient(ctx context.Context) (sessionProjectionSnapshotLease, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return sessionProjectionSnapshotLease{}, err
	}
	return sessionProjectionSnapshotLease{
		client: codefluxv1.NewSessionServiceClient(connection),
		close:  connection.Close,
	}, nil
}

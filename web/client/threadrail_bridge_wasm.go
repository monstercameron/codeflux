//go:build js && wasm

package main

import (
	"context"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func openBrowserThreadRailClient(
	ctx context.Context,
	repositoryID domain.RepositoryID,
	workspaceID domain.WorkspaceID,
) (threadRailClientLease, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return threadRailClientLease{}, err
	}
	client, err := threadrail.NewGRPCClient(
		repositoryID,
		workspaceID,
		codefluxv1.NewThreadServiceClient(connection),
	)
	if err != nil {
		_ = connection.Close()
		return threadRailClientLease{}, err
	}
	return threadRailClientLease{client: client, close: connection.Close}, nil
}

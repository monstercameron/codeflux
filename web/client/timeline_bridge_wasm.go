//go:build js && wasm

package main

import (
	"context"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/timeline"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func openBrowserTimelinePageClient(ctx context.Context, threadID domain.ThreadID) (timelinePageLease, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return timelinePageLease{}, err
	}
	client, err := timeline.NewPageClient(
		threadID,
		codefluxv1.NewThreadServiceClient(connection),
		timeline.DefaultPageLimit,
	)
	if err != nil {
		_ = connection.Close()
		return timelinePageLease{}, err
	}
	return timelinePageLease{client: client, close: connection.Close}, nil
}

//go:build js && wasm

package main

import (
	"context"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func sendComposerCommand(
	ctx context.Context,
	command composerSendCommand,
) (domain.MessageID, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return domain.MessageID{}, err
	}
	defer connection.Close()
	return (generatedComposerTransport{
		client: codefluxv1.NewThreadServiceClient(connection),
	}).Send(ctx, command)
}

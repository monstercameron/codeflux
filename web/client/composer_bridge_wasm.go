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
	messageID, err := (generatedComposerTransport{
		client: codefluxv1.NewThreadServiceClient(connection),
	}).Send(ctx, command)
	if err != nil {
		return domain.MessageID{}, err
	}
	// Recording the request is the durable part and it has already happened.
	// Starting a task from it is a second thing that can fail on its own, and
	// when it does the message must still stand: a person who typed a request
	// and saw it vanish because a task could not start has lost their work for
	// a reason that has nothing to do with what they wrote.
	if _, startErr := startRequestedTask(
		ctx, codefluxv1.NewTaskServiceClient(connection), command, messageID,
	); startErr != nil {
		return messageID, taskStartError{cause: startErr}
	}
	return messageID, nil
}

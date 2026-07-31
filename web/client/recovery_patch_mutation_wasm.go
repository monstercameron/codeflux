//go:build js && wasm

package main

import (
	"context"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func decorateMountedRecoveryPatch(
	props *taskcontrols.Props,
	scope taskResourceScope,
	state ui.State[mountedRecoveryPatchState],
	ref ui.Ref[mountedRecoveryPatchState],
	reload func(),
) {
	currentState := state.Get()
	if currentState.TaskID != "" && currentState.TaskID != scope.taskID.String() {
		currentState = mountedRecoveryPatchState{}
		ref.Set(currentState)
		state.Set(currentState)
	}
	bindMountedRecoveryPatchCallback(props, scope, currentState, func() {
		current, started := prepareMountedRecoveryPatch(
			ref.Get(), props.TaskRevision, composer.NewIdempotencyKey,
		)
		if !started {
			return
		}
		current.TaskID = scope.taskID.String()
		ref.Set(current)
		state.Set(current)
		ui.SafeGo("preserve authoritative recovery patch", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			response, err := executeMountedRecoveryPatch(ctx, scope, current.Revision, string(current.Key))
			ui.PostAsync(func() {
				next := settleMountedRecoveryPatch(current, scope, response, err)
				ref.Set(next)
				state.Set(next)
				if reload != nil {
					reload()
				}
			})
		})
	})
}

func executeMountedRecoveryPatch(
	ctx context.Context,
	scope taskResourceScope,
	revision uint64,
	key string,
) (*codefluxv1.PreserveRecoveryPatchResponse, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	return executeMountedRecoveryPatchWithClient(
		ctx,
		recoveryPatchClient(codefluxv1.NewTaskServiceClient(connection)),
		scope,
		revision,
		key,
	)
}

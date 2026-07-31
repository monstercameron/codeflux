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

func decorateMountedTaskMutations(
	props *taskcontrols.Props,
	scope taskResourceScope,
	state ui.State[mountedTaskMutationState],
	ref ui.Ref[mountedTaskMutationState],
	reload func(),
) {
	current := state.Get()
	bindMountedTaskMutationCallbacks(props, scope, current, func(kind mountedTaskMutationKind) {
		beginMountedTaskMutation(kind, scope, props.TaskRevision, state, ref, reload)
	})
}

func beginMountedTaskMutation(
	kind mountedTaskMutationKind,
	scope taskResourceScope,
	revision uint64,
	state ui.State[mountedTaskMutationState],
	ref ui.Ref[mountedTaskMutationState],
	reload func(),
) {
	current := ref.Get()
	current, started := prepareMountedTaskMutation(current, kind, revision, composer.NewIdempotencyKey)
	if !started {
		return
	}
	ref.Set(current)
	state.Set(current)
	ui.SafeGo("execute authoritative task command", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		view, err := executeMountedTaskMutation(ctx, kind, scope, current.Revision, string(current.Key))
		ui.PostAsync(func() {
			applyMountedTaskMutationSettlement(current, scope, view, err, func(next mountedTaskMutationState) {
				ref.Set(next)
				state.Set(next)
			}, reload)
		})
	})
}

func executeMountedTaskMutation(
	ctx context.Context,
	kind mountedTaskMutationKind,
	scope taskResourceScope,
	revision uint64,
	key string,
) (*codefluxv1.TaskView, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	client := taskMutationClient(codefluxv1.NewTaskServiceClient(connection))
	return executeMountedTaskMutationWithClient(ctx, client, kind, scope, revision, key)
}

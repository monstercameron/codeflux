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

func decorateMountedBudgetMutation(
	props *taskcontrols.Props,
	scope taskResourceScope,
	state ui.State[mountedBudgetMutationState],
	ref ui.Ref[mountedBudgetMutationState],
	reload func(),
) {
	if props == nil {
		return
	}
	current := state.Get()
	props.CommandNotice = joinedTaskCommandNotice(props.CommandNotice, current.Notice)
	if current.Editing || current.Confirming || current.Key != "" {
		props.BudgetAdjust = taskcontrols.BudgetAdjustment{
			Editing: current.Editing, Open: current.Confirming,
			DraftMinorUnits: current.DraftMinorUnits, InvalidMessage: current.InvalidMessage,
			OldLimit: current.OldLimit, NewLimit: current.NewLimit,
			Command: taskcontrols.CommandState{
				Busy: current.Busy, IdempotencyKey: string(current.Key),
			},
		}
	}
	set := func(next mountedBudgetMutationState) {
		ref.Set(next)
		state.Set(next)
	}
	props.OnBudgetAdjust = func() {
		if next, ok := openMountedBudgetEditor(*props); ok {
			set(next)
		}
	}
	props.OnBudgetDraftChange = func(value string) { set(updateMountedBudgetDraft(ref.Get(), value)) }
	props.OnBudgetPreview = func() { set(previewMountedBudgetMutation(ref.Get())) }
	props.OnBudgetCancel = func() {
		current := ref.Get()
		if current.Busy || current.Key != "" {
			return
		}
		set(mountedBudgetMutationState{})
	}
	props.OnBudgetConfirm = func() {
		current, started := prepareMountedBudgetMutation(ref.Get(), props.BudgetRevision, composer.NewIdempotencyKey)
		if !started {
			return
		}
		set(current)
		ui.SafeGo("execute authoritative budget command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			view, err := executeMountedBudgetMutation(ctx, scope, current)
			ui.PostAsync(func() {
				set(settleMountedBudgetMutation(current, scope, view, err))
				if reload != nil {
					reload()
				}
			})
		})
	}
}

func joinedTaskCommandNotice(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + " " + right
}

func executeMountedBudgetMutation(
	ctx context.Context,
	scope taskResourceScope,
	current mountedBudgetMutationState,
) (*codefluxv1.BudgetView, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	return executeMountedBudgetMutationWithClient(ctx, codefluxv1.NewTaskServiceClient(connection), scope, current)
}

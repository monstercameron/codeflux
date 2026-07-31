//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const mountedTaskResourceTimeout = 5 * time.Second

type mountedTaskControlSource struct {
	State            fetch.ResourceState[taskcontrols.Props]
	Reload           func()
	DecorateRecovery func(*taskcontrols.Props)
}

func useSelectedTaskControls(
	thread threadrail.Thread,
	session frontendstate.SessionView,
	projection taskprojection.TaskProjection,
	projectionReady bool,
) mountedTaskControlSource {
	mutationState := ui.UseState(mountedTaskMutationState{})
	mutationRef := ui.UseRef(mountedTaskMutationState{})
	budgetMutationState := ui.UseState(mountedBudgetMutationState{})
	budgetMutationRef := ui.UseRef(mountedBudgetMutationState{})
	recoveryPatchState := ui.UseState(mountedRecoveryPatchState{})
	recoveryPatchRef := ui.UseRef(mountedRecoveryPatchState{})
	recoveryReconcileState := ui.UseState(mountedRecoveryReconcileState{})
	recoveryReconcileRef := ui.UseRef(mountedRecoveryReconcileState{})
	recoverySafeResumeState := ui.UseState(mountedRecoverySafeResumeState{})
	recoverySafeResumeRef := ui.UseRef(mountedRecoverySafeResumeState{})
	stopConfirmationOpen := ui.UseState(false)
	scope, scopeErr := selectedTaskResourceScope(thread)
	dependency := "unavailable|" + string(session.Connection)
	if scopeErr == nil && projectionReady {
		dependency = scope.taskID.String() + "|" + scope.threadID.String() + "|" +
			strconv.FormatUint(thread.Revision(), 10) + "|" + string(session.Connection) + "|" +
			strconv.FormatUint(projection.Revision, 10) + "|" +
			strconv.FormatUint(projection.Budget.Revision, 10) + "|" +
			strconv.FormatUint(projection.LastSequence, 10)
	}
	resource := fetch.UseResource(func(parent context.Context) (taskcontrols.Props, error) {
		if scopeErr != nil {
			return taskcontrols.Props{}, scopeErr
		}
		if !projectionReady {
			return taskcontrols.Props{}, errTaskResourceSelectionUnavailable
		}
		ctx, cancel := context.WithTimeout(parent, mountedTaskResourceTimeout)
		defer cancel()
		return loadTaskControlProps(ctx, openBrowserTaskViewClient, scope, session, projection)
	}, dependency)
	state := resource.Get()
	if state.Ready {
		props := state.Value
		decorateMountedTaskMutations(&props, scope, mutationState, mutationRef, resource.Reload)
		decorateMountedStopConfirmation(&props, stopConfirmationOpen)
		decorateMountedBudgetMutation(&props, scope, budgetMutationState, budgetMutationRef, resource.Reload)
		state.Value = props
	}
	return mountedTaskControlSource{
		State:  state,
		Reload: resource.Reload,
		DecorateRecovery: func(props *taskcontrols.Props) {
			decorateMountedRecoverySafeResume(
				props, scope, recoverySafeResumeState, recoverySafeResumeRef, resource.Reload,
			)
			decorateMountedRecoveryPatch(
				props, scope, recoveryPatchState, recoveryPatchRef, resource.Reload,
			)
			decorateMountedRecoveryReconcile(
				props, scope, recoveryReconcileState, recoveryReconcileRef, resource.Reload,
			)
		},
	}
}

func decorateMountedStopConfirmation(props *taskcontrols.Props, open ui.State[bool]) {
	if props == nil || props.OnStop == nil || !taskStopRequiresConfirmation(props.TaskState) {
		return
	}
	originalStop := props.OnStop
	props.StopConfirm = taskcontrols.StopConfirmation{
		Required: true,
		Open:     open.Get(),
		Consequence: "An already-started provider or validation action may still settle, " +
			"while no new work will start; the current patch remains preserved.",
	}
	props.OnStopConfirm = func() { open.Set(true) }
	props.OnStopCancel = func() { open.Set(false) }
	props.OnStop = func() {
		open.Set(false)
		originalStop()
	}
}

func openBrowserTaskViewClient(ctx context.Context) (taskViewLease, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return taskViewLease{}, err
	}
	return taskViewLease{
		client: codefluxv1.NewTaskServiceClient(connection),
		close:  connection.Close,
	}, nil
}

package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mountedBudgetMutationState struct {
	Editing          bool
	Confirming       bool
	DraftMinorUnits  string
	InvalidMessage   string
	OldLimit         domain.Money
	NewLimit         domain.Money
	Key              composer.IdempotencyKey
	ExpectedRevision uint64
	Busy             bool
	Notice           string
}

type taskBudgetMutationClient interface {
	SetBudget(context.Context, *codefluxv1.SetBudgetRequest, ...grpc.CallOption) (*codefluxv1.SetBudgetResponse, error)
}

func openMountedBudgetEditor(props taskcontrols.Props) (mountedBudgetMutationState, bool) {
	if !props.Budget.HardLimitKnown || !props.BudgetRevisionKnown ||
		props.Delivery.State != taskcontrols.DeliveryLive || !props.Delivery.SequenceCertain {
		return mountedBudgetMutationState{}, false
	}
	return mountedBudgetMutationState{
		Editing: true, DraftMinorUnits: strconv.FormatInt(props.Budget.HardLimit.MinorUnits, 10),
		OldLimit: props.Budget.HardLimit,
	}, true
}

func updateMountedBudgetDraft(current mountedBudgetMutationState, value string) mountedBudgetMutationState {
	if current.Busy || current.Key != "" {
		return current
	}
	current.DraftMinorUnits = value
	current.InvalidMessage = ""
	current.Confirming = false
	return current
}

func previewMountedBudgetMutation(current mountedBudgetMutationState) mountedBudgetMutationState {
	if !current.Editing || current.Busy || current.Key != "" {
		return current
	}
	minorUnits, err := strconv.ParseInt(strings.TrimSpace(current.DraftMinorUnits), 10, 64)
	if err != nil || minorUnits < 0 {
		current.InvalidMessage = "Enter a non-negative whole number of exact minor units."
		return current
	}
	if minorUnits == current.OldLimit.MinorUnits {
		current.InvalidMessage = "Enter a limit different from the current hard budget."
		return current
	}
	current.NewLimit = domain.Money{Currency: current.OldLimit.Currency, MinorUnits: minorUnits}
	current.InvalidMessage = ""
	current.Confirming = true
	return current
}

func prepareMountedBudgetMutation(
	current mountedBudgetMutationState,
	revision uint64,
	newKey taskMutationKeyFactory,
) (mountedBudgetMutationState, bool) {
	if !current.Confirming || current.Busy {
		return current, false
	}
	if current.Key == "" {
		key, err := newKey()
		if err != nil || key == "" {
			return current, false
		}
		current.Key = key
		current.ExpectedRevision = revision
	}
	current.Busy = true
	current.Notice = ""
	return current, true
}

func settleMountedBudgetMutation(
	current mountedBudgetMutationState,
	scope taskResourceScope,
	view *codefluxv1.BudgetView,
	err error,
) mountedBudgetMutationState {
	if err == nil && validCommittedBudgetMutation(view, scope, current) {
		return mountedBudgetMutationState{Notice: "The exact hard budget change was committed by the coordinator."}
	}
	switch status.Code(err) {
	case codes.Aborted:
		return mountedBudgetMutationState{Notice: "The budget changed before confirmation. Authoritative state was refreshed; review the new value before trying again."}
	case codes.PermissionDenied, codes.NotFound:
		return mountedBudgetMutationState{Notice: "The coordinator denied this budget change because its exact approval is unavailable."}
	case codes.InvalidArgument, codes.FailedPrecondition:
		return mountedBudgetMutationState{Notice: "The coordinator rejected this exact budget change in the current task state."}
	}
	current.Busy = false
	current.Notice = "The coordinator did not confirm this budget change. Its exact amount, revision, and request identity are retained for a safe retry."
	return current
}

func validCommittedBudgetMutation(
	view *codefluxv1.BudgetView,
	scope taskResourceScope,
	command mountedBudgetMutationState,
) bool {
	if view == nil || view.GetRevision() <= command.ExpectedRevision ||
		view.GetHardLimit() == nil || view.GetHardLimit().GetDecimalPlaces() != 0 {
		return false
	}
	taskID, err := decodeTaskIdentity(view.GetTaskId())
	if err != nil || taskID != scope.taskID {
		return false
	}
	return view.GetHardLimit().GetCurrencyCode() == string(command.NewLimit.Currency) &&
		view.GetHardLimit().GetMinorUnits() == command.NewLimit.MinorUnits
}

func executeMountedBudgetMutationWithClient(
	ctx context.Context,
	client taskBudgetMutationClient,
	scope taskResourceScope,
	current mountedBudgetMutationState,
) (*codefluxv1.BudgetView, error) {
	if client == nil || scope.taskID.IsZero() || current.Key == "" {
		return nil, errors.New("invalid budget mutation request")
	}
	response, err := client.SetBudget(ctx, &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: string(current.Key), ExpectedRevision: &current.ExpectedRevision,
		},
		TaskId: taskIdentity(scope.taskID),
		HardLimit: &codefluxv1.Money{
			CurrencyCode: string(current.NewLimit.Currency), MinorUnits: current.NewLimit.MinorUnits,
			DecimalPlaces: 0,
		},
	})
	if err != nil || response == nil {
		return nil, err
	}
	return response.GetBudget(), nil
}

package main

import (
	"context"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMountedBudgetMutationOwnsExactConfirmationAndCanonicalRetry(t *testing.T) {
	props := taskcontrols.Props{
		BudgetRevision: 4, BudgetRevisionKnown: true,
		Delivery: taskcontrols.DeliveryView{State: taskcontrols.DeliveryLive, SequenceCertain: true},
		Budget: taskcontrols.BudgetView{
			HardLimitKnown: true,
			HardLimit:      domain.Money{Currency: domain.CurrencyCode("USD"), MinorUnits: 1000},
		},
	}
	current, ok := openMountedBudgetEditor(props)
	if !ok || !current.Editing || current.DraftMinorUnits != "1000" {
		t.Fatalf("editor = %+v, open=%t", current, ok)
	}
	current = updateMountedBudgetDraft(current, "1500")
	current = previewMountedBudgetMutation(current)
	if !current.Confirming || current.NewLimit.MinorUnits != 1500 || current.NewLimit.Currency != "USD" {
		t.Fatalf("confirmation = %+v", current)
	}
	created := 0
	newKey := func() (composer.IdempotencyKey, error) {
		created++
		return "budget-command-key", nil
	}
	current, started := prepareMountedBudgetMutation(current, props.BudgetRevision, newKey)
	if !started || !current.Busy || current.ExpectedRevision != 4 || current.Key != "budget-command-key" {
		t.Fatalf("prepared = %+v, started=%t", current, started)
	}
	if _, duplicate := prepareMountedBudgetMutation(current, 9, newKey); duplicate || created != 1 {
		t.Fatal("a repeated confirmation produced a duplicate command")
	}
	uncertain := settleMountedBudgetMutation(current, taskResourceFixtureScope(t), nil, context.DeadlineExceeded)
	if uncertain.Busy || uncertain.Key != current.Key || uncertain.ExpectedRevision != 4 ||
		!strings.Contains(uncertain.Notice, "retained") {
		t.Fatalf("uncertain settlement = %+v", uncertain)
	}
	retry, started := prepareMountedBudgetMutation(uncertain, 99, newKey)
	if !started || retry.Key != current.Key || retry.ExpectedRevision != 4 || created != 1 {
		t.Fatalf("retry = %+v, started=%t created=%d", retry, started, created)
	}
}

func TestMountedBudgetMutationRejectsInvalidDraftAndRefreshesStale(t *testing.T) {
	current := mountedBudgetMutationState{
		Editing: true, DraftMinorUnits: "1.5",
		OldLimit: domain.Money{Currency: "USD", MinorUnits: 1000},
	}
	current = previewMountedBudgetMutation(current)
	if current.Confirming || !strings.Contains(current.InvalidMessage, "whole number") {
		t.Fatalf("fractional draft = %+v", current)
	}
	current.DraftMinorUnits = "1000"
	current = previewMountedBudgetMutation(current)
	if current.Confirming || !strings.Contains(current.InvalidMessage, "different") {
		t.Fatalf("no-op draft = %+v", current)
	}
	current = mountedBudgetMutationState{
		Editing: true, Confirming: true, Busy: true, Key: "budget-command-key",
		ExpectedRevision: 4, OldLimit: domain.Money{Currency: "USD", MinorUnits: 1000},
		NewLimit: domain.Money{Currency: "USD", MinorUnits: 1500},
	}
	stale := settleMountedBudgetMutation(current, taskResourceFixtureScope(t), nil, status.Error(codes.Aborted, "stale"))
	if stale.Key != "" || stale.Editing || !strings.Contains(stale.Notice, "refreshed") {
		t.Fatalf("stale settlement = %+v", stale)
	}
}

func TestMountedBudgetMutationAcceptsAuthoritativeInitialRevisionZero(t *testing.T) {
	current := mountedBudgetMutationState{
		Editing: true, Confirming: true,
		OldLimit: domain.Money{Currency: "USD", MinorUnits: 1000},
		NewLimit: domain.Money{Currency: "USD", MinorUnits: 1500},
	}
	prepared, started := prepareMountedBudgetMutation(current, 0, func() (composer.IdempotencyKey, error) {
		return "budget-initial-revision", nil
	})
	if !started || prepared.ExpectedRevision != 0 || !prepared.Busy {
		t.Fatalf("initial budget revision was rejected: %+v started=%t", prepared, started)
	}
	view := &codefluxv1.BudgetView{
		TaskId: taskIdentity(taskResourceFixtureScope(t).taskID), Revision: 1,
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 1500},
	}
	if !validCommittedBudgetMutation(view, taskResourceFixtureScope(t), prepared) {
		t.Fatal("initial revision-zero budget commit was not accepted")
	}
}

func TestExecuteMountedBudgetMutationMapsExactWireRequest(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	client := &fakeTaskBudgetMutationClient{}
	current := mountedBudgetMutationState{
		Key: "budget-command-key", ExpectedRevision: 7,
		NewLimit: domain.Money{Currency: "USD", MinorUnits: 2500},
	}
	client.response = &codefluxv1.SetBudgetResponse{Budget: &codefluxv1.BudgetView{
		TaskId: taskIdentity(scope.taskID), Revision: 8,
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 2500},
	}}
	view, err := executeMountedBudgetMutationWithClient(context.Background(), client, scope, current)
	if err != nil || !validCommittedBudgetMutation(view, scope, current) {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	request := client.request
	if request.GetControl().GetIdempotencyKey() != "budget-command-key" ||
		request.GetControl().GetExpectedRevision() != 7 ||
		request.GetHardLimit().GetMinorUnits() != 2500 ||
		request.GetHardLimit().GetCurrencyCode() != "USD" ||
		request.GetHardLimit().GetDecimalPlaces() != 0 {
		t.Fatalf("request = %+v", request)
	}
}

type fakeTaskBudgetMutationClient struct {
	request  *codefluxv1.SetBudgetRequest
	response *codefluxv1.SetBudgetResponse
	err      error
}

func (client *fakeTaskBudgetMutationClient) SetBudget(
	_ context.Context,
	request *codefluxv1.SetBudgetRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.SetBudgetResponse, error) {
	client.request = request
	return client.response, client.err
}

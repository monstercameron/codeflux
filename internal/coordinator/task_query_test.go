package coordinator

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

func TestTaskQueryServiceProjectsExactKnownBudget(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	threadID, _ := domain.NewThreadID()
	sessionID, _ := domain.NewSessionID()
	budgetID, _ := domain.NewBudgetID()
	usd, _ := domain.ParseCurrencyCode("USD")
	created := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	observed := created.Add(9 * time.Minute)
	settling := false
	checkpointID, _ := domain.NewCheckpointID()
	store := &taskQueryStoreStub{snapshot: storage.TaskServiceSnapshot{
		Task: storage.Task{
			ID: taskID, ThreadID: threadID, State: domain.TaskStateRunning,
			Revision: 7, CreatedAt: created, UpdatedAt: created.Add(4 * time.Minute),
		},
		SessionID: sessionID, PlanRevision: 3, SummaryRedacted: "safe summary",
		SummaryOriginalBytes: 12, ObservedAt: observed,
		Budget: &storage.BudgetSnapshot{
			BudgetID: budgetID, TaskID: taskID, Revision: 5,
			WarningCost:     storage.ExactMinorCost{Currency: usd, Numerator: 10_000, Denominator: 1},
			HardCost:        storage.ExactMinorCost{Currency: usd, Numerator: 12_500, Denominator: 1},
			ActualKnownCost: storage.ExactMinorCost{Currency: usd, Numerator: 325, Denominator: 1},
			ActualTokens:    987,
			RemainingCost: func() *storage.ExactMinorCost {
				value := storage.ExactMinorCost{Currency: usd, Numerator: 12_175, Denominator: 1}
				return &value
			}(),
		},
		ActualPricingSnapshotIDs: []string{"prices-2026-07-31"},
		SettlingProviderRequest:  &settling,
		LatestCheckpoint: &storage.TaskServiceCheckpoint{
			ID: checkpointID, State: domain.CheckpointStateReady, PlanStep: "step-2",
		},
	}}
	service, err := NewTaskQueryService(store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.GetTaskQuery(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if view.TaskID != taskID || view.ThreadID != threadID || view.SessionID != sessionID ||
		view.PlanRevision != 3 || view.BudgetRevision != 5 || view.Elapsed != 9*time.Minute ||
		view.HardBudget == nil || view.HardBudget.MinorUnits != 12_500 ||
		view.ActualCost == nil || view.ActualCost.MinorUnits != 325 ||
		view.ActualTokens == nil || *view.ActualTokens != 987 ||
		view.SettlingProviderRequest == nil || *view.SettlingProviderRequest ||
		view.LatestCheckpointID == nil || *view.LatestCheckpointID != checkpointID ||
		view.LatestCheckpointState != domain.CheckpointStateReady || view.LatestCheckpointPlanStep != "step-2" {
		t.Fatalf("task query view = %#v", view)
	}
}

func TestTaskQueryServiceNeverRoundsOrInventsUnknownMoney(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	threadID, _ := domain.NewThreadID()
	usd, _ := domain.ParseCurrencyCode("USD")
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	service, err := NewTaskQueryService(&taskQueryStoreStub{snapshot: storage.TaskServiceSnapshot{
		Task: storage.Task{
			ID: taskID, ThreadID: threadID, State: domain.TaskStateCompleted,
			Revision: 2, CreatedAt: now, UpdatedAt: now.Add(time.Minute),
		},
		ObservedAt: now.Add(time.Hour),
		Budget: &storage.BudgetSnapshot{
			WarningCost:           storage.ExactMinorCost{Currency: usd, Numerator: 10_000, Denominator: 1},
			HardCost:              storage.ExactMinorCost{Currency: usd, Numerator: 25_001, Denominator: 2},
			ActualKnownCost:       storage.ExactMinorCost{Currency: usd, Numerator: 0, Denominator: 1},
			CostAccountingUnknown: true, TokenAccountingUnknown: true,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.GetTaskQuery(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if view.HardBudget != nil || view.ActualCost != nil || view.ActualTokens != nil {
		t.Fatalf("inexact or unknown values were invented: %#v", view)
	}
	if view.Elapsed != time.Minute {
		t.Fatalf("terminal elapsed = %s, want 1m", view.Elapsed)
	}
}

func TestTaskQueryServiceMapsStorageNotFound(t *testing.T) {
	service, err := NewTaskQueryService(&taskQueryStoreStub{err: storage.ErrNotFound})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.GetTaskQuery(t.Context(), domain.TaskID{})
	if !errors.Is(err, transport.ErrTaskQueryNotFound) {
		t.Fatalf("query error = %v", err)
	}
}

type taskQueryStoreStub struct {
	snapshot storage.TaskServiceSnapshot
	err      error
}

func (stub *taskQueryStoreStub) ReadTaskServiceSnapshot(
	_ context.Context,
	_ domain.TaskID,
) (storage.TaskServiceSnapshot, error) {
	return stub.snapshot, stub.err
}

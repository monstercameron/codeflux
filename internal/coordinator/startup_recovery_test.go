package coordinator

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/storage"
)

func TestRecoverUnownedTaskRunsDelegatesToStorage(t *testing.T) {
	t.Parallel()

	want := []storage.UnownedTaskRunRecoveryCandidate{{
		Reason: storage.TaskRunRecoveryMissingOwnership,
	}}
	store := &taskRunRecoveryTestStore{candidates: want}
	got, err := RecoverUnownedTaskRuns(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Reason != want[0].Reason || store.calls != 1 {
		t.Fatalf("recovery result = %#v, calls = %d", got, store.calls)
	}
}

func TestRecoverUnownedTaskRunsValidatesStoreAndCancellation(t *testing.T) {
	t.Parallel()

	if _, err := RecoverUnownedTaskRuns(t.Context(), nil); err == nil {
		t.Fatal("nil task run recovery store was accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := &taskRunRecoveryTestStore{}
	if _, err := RecoverUnownedTaskRuns(ctx, store); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery error = %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("cancelled recovery called storage %d times", store.calls)
	}
}

type taskRunRecoveryTestStore struct {
	candidates []storage.UnownedTaskRunRecoveryCandidate
	err        error
	calls      int
}

func (store *taskRunRecoveryTestStore) RecoverUnownedTaskRuns(
	context.Context,
) ([]storage.UnownedTaskRunRecoveryCandidate, error) {
	store.calls++
	return append(
		[]storage.UnownedTaskRunRecoveryCandidate(nil),
		store.candidates...,
	), store.err
}

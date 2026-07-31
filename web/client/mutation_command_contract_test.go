package main

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mutationCommandContractObservation struct {
	firstStarted           bool
	doubleClickBlocked     bool
	oneKeyAllocated        bool
	uncertainIdentityOwned bool
	uncertainRetryStarted  bool
	staleRetryBlocked      bool
	staleExplained         bool
	deniedRetryBlocked     bool
	deniedExplained        bool
}

func TestProductionUIMutationsOwnOneCommandIdentityThroughSettlement(t *testing.T) {
	tests := []struct {
		name  string
		probe func(*testing.T) mutationCommandContractObservation
	}{
		{name: "pause", probe: func(t *testing.T) mutationCommandContractObservation {
			return probeMountedTaskMutationContract(t, mountedTaskPause)
		}},
		{name: "resume", probe: func(t *testing.T) mutationCommandContractObservation {
			return probeMountedTaskMutationContract(t, mountedTaskResume)
		}},
		{name: "stop", probe: func(t *testing.T) mutationCommandContractObservation {
			return probeMountedTaskMutationContract(t, mountedTaskStop)
		}},
		{name: "budget", probe: probeMountedBudgetMutationContract},
		{name: "preserve-patch", probe: probeMountedRecoveryPatchContract},
		{name: "reconcile", probe: probeMountedRecoveryReconcileContract},
		{name: "safe-resume", probe: probeMountedRecoverySafeResumeContract},
		{name: "composer-send", probe: probeComposerSendContract},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.probe(t)
			if !got.firstStarted {
				t.Fatal("first invocation did not start")
			}
			if !got.doubleClickBlocked {
				t.Fatal("second invocation was not blocked while busy")
			}
			if !got.oneKeyAllocated {
				t.Fatal("command allocated more than one idempotency key")
			}
			if !got.uncertainIdentityOwned || !got.uncertainRetryStarted {
				t.Fatalf("uncertain settlement lost its canonical retry identity: %+v", got)
			}
			if !got.staleRetryBlocked || !got.staleExplained {
				t.Fatalf("stale settlement remained blindly retryable or unexplained: %+v", got)
			}
			if !got.deniedRetryBlocked || !got.deniedExplained {
				t.Fatalf("denied settlement remained blindly retryable or unexplained: %+v", got)
			}
		})
	}
}

func probeMountedTaskMutationContract(
	t *testing.T,
	kind mountedTaskMutationKind,
) mutationCommandContractObservation {
	t.Helper()
	const revision uint64 = 41
	allocated := 0
	newKey := func() (composer.IdempotencyKey, error) {
		allocated++
		return composer.IdempotencyKey("task-mutation-contract-key"), nil
	}
	first, firstStarted := prepareMountedTaskMutation(mountedTaskMutationState{}, kind, revision, newKey)
	_, duplicateStarted := prepareMountedTaskMutation(first, kind, revision, newKey)
	uncertain := settleMountedTaskMutation(first, taskResourceFixtureScope(t), nil, status.Error(codes.Unavailable, "delivery unknown"))
	retry, retryStarted := prepareMountedTaskMutation(uncertain, kind, revision+1, newKey)
	stale := settleMountedTaskMutation(first, taskResourceFixtureScope(t), nil, status.Error(codes.Aborted, "revision changed"))
	denied := settleMountedTaskMutation(first, taskResourceFixtureScope(t), nil, status.Error(codes.PermissionDenied, "denied"))
	return mutationCommandContractObservation{
		firstStarted:           firstStarted,
		doubleClickBlocked:     !duplicateStarted,
		oneKeyAllocated:        allocated == 1,
		uncertainIdentityOwned: uncertain.Key == first.Key && uncertain.Kind == kind && uncertain.Revision == revision,
		uncertainRetryStarted:  retryStarted && retry.Key == first.Key && retry.Revision == revision,
		staleRetryBlocked:      stale.Key == "" && stale.Kind == "",
		staleExplained:         strings.Contains(stale.Notice, "refreshed"),
		deniedRetryBlocked:     denied.Key == "" && denied.Kind == "",
		deniedExplained:        strings.Contains(denied.Notice, "denied") && strings.Contains(denied.Notice, "refreshed"),
	}
}

func probeMountedBudgetMutationContract(t *testing.T) mutationCommandContractObservation {
	t.Helper()
	const revision uint64 = 42
	allocated := 0
	newKey := func() (composer.IdempotencyKey, error) {
		allocated++
		return composer.IdempotencyKey("budget-mutation-contract-key"), nil
	}
	base := mountedBudgetMutationState{
		Editing: true, Confirming: true,
		OldLimit: domain.Money{Currency: "USD", MinorUnits: 1000},
		NewLimit: domain.Money{Currency: "USD", MinorUnits: 1500},
	}
	first, firstStarted := prepareMountedBudgetMutation(base, revision, newKey)
	_, duplicateStarted := prepareMountedBudgetMutation(first, revision, newKey)
	uncertain := settleMountedBudgetMutation(first, taskResourceFixtureScope(t), nil, status.Error(codes.Unavailable, "delivery unknown"))
	retry, retryStarted := prepareMountedBudgetMutation(uncertain, revision+1, newKey)
	stale := settleMountedBudgetMutation(first, taskResourceFixtureScope(t), nil, status.Error(codes.Aborted, "revision changed"))
	denied := settleMountedBudgetMutation(first, taskResourceFixtureScope(t), nil, status.Error(codes.PermissionDenied, "denied"))
	return mutationCommandContractObservation{
		firstStarted:       firstStarted,
		doubleClickBlocked: !duplicateStarted,
		oneKeyAllocated:    allocated == 1,
		uncertainIdentityOwned: uncertain.Key == first.Key && uncertain.ExpectedRevision == revision &&
			uncertain.NewLimit == first.NewLimit && uncertain.OldLimit == first.OldLimit,
		uncertainRetryStarted: retryStarted && retry.Key == first.Key && retry.ExpectedRevision == revision &&
			retry.NewLimit == first.NewLimit,
		staleRetryBlocked:  stale.Key == "" && !stale.Confirming,
		staleExplained:     strings.Contains(stale.Notice, "refreshed"),
		deniedRetryBlocked: denied.Key == "" && !denied.Confirming,
		deniedExplained:    strings.Contains(denied.Notice, "denied"),
	}
}

func probeMountedRecoveryPatchContract(t *testing.T) mutationCommandContractObservation {
	t.Helper()
	const revision uint64 = 43
	allocated := 0
	newKey := func() (composer.IdempotencyKey, error) {
		allocated++
		return composer.IdempotencyKey("patch-mutation-contract-key"), nil
	}
	first, firstStarted := prepareMountedRecoveryPatch(mountedRecoveryPatchState{}, revision, newKey)
	_, duplicateStarted := prepareMountedRecoveryPatch(first, revision, newKey)
	uncertain := settleMountedRecoveryPatch(first, taskResourceFixtureScope(t), nil, status.Error(codes.Unavailable, "delivery unknown"))
	retry, retryStarted := prepareMountedRecoveryPatch(uncertain, revision+1, newKey)
	stale := settleMountedRecoveryPatch(first, taskResourceFixtureScope(t), nil, status.Error(codes.Aborted, "revision changed"))
	denied := settleMountedRecoveryPatch(first, taskResourceFixtureScope(t), nil, status.Error(codes.PermissionDenied, "denied"))
	return recoveryMutationContractObservation(
		firstStarted, duplicateStarted, allocated, first.Key, revision,
		uncertain.Key, uncertain.Revision, retryStarted, retry.Key, retry.Revision,
		stale.Key, stale.Notice, denied.Key, denied.Notice,
	)
}

func probeMountedRecoveryReconcileContract(t *testing.T) mutationCommandContractObservation {
	t.Helper()
	const revision uint64 = 44
	allocated := 0
	newKey := func() (composer.IdempotencyKey, error) {
		allocated++
		return composer.IdempotencyKey("reconcile-mutation-contract-key"), nil
	}
	first, firstStarted := prepareMountedRecoveryReconcile(mountedRecoveryReconcileState{}, revision, newKey)
	_, duplicateStarted := prepareMountedRecoveryReconcile(first, revision, newKey)
	uncertain := settleMountedRecoveryReconcile(first, taskResourceFixtureScope(t), nil, status.Error(codes.Unavailable, "delivery unknown"))
	retry, retryStarted := prepareMountedRecoveryReconcile(uncertain, revision+1, newKey)
	stale := settleMountedRecoveryReconcile(first, taskResourceFixtureScope(t), nil, status.Error(codes.Aborted, "revision changed"))
	denied := settleMountedRecoveryReconcile(first, taskResourceFixtureScope(t), nil, status.Error(codes.PermissionDenied, "denied"))
	return recoveryMutationContractObservation(
		firstStarted, duplicateStarted, allocated, first.Key, revision,
		uncertain.Key, uncertain.Revision, retryStarted, retry.Key, retry.Revision,
		stale.Key, stale.Notice, denied.Key, denied.Notice,
	)
}

func probeMountedRecoverySafeResumeContract(t *testing.T) mutationCommandContractObservation {
	t.Helper()
	const revision uint64 = 45
	allocated := 0
	newKey := func() (composer.IdempotencyKey, error) {
		allocated++
		return composer.IdempotencyKey("safe-resume-mutation-contract-key"), nil
	}
	first, firstStarted := prepareMountedRecoverySafeResume(mountedRecoverySafeResumeState{}, revision, newKey)
	_, duplicateStarted := prepareMountedRecoverySafeResume(first, revision, newKey)
	uncertain := settleMountedRecoverySafeResume(first, taskResourceFixtureScope(t), nil, status.Error(codes.Unavailable, "delivery unknown"))
	retry, retryStarted := prepareMountedRecoverySafeResume(uncertain, revision+1, newKey)
	stale := settleMountedRecoverySafeResume(first, taskResourceFixtureScope(t), nil, status.Error(codes.Aborted, "revision changed"))
	denied := settleMountedRecoverySafeResume(first, taskResourceFixtureScope(t), nil, status.Error(codes.PermissionDenied, "denied"))
	return recoveryMutationContractObservation(
		firstStarted, duplicateStarted, allocated, first.Key, revision,
		uncertain.Key, uncertain.Revision, retryStarted, retry.Key, retry.Revision,
		stale.Key, stale.Notice, denied.Key, denied.Notice,
	)
}

func recoveryMutationContractObservation(
	firstStarted, duplicateStarted bool,
	allocated int,
	firstKey composer.IdempotencyKey,
	firstRevision uint64,
	uncertainKey composer.IdempotencyKey,
	uncertainRevision uint64,
	retryStarted bool,
	retryKey composer.IdempotencyKey,
	retryRevision uint64,
	staleKey composer.IdempotencyKey,
	staleNotice string,
	deniedKey composer.IdempotencyKey,
	deniedNotice string,
) mutationCommandContractObservation {
	return mutationCommandContractObservation{
		firstStarted:           firstStarted,
		doubleClickBlocked:     !duplicateStarted,
		oneKeyAllocated:        allocated == 1,
		uncertainIdentityOwned: uncertainKey == firstKey && uncertainRevision == firstRevision,
		uncertainRetryStarted:  retryStarted && retryKey == firstKey && retryRevision == firstRevision,
		staleRetryBlocked:      staleKey == "",
		staleExplained:         strings.Contains(staleNotice, "changed") || strings.Contains(staleNotice, "refused"),
		deniedRetryBlocked:     deniedKey == "",
		deniedExplained:        strings.Contains(deniedNotice, "changed") || strings.Contains(deniedNotice, "refused"),
	}
}

func probeComposerSendContract(t *testing.T) mutationCommandContractObservation {
	t.Helper()
	model, command, _ := pendingComposerCommand(t, "retain this request")
	_, duplicateErr := composer.Reduce(model, composer.SendStarted{ThreadID: command.ThreadID, Key: command.Key})

	uncertain, _, err := settleComposerCommand(model, command, domain.MessageID{}, status.Error(codes.Unavailable, "delivery unknown"))
	if err != nil {
		t.Fatal(err)
	}
	uncertainAttempt, uncertainPresent := uncertain.Attempt(command.ThreadID)
	retry, retryErr := composer.Reduce(uncertain, composer.SendRetryRequested{ThreadID: command.ThreadID, Key: command.Key})
	retryAttempt, retryPresent := retry.Attempt(command.ThreadID)

	staleModel, staleCommand, _ := pendingComposerCommand(t, "preserve stale draft")
	stale, _, err := settleComposerCommand(staleModel, staleCommand, domain.MessageID{}, status.Error(codes.Aborted, "revision changed"))
	if err != nil {
		t.Fatal(err)
	}
	staleAttempt, stalePresent := stale.Attempt(staleCommand.ThreadID)
	_, staleRetryErr := composer.Reduce(stale, composer.SendRetryRequested{ThreadID: staleCommand.ThreadID, Key: staleCommand.Key})

	deniedModel, deniedCommand, _ := pendingComposerCommand(t, "preserve denied draft")
	denied, _, err := settleComposerCommand(deniedModel, deniedCommand, domain.MessageID{}, status.Error(codes.PermissionDenied, "denied"))
	if err != nil {
		t.Fatal(err)
	}
	deniedAttempt, deniedPresent := denied.Attempt(deniedCommand.ThreadID)
	_, deniedRetryErr := composer.Reduce(denied, composer.SendRetryRequested{ThreadID: deniedCommand.ThreadID, Key: deniedCommand.Key})

	return mutationCommandContractObservation{
		firstStarted:           true,
		doubleClickBlocked:     errors.Is(duplicateErr, composer.ErrComposerBusy),
		oneKeyAllocated:        uncertainPresent && uncertainAttempt.Key() == command.Key,
		uncertainIdentityOwned: uncertainPresent && uncertainAttempt.Key() == command.Key && uncertainAttempt.Retryable(),
		uncertainRetryStarted: retryErr == nil && retryPresent && retryAttempt.Key() == command.Key &&
			retryAttempt.Status() == composer.SendPending,
		staleRetryBlocked: stalePresent && staleAttempt.Key() == staleCommand.Key && !staleAttempt.Retryable() &&
			errors.Is(staleRetryErr, composer.ErrSendNotRetryable),
		staleExplained: strings.Contains(staleAttempt.SafeMessage(), "changed") &&
			strings.Contains(staleAttempt.SafeMessage(), "refresh"),
		deniedRetryBlocked: deniedPresent && deniedAttempt.Key() == deniedCommand.Key && !deniedAttempt.Retryable() &&
			errors.Is(deniedRetryErr, composer.ErrSendNotRetryable),
		deniedExplained: strings.Contains(deniedAttempt.SafeMessage(), "denied"),
	}
}

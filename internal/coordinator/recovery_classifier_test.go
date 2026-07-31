package coordinator

import (
	"testing"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
)

func TestClassifyCheckpointRecoveryUnchangedWorktreeIsSafe(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoverySafeResume ||
		len(got.Findings) != 0 ||
		len(got.ActionsThatMustNotBeRepeated) != 0 {
		t.Fatalf("unchanged assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoverySurfacesNonOverlappingUserEdit(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.DirtyFiles = append(
		input.Observation.DirtyFiles,
		checkpoint.DirtyFileHash{
			Path: "docs/user.txt", Exists: true, SHA256: recoverySHA('d'),
		},
	)
	input.Observation.DiffSHA256 = recoverySHA('e')
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoverySafeResume ||
		len(got.NonOverlappingUserEditPaths) != 1 ||
		got.NonOverlappingUserEditPaths[0] != "docs/user.txt" ||
		!hasRecoveryFinding(got.Findings, RecoveryFindingNonOverlappingUserEdit) {
		t.Fatalf("non-overlapping assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryRequiresReconciliationForConflict(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.DirtyFiles[0].SHA256 = recoverySHA('f')
	input.Observation.DiffSHA256 = recoverySHA('0')
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoveryReconcileRequired ||
		!hasRecoveryFinding(got.Findings, RecoveryFindingDirtyFileConflict) {
		t.Fatalf("conflicting assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryMissingWorktreeUsesPreservedPatch(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.WorktreeExists = false
	input.Observation.WorktreeOwned = false
	input.Observation.WorktreeBindingRevision = 0
	input.Observation.WorktreeHead = ""
	input.Observation.DirtyFiles = nil
	input.Observation.DiffSHA256 = ""
	input.Observation.PatchAvailable = true
	input.Observation.PatchLocator = "refs/codeflux/checkpoints/fixture"
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoveryPatchPreservationOnly ||
		!got.PatchAvailable ||
		got.PatchLocator != input.Observation.PatchLocator ||
		!hasRecoveryFinding(got.Findings, RecoveryFindingWorktreeMissing) {
		t.Fatalf("missing-worktree assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryMissingPatchIsUnrecoverable(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.RepositoryIdentityMatches = false
	input.Observation.PatchAvailable = false
	input.Observation.PatchLocator = ""
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoveryUnrecoverable ||
		!hasRecoveryFinding(got.Findings, RecoveryFindingRepositoryIdentityChanged) {
		t.Fatalf("unrecoverable assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryRejectsChangedPolicyProviderOrTools(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name     string
		change   func(*RecoveryObservation)
		wantCode RecoveryFindingCode
	}{
		{
			name: "policy",
			change: func(value *RecoveryObservation) {
				value.PolicyCompatible = false
			},
			wantCode: RecoveryFindingPolicyChanged,
		},
		{
			name: "provider",
			change: func(value *RecoveryObservation) {
				value.ProviderCompatible = false
			},
			wantCode: RecoveryFindingProviderChanged,
		},
		{
			name: "tools",
			change: func(value *RecoveryObservation) {
				value.ToolsCompatible = false
			},
			wantCode: RecoveryFindingToolConfigurationChanged,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := recoveryClassificationFixture(t)
			test.change(&input.Observation)
			got, err := ClassifyCheckpointRecovery(input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Classification != RecoveryReconcileRequired ||
				!hasRecoveryFinding(got.Findings, test.wantCode) {
				t.Fatalf("%s assessment = %#v", test.name, got)
			}
		})
	}
}

func TestClassifyCheckpointRecoveryNeverRepeatsAmbiguousAction(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Checkpoint.ExternalOutcomeAmbiguous = true
	input.Checkpoint.AmbiguousExternalActions = []checkpoint.AmbiguousExternalAction{
		{
			ActionID:     "external-action-1",
			Kind:         "provider-request",
			IntentSHA256: recoverySHA('1'),
		},
	}
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoveryReconcileRequired ||
		len(got.ActionsThatMustNotBeRepeated) != 1 ||
		got.ActionsThatMustNotBeRepeated[0] != "external-action-1" ||
		!hasRecoveryFinding(got.Findings, RecoveryFindingAmbiguousExternalAction) {
		t.Fatalf("ambiguous assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryNeverRepeatsDurablyCompletedAction(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.CompletedActionIDs = []string{"tool-request-completed"}
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoverySafeResume ||
		len(got.ActionsThatMustNotBeRepeated) != 1 ||
		got.ActionsThatMustNotBeRepeated[0] != "tool-request-completed" {
		t.Fatalf("completed-action assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryDetectsAmbiguityAfterCheckpoint(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.AmbiguousExternalActions =
		[]checkpoint.AmbiguousExternalAction{{
			ActionID:     "command-outcome-unknown",
			Kind:         "command",
			IntentSHA256: recoverySHA('2'),
		}}
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoveryReconcileRequired ||
		len(got.ActionsThatMustNotBeRepeated) != 1 ||
		got.ActionsThatMustNotBeRepeated[0] != "command-outcome-unknown" {
		t.Fatalf("post-checkpoint ambiguity assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryUnresolvedGitOperationRequiresReconcile(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.GitOperationStates = []string{"merge"}
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoveryReconcileRequired ||
		!hasRecoveryFinding(got.Findings, RecoveryFindingGitOperationUnresolved) {
		t.Fatalf("Git-operation assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryJournalBehindCheckpointIsUnrecoverable(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.DurableEventSequence--
	got, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != RecoveryUnrecoverable ||
		!hasRecoveryFinding(got.Findings, RecoveryFindingEventJournalBehind) {
		t.Fatalf("event-journal assessment = %#v", got)
	}
}

func TestClassifyCheckpointRecoveryRequiresTruthfulPatchLocator(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.PatchLocator = ""
	if _, err := ClassifyCheckpointRecovery(input); err == nil {
		t.Fatal("available patch without locator was accepted")
	}
}

func recoveryClassificationFixture(t *testing.T) RecoveryClassificationInput {
	t.Helper()

	checkpointID, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	dirty := []checkpoint.DirtyFileHash{{
		Path: "internal/example.go", Exists: true, SHA256: recoverySHA('a'),
	}}
	snapshot := checkpoint.Snapshot{
		SchemaVersion:           checkpoint.SchemaVersion,
		TaskID:                  taskID,
		RunID:                   runID,
		RepositoryID:            repositoryID,
		WorktreeBindingRevision: 3,
		PlanRevision:            2,
		BaseRevision:            recoveryGitObject('1'),
		WorktreeHead:            recoveryGitObject('2'),
		PreservedRevision:       recoveryGitObject('3'),
		DirtyFiles:              dirty,
		DiffSHA256:              recoverySHA('b'),
		CompletedPlanSteps: []checkpoint.PlanStepSnapshot{{
			ID: "step-1", State: checkpoint.PlanStepImplemented,
		}},
		PendingPlanSteps: []checkpoint.PlanStepSnapshot{{
			ID: "step-2", State: checkpoint.PlanStepPending,
		}},
		Budget: checkpoint.BudgetPosition{
			BudgetID: budgetID, SnapshotRevision: 4, LimitRevision: 1,
			ReservedCost: checkpoint.ExactCost{
				Currency: "USD", Numerator: 0, Denominator: 1,
			},
			ChargedCost: checkpoint.ExactCost{
				Currency: "USD", Numerator: 0, Denominator: 1,
			},
			ActualKnownCost: checkpoint.ExactCost{
				Currency: "USD", Numerator: 0, Denominator: 1,
			},
		},
		Policy: checkpoint.PolicyBinding{
			Revision: 2, Version: "fixed-v1", ContentSHA256: recoverySHA('c'),
		},
		Provider: checkpoint.ProviderBinding{
			SettingsRevision:       3,
			RunConfigurationSHA256: recoverySHA('e'),
			Adapter:                "openai-responses",
			AdapterVersion:         "v1",
			Provider:               "openai",
			ProviderVersion:        "2026-07-30",
			Model:                  "gpt-5",
			ModelRevision:          "2026-07-30",
		},
		Tools: checkpoint.ToolBinding{
			SchemaVersion: 1, CatalogSHA256: recoverySHA('d'),
		},
		LastDurableEventSequence: 8,
	}
	return RecoveryClassificationInput{
		CheckpointID:            checkpointID,
		CheckpointEventSequence: snapshot.LastDurableEventSequence + 1,
		Checkpoint:              snapshot,
		Observation: RecoveryObservation{
			RepositoryPathMatches:     true,
			RepositoryIdentityMatches: true,
			BaseRevisionAvailable:     true,
			WorktreeExists:            true,
			WorktreeOwned:             true,
			WorktreeBindingRevision:   snapshot.WorktreeBindingRevision,
			WorktreeHead:              snapshot.WorktreeHead,
			DirtyFiles:                append([]checkpoint.DirtyFileHash(nil), dirty...),
			DiffSHA256:                snapshot.DiffSHA256,
			PolicyCompatible:          true,
			ProviderCompatible:        true,
			ToolsCompatible:           true,
			DurableEventSequence:      snapshot.LastDurableEventSequence + 1,
			PatchAvailable:            true,
			PatchLocator:              "refs/codeflux/checkpoints/fixture",
		},
	}
}

func recoverySHA(character byte) string {
	return string(makeRepeatedByte(character, 64))
}

func recoveryGitObject(character byte) string {
	return string(makeRepeatedByte(character, 40))
}

func makeRepeatedByte(character byte, count int) []byte {
	value := make([]byte, count)
	for index := range value {
		value[index] = character
	}
	return value
}

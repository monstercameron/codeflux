package coordinator

import (
	"testing"

	"codeflux.dev/codeflux/internal/checkpoint"
)

func TestResumeAssessmentAllowsNormalPostCheckpointEventsAndNonOverlappingEdits(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.DurableEventSequence += 2
	input.Observation.DirtyFiles = append(
		input.Observation.DirtyFiles,
		checkpoint.DirtyFileHash{
			Path: "docs/user-note.md", Exists: true,
			SHA256: recoverySHA('9'),
		},
	)
	assessment, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	got := resumeAssessmentFromRecovery(input.Observation, assessment)
	if !got.RepositoryCurrent || !got.WorktreeCurrent ||
		!got.PolicyCurrent || !got.ProviderCurrent || !got.ToolsCurrent ||
		got.AmbiguousExternalOutcome ||
		got.PausedEdits != PausedEditsNonOverlapping ||
		len(got.ChangedFiles) != 1 ||
		got.ChangedFiles[0] != "docs/user-note.md" {
		t.Fatalf("compatible resume assessment = %#v", got)
	}
}

func TestResumeAssessmentBlocksConflictAndAmbiguousIntent(t *testing.T) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.DirtyFiles[0].SHA256 = recoverySHA('8')
	input.Observation.AmbiguousExternalActions =
		[]checkpoint.AmbiguousExternalAction{{
			ActionID:      "tool-intent-without-result",
			Kind:          "tool-request",
			IntentSHA256:  recoverySHA('7'),
			ToolRequestID: "tool-intent-without-result",
		}}
	assessment, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	got := resumeAssessmentFromRecovery(input.Observation, assessment)
	if got.WorktreeCurrent ||
		!got.AmbiguousExternalOutcome ||
		got.PausedEdits != PausedEditsConflicting ||
		len(got.ConflictFiles) != 1 ||
		got.ConflictFiles[0] != "internal/example.go" {
		t.Fatalf("blocked resume assessment = %#v", got)
	}
}

func TestResumeAssessmentBlocksCompletedActionObservedAfterCheckpoint(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	input.Observation.CompletedActionIDs =
		[]string{"tool:completed-after-checkpoint"}
	assessment, err := ClassifyCheckpointRecovery(input)
	if err != nil {
		t.Fatal(err)
	}
	got := resumeAssessmentFromRecovery(input.Observation, assessment)
	if !got.AmbiguousExternalOutcome ||
		len(assessment.ActionsThatMustNotBeRepeated) != 1 ||
		assessment.ActionsThatMustNotBeRepeated[0] !=
			"tool:completed-after-checkpoint" {
		t.Fatalf("post-checkpoint replay gate = %#v / %#v", got, assessment)
	}
}

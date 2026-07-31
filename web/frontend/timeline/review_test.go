package timeline

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

const interactionUUID = "01890f3c-4a00-7abc-8def-0123456789ab"

func TestReviewOpenClosePreservesThreadAndGraphPositionExactly(t *testing.T) {
	revision := parseGraphRevisionFixture(t)
	position := WorkspacePosition{
		ThreadAnchor: "message:stable", ThreadOffset: 18.5,
		GraphRevision: revision, GraphSelectedNode: "node:implementation",
		GraphPanX: 22, GraphPanY: -14, GraphZoom: 1.25,
	}
	opened, err := OpenReview(ReviewTransition{}, position)
	if err != nil {
		t.Fatal(err)
	}
	closed, restored, err := CloseReview(opened)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Open || restored != position {
		t.Fatalf("closed=%#v restored=%#v want=%#v", closed, restored, position)
	}
}

func TestGraphAutoHighlightNeverPansAwayFromUserInspection(t *testing.T) {
	current, err := ApplyGraphAutoHighlight(GraphInspection{}, "node:running")
	if err != nil || current.SelectedNode != "node:running" {
		t.Fatalf("initial highlight = %#v err=%v", current, err)
	}
	current, err = BeginGraphInspection(current, "node:evidence")
	if err != nil {
		t.Fatal(err)
	}
	current, err = ApplyGraphAutoHighlight(current, "node:validation")
	if err != nil {
		t.Fatal(err)
	}
	if current.SelectedNode != "node:evidence" || !current.ReturnToCurrentAvailable {
		t.Fatalf("auto highlight stole inspection: %#v", current)
	}
	current, err = ReturnToCurrentGraphNode(current)
	if err != nil || current.SelectedNode != "node:validation" ||
		current.UserInspecting || current.ReturnToCurrentAvailable {
		t.Fatalf("return to current = %#v err=%v", current, err)
	}
}

func TestRepairFeedbackSupportsEveryStableTargetIdentity(t *testing.T) {
	task, err := domain.ParseTaskID("tsk_" + interactionUUID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := domain.ParseArtifactID("art_" + interactionUUID)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := domain.ParseValidationID("val_" + interactionUUID)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []RepairFeedbackTarget{
		{Kind: RepairTargetTask, Task: task},
		{Kind: RepairTargetFile, File: file},
		{Kind: RepairTargetHunk, Hunk: "diff:sha256:abc#hunk-4"},
		{Kind: RepairTargetValidation, Validation: validation},
		{Kind: RepairTargetGraphNode, GraphNode: "node:implementation"},
	}
	for _, fixture := range fixtures {
		if err := fixture.Validate(); err != nil {
			t.Errorf("%s target invalid: %v", fixture.Kind, err)
		}
	}
	invalid := fixtures[0]
	invalid.GraphNode = "node:second"
	if err := invalid.Validate(); err == nil {
		t.Fatal("multi-identity repair target was accepted")
	}
}

func parseGraphRevisionFixture(t *testing.T) domain.GraphRevisionID {
	t.Helper()
	value, err := domain.ParseGraphRevisionID("grv_" + interactionUUID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

package reviewactions

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestRunningValidationDisablesAcceptance(t *testing.T) {
	props := fixtureProps(t)
	props.Review.RequiredChecks = []acceptance.RequiredCheck{{ID: "unit", Status: acceptance.CheckPassed}}
	props.LiveRequiredChecks = []acceptance.RequiredCheck{{ID: "unit", Status: acceptance.CheckRunning}}
	markup := render(t, props)
	for _, want := range []string{"Required validations are still running", `data-gate="running"`, "Accept exact report and diff", "disabled"} {
		if !strings.Contains(markup, want) {
			t.Errorf("running gate lacks %q: %s", want, markup)
		}
	}
}

func TestFailedAndWaivedChecksRequireExactAcknowledgements(t *testing.T) {
	props := fixtureProps(t)
	props.Review.RequiredChecks = []acceptance.RequiredCheck{
		{ID: "unit", Status: acceptance.CheckFailed},
		{ID: "policy", Status: acceptance.CheckWaived},
	}
	markup := render(t, props)
	for _, want := range []string{"Acknowledge failed required check unit", "Acknowledge waived required check policy", "Explicit acknowledgement is required", `data-gate="acknowledgement-required"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("acknowledgement gate lacks %q: %s", want, markup)
		}
	}
	props.AcknowledgedCheckIDs = []string{"unit", "policy"}
	markup = render(t, props)
	acceptButton := elementTag(markup, `aria-label="Accept exact report and diff"`)
	if strings.Contains(markup, `data-gate="acknowledgement-required"`) || strings.Contains(acceptButton, "disabled") {
		t.Fatalf("fully acknowledged acceptance remains disabled: %s", markup)
	}
}

func TestChangedReportOrDiffRequiresRenewedReview(t *testing.T) {
	props := fixtureProps(t)
	props.CurrentDiffIdentity = strings.Repeat("e", 64)
	markup := render(t, props)
	for _, want := range []string{"changed after this review opened", "Open the renewed review", `data-stale="true"`, `data-gate="stale"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("stale review lacks %q: %s", want, markup)
		}
	}
}

func TestRepairSelectionIsBoundToFailuresAndHunks(t *testing.T) {
	props := fixtureProps(t)
	props.RepairFeedback = "Repair only these selected findings."
	props.RepairTargets = []SelectableRepairTarget{
		{Kind: acceptance.RepairTargetValidation, ID: "unit", Label: "unit tests", Status: acceptance.CheckFailed, Selected: true},
		{Kind: acceptance.RepairTargetHunk, ID: "hunk-7", Label: "parser branch", Selected: true},
		{Kind: acceptance.RepairTargetValidation, ID: "lint", Label: "lint", Status: acceptance.CheckPassed},
	}
	markup := render(t, props)
	for _, want := range []string{"Validation unit: unit tests (failed)", "Diff hunk hunk-7: parser branch", `data-selected-target-count="2"`, "Create new repair plan from selected targets"} {
		if !strings.Contains(markup, want) {
			t.Errorf("repair controls lack %q: %s", want, markup)
		}
	}
	if !strings.Contains(markup, "Validation lint: lint (passed)") || !strings.Contains(markup, "disabled") {
		t.Fatalf("passed validation target is selectable: %s", markup)
	}
}

func TestRollbackAndRejectionAreExplicitAndNonDestructive(t *testing.T) {
	props := fixtureProps(t)
	props.Rollback = &RollbackTarget{
		RepairRequestID: acceptance.RepairRequestID(strings.Repeat("4", 64)),
		CheckpointID:    checkpointID(t, "ckp_018f0123-4567-789a-8bcd-ef0123456789"),
	}
	markup := render(t, props)
	for _, want := range []string{"Roll back to pre-repair checkpoint", props.Rollback.CheckpointID.String(), "Reject and preserve patch", `data-preserve-patch="true"`, "remain available for inspection"} {
		if !strings.Contains(markup, want) {
			t.Errorf("non-destructive controls lack %q: %s", want, markup)
		}
	}
}

func fixtureProps(t *testing.T) Props {
	t.Helper()
	taskID, err := domain.ParseTaskID("tsk_018f0123-4567-789a-8bcd-ef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	return Props{
		Review: acceptance.Review{
			ID: acceptance.ReviewID(strings.Repeat("1", 64)), TaskID: taskID,
			Revision: 3, ReportID: strings.Repeat("2", 64), DiffIdentity: strings.Repeat("3", 64),
			PlanRevision: 2, OpenedBy: "user:test", IdempotencyKey: "review-test",
			OpenedAt: time.Unix(1, 0).UTC(),
		},
		CurrentReportID: strings.Repeat("2", 64), CurrentDiffIdentity: strings.Repeat("3", 64),
		OnAccept: func() {}, OnRequestRepair: func() {}, OnRollback: func() {},
		OnRejectPreserving: func() {},
	}
}

func checkpointID(t *testing.T, value string) domain.CheckpointID {
	t.Helper()
	id, err := domain.ParseCheckpointID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func render(t *testing.T, props Props) string {
	t.Helper()
	markup, err := ui.RenderToString(Component(props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func elementTag(markup, marker string) string {
	index := strings.Index(markup, marker)
	if index < 0 {
		return ""
	}
	start := strings.LastIndex(markup[:index], "<")
	end := strings.Index(markup[index:], ">")
	if start < 0 || end < 0 {
		return ""
	}
	return markup[start : index+end+1]
}

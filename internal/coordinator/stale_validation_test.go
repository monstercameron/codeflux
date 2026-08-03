package coordinator

import (
	"strings"
	"testing"
)

// TestValidationVerdictIsStaleAfterAWrite is the discrimination the
// verification gate needed.
//
// The gate read the most recent validation run and nothing else, so a run that
// tested, then edited, and then stopped reported "the project's own test
// command ran and passed" — about code that no longer existed.
//
// Observed on ladder rung 2 on 2026-08-03: integration-tests recorded
// satisfied with that exact sentence, while repetition and platform-matrix,
// which re-run the suite themselves, both failed in the same ledger. Running
// `go test ./...` in the worktree by hand confirmed the suite did not build:
// `cmd\generated\main_test.go:60:14: undefined: run`. One stage was reporting
// a verdict about superseded code.
func TestValidationVerdictIsStaleAfterAWrite(t *testing.T) {
	narrator := &narratingExecutor{ranValidation: true, validationFailed: false}
	if detail := validationDetail(narrator); !strings.Contains(detail, "passed") {
		t.Fatalf("a fresh passing run should read as passed, got %q", detail)
	}

	narrator.filesChangedSinceValidation = true
	detail := validationDetail(narrator)
	if strings.Contains(detail, "ran and passed") {
		t.Errorf("a write after the last test run supersedes its verdict, so "+
			"this must not claim the tests passed: %q", detail)
	}
	if !strings.Contains(detail, "written after the last test run") {
		t.Errorf("the reason should say why the verdict does not hold, got %q",
			detail)
	}
}

// TestAFailedWriteLeavesTheVerdictStanding is the control.
//
// The verdict is superseded by a write that changed something. A write that
// failed changed nothing, and treating it as invalidating would report an
// unchecked run for a worktree nobody touched.
func TestAFailedWriteLeavesTheVerdictStanding(t *testing.T) {
	narrator := &narratingExecutor{ranValidation: true}
	// filesChangedSinceValidation is only set on a successful apply-edit; this
	// pins the shape the caller relies on.
	if narrator.filesChangedSinceValidation {
		t.Fatal("nothing has been written yet")
	}
	if detail := validationDetail(narrator); !strings.Contains(detail, "passed") {
		t.Errorf("an untouched worktree keeps its passing verdict, got %q", detail)
	}
}

// TestARunThatNeverTestedStillSaysSo pins the case that already worked.
func TestARunThatNeverTestedStillSaysSo(t *testing.T) {
	narrator := &narratingExecutor{}
	if detail := validationDetail(narrator); !strings.Contains(
		detail, "never reached its validation step") {
		t.Errorf("a run that never tested must still say so, got %q", detail)
	}
}

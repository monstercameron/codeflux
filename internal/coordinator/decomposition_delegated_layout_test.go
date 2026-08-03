package coordinator

import (
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
)

// TestDecompositionDelegatedLayoutIsNotInventedWork is the discrimination the
// decomposition-coverage gate needed.
//
// The gate reports two different defects: files the request named that no step
// will write, and files steps will write that the request never named. The
// second arm read an empty asked-set as "the request named nothing, so
// everything is invented", which made the gate unpassable for every request
// that describes behaviour instead of naming files — the ordinary case, and
// the one the product exists to serve.
//
// Proven to discriminate: against the previous implementation this case
// returned both planned files as unasked, so the stage failed with "steps will
// write files the request never named: cmd/generated/main.go,
// cmd/generated/main_test.go". That is not a hypothetical — it is the exact
// detail two unrelated ladder rungs recorded, with correct produced code, on
// 2026-08-03.
func TestDecompositionDelegatedLayoutIsNotInventedWork(t *testing.T) {
	// A requirement that states a behaviour and names no file, which is what
	// a person asking for a program actually writes.
	requirement := "Write a command-line program whose main function prints " +
		"exactly this one line and nothing else: Hello from CodeFlux"
	steps := []agentloop.PlanStep{{
		ID:            "step-1",
		ExpectedFiles: []string{"cmd/generated/main.go", "cmd/generated/main_test.go"},
	}}

	uncovered, unasked := decompositionGaps(requirement, steps)
	if len(uncovered) != 0 {
		t.Errorf("a request naming no file cannot have dropped one, but the "+
			"gate reported %v uncovered", uncovered)
	}
	if len(unasked) != 0 {
		t.Errorf("the request delegated the layout, so choosing one is the "+
			"run's job rather than invented work, but the gate reported %v "+
			"as unasked", unasked)
	}
	if detail := decompositionDetail(uncovered, unasked); !strings.Contains(
		detail, "no step writes a file the request did not ask for") {
		t.Errorf("the recorded detail should read as a pass, got %q", detail)
	}
}

// TestDecompositionStillRefusesInventedWorkWhenTheRequestNamedFiles is the
// control, and it is what keeps the fix from being a hole.
//
// The delegation rule turns on the request having named nothing at all. A
// request that does name a file has stated an intent, and a step writing
// something else still contradicts it — that arm must keep firing, or the
// change would have deleted the check rather than corrected it.
func TestDecompositionStillRefusesInventedWorkWhenTheRequestNamedFiles(t *testing.T) {
	requirement := "Update internal/report/report.go so the summary line " +
		"includes the total."
	steps := []agentloop.PlanStep{{
		ID: "step-1",
		ExpectedFiles: []string{
			"internal/report/report.go",
			"internal/report/report_test.go",
			"internal/unrelated/helper.go",
		},
	}}

	_, unasked := decompositionGaps(requirement, steps)
	if len(unasked) == 0 {
		t.Fatal("a step writing a file outside what the request named is " +
			"still invented work and must still fail the gate")
	}
	for _, file := range unasked {
		if strings.HasSuffix(file, "_test.go") {
			t.Errorf("a test for a file the request named is wanted work, "+
				"not invented work, but %q was reported unasked", file)
		}
		if file == "internal/report/report.go" {
			t.Error("the file the request named must never be reported unasked")
		}
	}
}

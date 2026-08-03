package coordinator

import (
	"strings"
	"testing"
)

// TestMainIsNotAskedToReturnAnError covers a demand no Go program can meet.
//
// Ladder rung 3 was told twice per attempt that main swallowed an error by
// returning none of its own, could not comply, was sent back three times for
// the same finding, escalated to the most expensive rung on the resulting
// stall, and spent the rest of its budget there.
func TestMainIsNotAskedToReturnAnError(t *testing.T) {
	functions := []producedFunction{
		{Name: "run", Parameters: []string{"[]string"},
			Results: []string{"error"}, ReturnsError: true},
		{Name: "main", Calls: []string{"run"},
			Effects: []string{"fmt.Fprintln", "os.Exit"}},
	}
	for _, finding := range findUnhandledFailures(functions) {
		if finding.Where == "main" &&
			strings.Contains(finding.What, "returns no error of its own") {
			t.Errorf("main was asked for a return Go does not allow: %s",
				finding.What)
		}
	}
}

// TestMainMustReportAndExitNonZero is the obligation main genuinely has, and
// the one it can actually satisfy.
func TestMainMustReportAndExitNonZero(t *testing.T) {
	silent := []producedFunction{
		{Name: "run", Results: []string{"error"}, ReturnsError: true},
		{Name: "main", Calls: []string{"run"}, Effects: []string{"fmt.Fprintln"}},
	}
	found := false
	for _, finding := range findUnhandledFailures(silent) {
		if finding.Where == "main" && strings.Contains(finding.What, "os.Exit(1)") {
			found = true
		}
	}
	if !found {
		t.Error("a main that exits zero after a failure was not reported")
	}

	exiting := []producedFunction{
		{Name: "run", Results: []string{"error"}, ReturnsError: true},
		{Name: "main", Calls: []string{"run"},
			Effects: []string{"fmt.Fprintln", "os.Exit"}},
	}
	for _, finding := range findUnhandledFailures(exiting) {
		if finding.Where == "main" {
			t.Errorf("a main that already exits non-zero was still faulted: %s",
				finding.What)
		}
	}
}

// TestAMainThatCannotFailOwesNothing keeps the rule from becoming the same
// unfollowable instruction in a different sentence.
func TestAMainThatCannotFailOwesNothing(t *testing.T) {
	functions := []producedFunction{
		{Name: "greet", Results: []string{"string"}, Pure: true},
		{Name: "main", Calls: []string{"greet"}, Effects: []string{"fmt.Println"}},
	}
	for _, finding := range findUnhandledFailures(functions) {
		if finding.Where == "main" {
			t.Errorf("a main calling nothing fallible was faulted: %s", finding.What)
		}
	}
}

// TestARenamedFunctionIsTheSameCriticism covers the fingerprint.
//
// "main calls run, which can fail" and "main calls runCommand, which can fail"
// are one objection, and counting them as two lets a rename reset the
// repetition count that decides whether a run is stalling.
func TestARenamedFunctionIsTheSameCriticism(t *testing.T) {
	first := adversarialFinding{
		Kind: findingDefect, Where: "main",
		What:    "calls run, which can fail, but returns no error of its own",
		Lineage: findingLineageSwallowedError,
	}
	second := first
	second.What = "calls runCommand, which can fail, but returns no error of its own"
	if findingFingerprint(first) != findingFingerprint(second) {
		t.Errorf("a rename produced a different criticism:\n  %s\n  %s",
			findingFingerprint(first), findingFingerprint(second))
	}

	different := first
	different.What = "ignores the value strconv.Atoi returns"
	different.Lineage = findingLineageAntiPattern
	if findingFingerprint(first) == findingFingerprint(different) {
		t.Error("two different criticisms collapsed into one fingerprint")
	}
}

// TestOnlyAMeasuredDefectBlocks keeps the completion floor and the refinement
// ceiling apart: a demonstrated defect is a fact about the suite, a mechanical
// rule's output is an opinion about the code.
func TestOnlyAMeasuredDefectBlocks(t *testing.T) {
	findings := []adversarialFinding{
		{Where: "run", What: "a mutant survived", EvidenceLevel: findingEvidenceMeasured},
		{Where: "main", What: "an opinion", EvidenceLevel: findingEvidenceMechanicalRule},
		{Where: "run", What: "an untried input", EvidenceLevel: findingEvidenceSynthesizedCase},
	}
	if got := blockingFindings(findings); len(got) != 1 {
		t.Errorf("%d finding(s) block, wanted only the measured one", len(got))
	}
	if got := advisoryFindings(findings); len(got) != 2 {
		t.Errorf("%d advisory finding(s), wanted the two opinions", len(got))
	}
}

// TestATerminalReportNamesTheVerifiedRevision is the fact a reader most needs
// and which no ending used to state.
func TestATerminalReportNamesTheVerifiedRevision(t *testing.T) {
	report := terminalReport(terminalFacts{
		status:                "provider-unavailable-after-verified-result",
		verifiedRevision:      "23b6905d36cc",
		verifiedBecause:       "compiled, tests passed, acceptance matched",
		currentIsVerified:     true,
		attempts:              7,
		infrastructureRetries: 3,
		advisories: []adversarialFinding{
			{Where: "main", What: "an opinion"},
		},
	})
	for _, wanted := range []string{
		"provider-unavailable-after-verified-result",
		"23b6905d36cc",
		"The worktree is that revision",
		"lost to the provider rather than spent on the work",
		"1 advisory finding(s)",
	} {
		if !strings.Contains(report, wanted) {
			t.Errorf("the terminal report never says %q:\n%s", wanted, report)
		}
	}
}

// TestABlindSpotOnlyReviewAsksForTestsFirst covers the order rung 5 got wrong.
//
// It was given two blind spots — empty input and malformed JSON — rewrote its
// production code to address them, changed its output in the process, and spent
// four attempts getting back to a program it already had.
func TestABlindSpotOnlyReviewAsksForTestsFirst(t *testing.T) {
	blindSpotsOnly := []adversarialFinding{
		{Kind: findingBlindSpot, Where: "process",
			What: "is never given empty input"},
		{Kind: findingBlindSpot, Where: "process",
			What: "is never given malformed JSON"},
	}
	instruction := adversarialInstruction(blindSpotsOnly, true)
	for _, wanted := range []string{
		"Nothing here is a defect",
		"Add the missing test first",
		"Do not rewrite working code",
	} {
		if !strings.Contains(instruction, wanted) {
			t.Errorf("a blind-spot-only review never says %q:\n%s",
				wanted, instruction)
		}
	}

	// A review holding a real defect must not get the same advice: a
	// demonstrated defect is not closed by a test that passes.
	withDefect := append([]adversarialFinding{{
		Kind: findingDefect, Where: "process",
		What: "discards the error strconv.Atoi returns",
	}}, blindSpotsOnly...)
	if strings.Contains(adversarialInstruction(withDefect, true),
		"Nothing here is a defect") {
		t.Error("a review holding a defect was told there were none")
	}
}

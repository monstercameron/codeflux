package coordinator

import (
	"sort"
	"strings"
)

// findingClass says what kind of change a finding calls for, which is what
// decides who may edit what.
//
// The scope used to be derived from the gate that sent the work back, and a
// gate is one word about a whole review. Rung 6's review found a real defect in
// main and the next attempt was restricted to test files, because the gate said
// "adversarial-review" and the rule keyed on that. The finding knew what it
// wanted; nothing asked it.
type findingClass string

const (
	// findingProductionDefect is code that is wrong. Fixing it means changing
	// the program, and usually a test to prove it.
	findingProductionDefect findingClass = "production-defect"
	// findingMissingTest is behaviour nothing checks. The remedy is a test, and
	// the program is very often already right about it.
	findingMissingTest findingClass = "missing-test"
	// findingDocumentationGap is a comment, and nothing else.
	findingDocumentationGap findingClass = "documentation-gap"
)

// classify decides what a finding is asking for.
//
// Read from the finder that raised it rather than from the prose, because the
// prose is written for a person and the lineage is what the code decided. A
// swallowed error and a surviving mutant are defects in the program; an untried
// input is a missing test; everything the documentation gate raises is a
// comment.
func classifyFinding(finding adversarialFinding) findingClass {
	switch finding.Lineage {
	case findingLineageMutationSurvivor, findingLineageSwallowedError,
		findingLineageAntiPattern:
		return findingProductionDefect
	case findingLineageSynthesizedCase, findingLineageBoundary:
		return findingMissingTest
	}
	if finding.Kind == findingBlindSpot {
		return findingMissingTest
	}
	return findingProductionDefect
}

// scopeForFindings is what the next attempt may touch, derived from what it is
// being asked to fix rather than from which gate spoke.
//
// A round holding any production defect may change production code, because
// that is what a production defect is. A round holding only missing tests may
// not, because the program is usually already right and rewriting it is how
// rung 6 broke a working evaluator while adding two tests.
func scopeForFindings(findings []adversarialFinding) editScope {
	sawTest, sawDocumentation := false, false
	for _, finding := range findings {
		switch classifyFinding(finding) {
		case findingProductionDefect:
			return editAnything
		case findingMissingTest:
			sawTest = true
		case findingDocumentationGap:
			sawDocumentation = true
		}
	}
	switch {
	case sawTest:
		return editTestsOnly
	case sawDocumentation:
		return editCommentsOnly
	default:
		return editAnything
	}
}

// maximumFindingsPerRound is how many things one attempt is asked to fix.
//
// Two. A round given ten findings makes ten changes and verifies once, so a
// failure afterwards says only that one of ten changes was wrong. Two changes
// and a verification is a bisection; ten is a guess. Rung 6 was handed a full
// review and rewrote its files ten and thirteen times inside single attempts.
//
// Related ones, not the first two: fixing two findings in the same function is
// one edit, and fixing one in the parser and one in the printer is two, with
// the second undoing the verification the first earned.
const maximumFindingsPerRound = 2

// selectRoundFindings picks the small related set one attempt will work on.
//
// Ordered by what the review already decided matters — a measured defect
// outranks a rule's opinion, which outranks an untried input — and then grouped
// so the pair share a subject. What is left over is not discarded: it is still
// open, and the next round selects from it.
func selectRoundFindings(
	findings []adversarialFinding,
) (selected, remaining []adversarialFinding) {
	if len(findings) <= maximumFindingsPerRound {
		return findings, nil
	}
	ordered := append([]adversarialFinding(nil), findings...)
	sort.SliceStable(ordered, func(first, second int) bool {
		return findingCostRank[ordered[first].Lineage] >
			findingCostRank[ordered[second].Lineage]
	})
	selected = append(selected, ordered[0])
	subject := ordered[0].Where
	for _, candidate := range ordered[1:] {
		if len(selected) >= maximumFindingsPerRound {
			remaining = append(remaining, candidate)
			continue
		}
		if candidate.Where == subject {
			selected = append(selected, candidate)
			continue
		}
		remaining = append(remaining, candidate)
	}
	return selected, remaining
}

// findingLedger tracks what has been asked for and what came of it, across
// attempts.
//
// Without it a run resends the same prose and hopes: the review runs again,
// produces the same findings, and nothing knows whether the last attempt
// addressed them or ignored them. Rung 6 sent the same criticism three times.
//
// The states are deliberately few. Open is not yet worked on; addressed means
// an attempt was given it and made changes; verified means a later review no
// longer raises it. A finding that survives being addressed twice is one this
// run is not going to fix, and saying so is more useful than a fourth attempt.
type findingLedger struct {
	addressed map[string]int
	verified  map[string]bool
}

func newFindingLedger() *findingLedger {
	return &findingLedger{addressed: map[string]int{}, verified: map[string]bool{}}
}

// record marks the findings an attempt was given.
func (ledger *findingLedger) record(findings []adversarialFinding) {
	for _, finding := range findings {
		ledger.addressed[findingFingerprint(finding)]++
	}
}

// reconcile marks anything previously addressed and no longer raised as
// verified, and reports which of the current findings are worth another
// attempt.
//
// A finding addressed twice and still raised is dropped from the working set.
// It is not resolved and it is not going to be by repetition; carrying it as an
// advisory is honest, and spending a third attempt on it is not.
func (ledger *findingLedger) reconcile(
	current []adversarialFinding,
) (workable, exhausted []adversarialFinding) {
	raised := map[string]bool{}
	for _, finding := range current {
		raised[findingFingerprint(finding)] = true
	}
	for print := range ledger.addressed {
		if !raised[print] {
			ledger.verified[print] = true
		}
	}
	for _, finding := range current {
		if ledger.addressed[findingFingerprint(finding)] >= 2 {
			exhausted = append(exhausted, finding)
			continue
		}
		workable = append(workable, finding)
	}
	return workable, exhausted
}

// summary is one line for the trace: how the working set moved.
func (ledger *findingLedger) summary(open, exhausted int) string {
	return strings.Join([]string{
		"open " + itoaPlan(open),
		"addressed " + itoaPlan(len(ledger.addressed)),
		"verified " + itoaPlan(len(ledger.verified)),
		"exhausted " + itoaPlan(exhausted),
	}, ", ")
}

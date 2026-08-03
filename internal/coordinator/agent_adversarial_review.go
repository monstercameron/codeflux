package coordinator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// findingKind groups what a review found, so the instruction can be ordered by
// what is worth fixing first.
//
// The order is not cosmetic. A swallowed error is a defect and an untried
// empty slice is a gap, and asking for both in one flat list gets the gap
// fixed and the defect left alone, because the gap is the smaller job.
type findingKind string

const (
	// findingDefect is code that is wrong in a way no current test would
	// notice: a swallowed error, a shadowed name, an unchecked assertion.
	findingDefect findingKind = "defects — code that is wrong in a way no test would notice"
	// findingBlindSpot is behaviour nothing checks: a mutation that survived,
	// an input never tried.
	findingBlindSpot findingKind = "blind spots — behaviour nothing checks"
)

// adversarialFinding is one way the work is weaker than it looks.
type adversarialFinding struct {
	Kind findingKind
	// Where names the function or file the finding is about, so the next
	// attempt is told which thing to fix rather than that something is wrong.
	Where string
	What  string
	// Fix says what to do about it, where that is not obvious from the finding.
	Fix string
}

// reviewAdversarially attacks the work rather than waiting for a gate to trip.
//
// The gates ask whether declared conditions hold. This asks the different and
// harder question a reviewer asks: given that everything passed, where is this
// still weak? A suite can be green because the code is right or because the
// tests do not look anywhere interesting, and only one of those is worth
// shipping.
//
// It is deliberately run when the work already compiles and passes. Attacking
// broken code tells you what you already know.
func (execution *AgentExecution) reviewAdversarially(
	ctx context.Context,
	worktree string,
) ([]adversarialFinding, error) {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return nil, err
	}
	var findings []adversarialFinding
	// Hygiene belongs here rather than in a pass of its own. Three separate
	// reviews each cost an attempt and each showed the model a third of the
	// picture, so it fixed things piecemeal and had fewer attempts left to fix
	// anything properly. One review, everything at once, ordered by what
	// actually matters.
	findings = append(findings, findAntiPatternFindings(worktree)...)
	findings = append(findings, execution.findUnkilledMutants(ctx, worktree)...)
	findings = append(findings, findUnhandledFailures(functions)...)
	findings = append(findings, findUncheckedBoundaries(worktree, functions)...)
	findings = append(findings, findUntriedCaseFindings(worktree)...)
	sort.Slice(findings, func(first, second int) bool {
		if findings[first].Kind != findings[second].Kind {
			return findings[first].Kind == findingDefect
		}
		if findings[first].Where != findings[second].Where {
			return findings[first].Where < findings[second].Where
		}
		return findings[first].What < findings[second].What
	})
	return findings, nil
}

// findUnkilledMutants reports defects the tests did not notice.
//
// This is the sharpest question available: not whether the tests pass, but
// whether they would still pass if the code were wrong. A mutation that
// survives names a specific line whose behaviour nothing checks, which is a
// far more actionable finding than a coverage percentage.
func (execution *AgentExecution) findUnkilledMutants(
	ctx context.Context,
	worktree string,
) []adversarialFinding {
	outcome := execution.checkMutations(ctx, worktree)
	survivors, present := outcome.Evidence["survivors"].([]string)
	if !present || len(survivors) == 0 {
		return nil
	}
	findings := make([]adversarialFinding, 0, len(survivors))
	for _, survivor := range survivors {
		file, why, _ := strings.Cut(survivor, ": ")
		findings = append(findings, adversarialFinding{
			Kind:  findingBlindSpot,
			Where: strings.TrimSpace(file),
			What: "a deliberate defect survived every test — " +
				strings.TrimSpace(why) + " — so nothing checks that behaviour",
		})
	}
	return findings
}

// findUnhandledFailures reports work that can fail without saying so.
//
// A function returning an error whose caller ignores it turns a failure into
// a wrong answer, which is the failure mode that survives longest in the wild
// because nothing about it looks broken.
func findUnhandledFailures(functions []producedFunction) []adversarialFinding {
	canFail := map[string]bool{}
	for _, function := range functions {
		if function.ReturnsError {
			canFail[function.Name] = true
		}
	}
	var findings []adversarialFinding
	for _, function := range functions {
		if isTestScaffolding(function) || !function.Pure {
			continue
		}
		// A pure function that calls something able to fail, and returns no
		// error of its own, has swallowed it.
		if function.ReturnsError {
			continue
		}
		for _, called := range function.Calls {
			if canFail[called] {
				findings = append(findings, adversarialFinding{
					Kind:  findingDefect,
					Where: function.Name,
					What: "calls " + called + ", which can fail, but returns no " +
						"error of its own, so the failure disappears here",
				})
			}
		}
	}
	return findings
}

// findUncheckedBoundaries reports the inputs a test never reaches for.
//
// Tests written from a working example examine the middle of the input space.
// The interesting behaviour is at its edges, and a suite that never mentions
// an empty collection, a zero, or a negative number has not been there.
// boundaryEdge is one input worth asking whether the tests ever tried.
type boundaryEdge struct {
	// token is what the suite would have to mention to have tried it.
	token string
	what  string
	// because names the parameter that makes the question apply, so a finding
	// can be checked rather than taken on faith.
	because string
}

// edgesWorthChecking derives the boundaries from the produced signatures.
//
// The point of asking about an edge is that some function could be handed it.
// A program that takes no strings cannot be handed the empty string, and
// reporting that gap taught nothing about the program and buried the findings
// that did.
func edgesWorthChecking(functions []producedFunction) []boundaryEdge {
	// One edge per kind, attributed to the first parameter that raises it, so
	// the same question is not asked once per function.
	seen := map[string]bool{}
	var edges []boundaryEdge
	add := func(token, what, because string) {
		if seen[token] {
			return
		}
		seen[token] = true
		edges = append(edges, boundaryEdge{token, what, because})
	}
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		for _, parameter := range function.Parameters {
			naming := fmt.Sprintf("%s takes %s", function.Name, parameter)
			base := strings.TrimPrefix(parameter, "...")
			switch {
			case base == "string":
				add(`""`, "the empty string", naming)
			case strings.HasPrefix(base, "[]"), strings.HasPrefix(base, "map["):
				add(`nil`, "a nil value", naming)
				add(`{}`, "an empty collection", naming)
			case strings.HasPrefix(base, "*"), base == "error", base == "any":
				add(`nil`, "a nil value", naming)
			case isSignedInteger(base):
				add(`-1`, "a negative number", naming)
				add(`0`, "zero", naming)
			case isFloat(base):
				add(`0`, "zero", naming)
			}
		}
	}
	return edges
}

// isSignedInteger reports whether a type can hold a value below zero, which is
// the only reason to ask whether a negative one was tried.
func isSignedInteger(typeName string) bool {
	switch typeName {
	case "int", "int8", "int16", "int32", "int64", "rune":
		return true
	}
	return false
}

// isFloat reports whether a type carries a fractional value.
func isFloat(typeName string) bool {
	return typeName == "float32" || typeName == "float64"
}

func findUncheckedBoundaries(
	worktree string,
	functions []producedFunction,
) []adversarialFinding {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil
	}
	var tests strings.Builder
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(worktree, file))
		if readErr != nil {
			continue
		}
		tests.WriteString(string(body))
	}
	if tests.Len() == 0 {
		return nil
	}
	written := tests.String()

	var findings []adversarialFinding
	// Which edges are worth asking about comes from what the produced
	// functions actually accept, not from a fixed list. A fixed list said
	// "never mentions the empty string" about a program with no string
	// parameter anywhere — a finding that was true, unactionable, and had
	// nothing to do with the code under review.
	for _, edge := range edgesWorthChecking(functions) {
		if strings.Contains(written, edge.token) {
			continue
		}
		findings = append(findings, adversarialFinding{
			Kind:  findingBlindSpot,
			Where: "the test suite",
			What: "never mentions " + edge.what + ", though " + edge.because +
				", so the behaviour at that edge is unexamined",
		})
	}
	// A suite with no failing-path assertion has only ever watched things work.
	if !strings.Contains(written, "err != nil") &&
		!strings.Contains(written, "Error") &&
		anyReturnsError(functions) {
		findings = append(findings, adversarialFinding{
			Kind:  findingBlindSpot,
			Where: "the test suite",
			What: "never asserts on a returned error, though the code can fail, " +
				"so no test has seen it fail",
		})
	}
	return findings
}

// anyReturnsError reports whether the produced code can fail at all.
func anyReturnsError(functions []producedFunction) bool {
	for _, function := range functions {
		if !isTestScaffolding(function) && function.ReturnsError {
			return true
		}
	}
	return false
}

// adversarialInstruction is what the next attempt is told to do about the
// findings.
//
// Grouped by kind, defects first, because a flat list gets the easy items
// fixed and the important ones left: an untried empty slice is a smaller job
// than a swallowed error, and an agent working a list in order does the small
// one and runs out of attempts.
func adversarialInstruction(findings []adversarialFinding) string {
	var report strings.Builder
	report.WriteString(
		"The code compiles and its tests pass. A review found it is still " +
			"weaker than it looks:\n")

	grouped := map[findingKind][]adversarialFinding{}
	for _, finding := range findings {
		grouped[finding.Kind] = append(grouped[finding.Kind], finding)
	}
	for _, kind := range []findingKind{findingDefect, findingBlindSpot} {
		if len(grouped[kind]) == 0 {
			continue
		}
		fmt.Fprintf(&report, "\n%s\n", kind)
		for _, finding := range grouped[kind] {
			fmt.Fprintf(&report, "\n- %s: %s", finding.Where, finding.What)
			if finding.Fix != "" {
				fmt.Fprintf(&report, "\n  %s", finding.Fix)
			}
		}
	}
	report.WriteString(
		"\n\nFix the defects first, then close the blind spots. Do not weaken " +
			"an assertion to make something pass, and do not change behaviour " +
			"the request asked for.")
	return report.String()
}

// findAntiPatternFindings brings hygiene into the review.
func findAntiPatternFindings(worktree string) []adversarialFinding {
	patterns, err := findAntiPatterns(worktree)
	if err != nil {
		return nil
	}
	findings := make([]adversarialFinding, 0, len(patterns))
	for _, pattern := range patterns {
		findings = append(findings, adversarialFinding{
			Kind: findingDefect, Where: pattern.Where,
			What: pattern.What, Fix: pattern.Why,
		})
	}
	return findings
}

// findUntriedCaseFindings brings the synthesised inputs into the review.
func findUntriedCaseFindings(worktree string) []adversarialFinding {
	owed, err := untriedCases(worktree)
	if err != nil {
		return nil
	}
	var findings []adversarialFinding
	for name, cases := range owed {
		for _, candidate := range cases {
			findings = append(findings, adversarialFinding{
				Kind: findingBlindSpot, Where: name,
				What: fmt.Sprintf("is never given %s (%s)",
					candidate.Shape, candidate.Why),
				Fix: string(candidate.Class) + " — " + candidate.Class.assertion(),
			})
		}
	}
	return findings
}

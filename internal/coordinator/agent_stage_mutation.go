package coordinator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// mutation is one deliberate defect introduced to see whether the tests notice.
type mutation struct {
	// From and To are the exact text swapped. They are chosen to change
	// behaviour without changing whether the code compiles: a mutation that
	// fails to build tells you nothing, because every suite "catches" it.
	From string
	To   string
	Why  string
}

// mutations are the defects worth trying.
//
// They are the mistakes that actually get made — an inverted comparison, an
// off-by-one boundary, the wrong arithmetic operator, a dropped negation — and
// each one changes what the program computes while leaving it valid Go.
var mutations = []mutation{
	{From: " == ", To: " != ", Why: "equality inverted"},
	{From: " != ", To: " == ", Why: "inequality inverted"},
	{From: " < ", To: " <= ", Why: "boundary moved outward"},
	{From: " > ", To: " >= ", Why: "boundary moved outward"},
	{From: " <= ", To: " < ", Why: "boundary moved inward"},
	{From: " >= ", To: " > ", Why: "boundary moved inward"},
	{From: " + ", To: " - ", Why: "addition became subtraction"},
	{From: " * ", To: " + ", Why: "multiplication became addition"},
	{From: " && ", To: " || ", Why: "conjunction became disjunction"},
	{From: " || ", To: " && ", Why: "disjunction became conjunction"},
}

// checkMutations measures whether the tests can detect a defect at all.
//
// Every other check in the flow asks whether the tests pass. This asks the
// question underneath it: if the code were wrong, would anybody know? A suite
// that passes against deliberately broken code is not evidence of anything,
// and until this ran there was no way to tell that suite apart from a good one.
//
// Each mutation is applied to one produced source file, the suite is run, and
// the file is put back. A mutation the suite fails on is caught; one it passes
// is a hole. The score is the proportion caught.
func (execution *AgentExecution) checkMutations(
	ctx context.Context,
	worktree string,
) stageOutcome {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return broke("the produced source could not be read: "+err.Error(), nil)
	}
	var sources []string
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			sources = append(sources, file)
		}
	}
	if len(sources) == 0 {
		return skipped("the run produced no source to mutate")
	}

	// A bounded number of mutations, because each one costs a full test run
	// and the answer stops changing shape well before the budget is spent.
	const maximumMutations = 12
	applied, caught := 0, 0
	var survivors []string

	for _, file := range sources {
		path := filepath.Join(worktree, file)
		original, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		for _, candidate := range mutations {
			if applied >= maximumMutations {
				break
			}
			body := string(original)
			if !strings.Contains(body, candidate.From) {
				continue
			}
			// Only the first occurrence is changed. Mutating every one at once
			// would produce a program broken in so many ways that catching it
			// says nothing about which test caught what.
			mutated := strings.Replace(body, candidate.From, candidate.To, 1)
			if mutated == body {
				continue
			}
			if writeErr := os.WriteFile(path, []byte(mutated), 0o600); writeErr != nil {
				continue
			}
			applied++
			if execution.suiteRejects(ctx, worktree) {
				caught++
			} else {
				survivors = append(survivors,
					fmt.Sprintf("%s: %s", file, candidate.Why))
			}
			// The file is restored before the next mutation regardless of the
			// outcome. Leaving a mutation in place would ship deliberately
			// broken code, which is the one failure this check must not cause.
			if restoreErr := os.WriteFile(path, original, 0o600); restoreErr != nil {
				return broke("a mutation could not be undone, so the worktree "+
					"may hold deliberately broken code: "+restoreErr.Error(), nil)
			}
		}
	}

	if applied == 0 {
		return skipped("the produced source held nothing worth mutating")
	}
	score := float64(caught) / float64(applied) * 100
	evidence := map[string]any{
		"mutations_applied": applied,
		"mutations_caught":  caught,
		"score_percent":     score,
		"survivors":         survivors,
	}
	// A suite that catches nothing is worthless; one that catches most is
	// doing its job. The threshold is deliberately low, because the point of
	// the first version of this check is to catch the suite that detects
	// nothing at all, and a high bar would make it fire on work that is merely
	// imperfect.
	const threshold = 50.0
	if score < threshold {
		return broke(fmt.Sprintf(
			"the tests caught %d of %d deliberate defects (%.0f%%), so they "+
				"cannot be relied on to notice a real one: %s",
			caught, applied, score, strings.Join(survivors, "; ")), evidence)
	}
	return held(fmt.Sprintf(
		"the tests caught %d of %d deliberate defects (%.0f%%)",
		caught, applied, score), evidence)
}

// suiteRejects reports whether the repository's tests fail as they stand.
func (execution *AgentExecution) suiteRejects(
	ctx context.Context,
	worktree string,
) bool {
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	command.Dir = worktree
	_, err := command.CombinedOutput()
	return err != nil
}

package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
)

const evaluatorBefore = `package main

import "errors"

// evaluate reduces a token stream to one value.
func evaluate(tokens []string) (int, error) {
	if len(tokens) == 0 {
		return 0, errors.New("empty expression")
	}
	return 0, nil
}
`

// TestADocumentationRoundMayNotChangeBehaviour covers what rung 6 did: asked
// for a comment on main, it rewrote the evaluator's error semantics.
func TestADocumentationRoundMayNotChangeBehaviour(t *testing.T) {
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, "main.go"),
		[]byte(evaluatorBefore), 0o644); err != nil {
		t.Fatal(err)
	}

	// A comment added, nothing else touched: allowed.
	commented := strings.Replace(evaluatorBefore,
		"// evaluate reduces a token stream to one value.",
		"// evaluate reduces a token stream to one value.\n//\n// It refuses an "+
			"empty expression rather than returning zero.", 1)
	if allowed, why := editCommentsOnly.permits(
		worktree, "main.go", []byte(commented),
	); !allowed {
		t.Errorf("a comment-only edit was refused: %s", why)
	}

	// One word of the error message changed: refused.
	reworded := strings.Replace(evaluatorBefore,
		`errors.New("empty expression")`,
		`errors.New("expression leaves 0 values on the stack")`, 1)
	allowed, why := editCommentsOnly.permits(worktree, "main.go", []byte(reworded))
	if allowed {
		t.Error("a documentation round rewrote an error message unchallenged")
	}
	if !strings.Contains(why, "changes what main.go does") {
		t.Errorf("the refusal does not say what is wrong: %s", why)
	}

	// Reformatting alone is not a behaviour change.
	reformatted := strings.ReplaceAll(evaluatorBefore, "\t", "    ")
	if allowed, why := editCommentsOnly.permits(
		worktree, "main.go", []byte(reformatted),
	); !allowed {
		t.Errorf("reformatting was read as a behaviour change: %s", why)
	}
}

// TestABlindSpotRoundMayOnlyWriteTests is the test-first rule made real.
//
// The instruction already said it. Rung 6 rewrote production code in the same
// turn anyway and broke a program that was already correct about both inputs.
func TestABlindSpotRoundMayOnlyWriteTests(t *testing.T) {
	worktree := t.TempDir()
	if allowed, _ := editTestsOnly.permits(
		worktree, "cmd/generated/main_test.go", []byte("package main"),
	); !allowed {
		t.Error("a blind-spot round refused a test file")
	}
	allowed, why := editTestsOnly.permits(
		worktree, "cmd/generated/main.go", []byte("package main"))
	if allowed {
		t.Error("a blind-spot round rewrote production code")
	}
	if !strings.Contains(why, "write the test, run it") {
		t.Errorf("the refusal does not say what to do instead: %s", why)
	}
}

// TestAnOrdinaryRoundIsUnrestricted keeps the rule narrow: a run building or
// repairing may touch whatever it needs.
func TestAnOrdinaryRoundIsUnrestricted(t *testing.T) {
	for _, path := range []string{"main.go", "main_test.go", "go.mod"} {
		if allowed, why := editAnything.permits(
			t.TempDir(), path, []byte("package main"),
		); !allowed {
			t.Errorf("an ordinary round was refused %s: %s", path, why)
		}
	}
}

// TestTheScopeComesFromTheGate pins the mapping, since the prose and the gate
// can drift and only one of them is what the coordinator decided.
func TestTheScopeComesFromTheGate(t *testing.T) {
	for _, expected := range []struct {
		gate           string
		blindSpotsOnly bool
		scope          editScope
	}{
		{"adversarial-review", true, editTestsOnly},
		{"adversarial-review", false, editAnything},
		{"atom-documentation", false, editCommentsOnly},
		{"acceptance", false, editAnything},
		{"integration-tests", false, editAnything},
	} {
		if got := scopeOfNextAttempt(
			expected.gate, expected.blindSpotsOnly,
		); got != expected.scope {
			t.Errorf("gate %q (blind spots only: %t) permits %s, wanted %s",
				expected.gate, expected.blindSpotsOnly, got, expected.scope)
		}
	}
}

// TestAPatchToolNobodyIsOfferedIsNoPatchTool covers the defect that made every
// other patch change pointless.
//
// The loop offers a model only the tools an open step declares. Listing the
// whole-file tool alone made apply-patch unreachable: registered, described,
// tested, and never once in tools_you_may_call. Rung 6 wrote whole files eleven
// and thirteen times in single attempts with the patch tool sitting unoffered.
func TestAPatchToolNobodyIsOfferedIsNoPatchTool(t *testing.T) {
	for _, step := range agentPlanSteps(
		"Write cmd/generated/main.go that prints a line.",
	) {
		if step.Kind != agentloop.StepKindEdit {
			continue
		}
		offered := map[executor.ToolName]bool{}
		for _, tool := range step.CompletionTools {
			offered[tool] = true
		}
		if !offered[executor.ToolApplyPatch] {
			t.Errorf("step %q cannot be completed by a patch, so the patch "+
				"tool will never be offered for it", step.ID)
		}
		if !offered[executor.ToolApplyEdit] {
			t.Errorf("step %q cannot be completed by a whole-file write, so a "+
				"file cannot be created for it", step.ID)
		}
	}
}

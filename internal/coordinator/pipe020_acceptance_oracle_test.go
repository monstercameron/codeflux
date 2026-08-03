package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// TestPIPE020_AnExampleAlreadyTrueBeforeAnyWorkFailsTheOracle proves the
// control catches the case it exists for: an acceptance example a stub the
// repository already holds satisfies, which would otherwise clear both
// StageInstructions (PIPE-019) and StageEndToEndTests without either one
// having examined this run's own work.
func TestPIPE020_AnExampleAlreadyTrueBeforeAnyWorkFailsTheOracle(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/echo/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"hello\")\n}\n",
	})
	requirement := "Create cmd/echo/main.go that prints hello.\n\n" +
		"<<<ACCEPTANCE\nexpected:\nhello\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 {
		t.Fatalf("%d example(s) parsed, want 1", len(examples))
	}

	execution := &AgentExecution{settings: pipeline.DefaultSettings()}
	outcome := execution.checkAcceptanceOracle(
		t.Context(), worktree, requirement, examples)

	if outcome.Held {
		t.Fatalf("the oracle held for an example the repository already "+
			"satisfies before any work: %s", outcome.Detail)
	}
	if outcome.Skipped {
		t.Fatalf("the oracle skipped an example that was actually run: %s",
			outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "already hold") {
		t.Errorf("the detail does not say the example already held: %q",
			outcome.Detail)
	}
}

// TestPIPE020_AnExampleThatFailsAgainstNothingSatisfiesTheOracle is the good
// case: a repository with no command yet matching the request fails the
// example, which is what proves the example discriminates.
func TestPIPE020_AnExampleThatFailsAgainstNothingSatisfiesTheOracle(t *testing.T) {
	worktree := writeWorktree(t, nil)
	requirement := "Create cmd/echo/main.go that prints hello.\n\n" +
		"<<<ACCEPTANCE\nexpected:\nhello\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 {
		t.Fatalf("%d example(s) parsed, want 1", len(examples))
	}

	execution := &AgentExecution{settings: pipeline.DefaultSettings()}
	outcome := execution.checkAcceptanceOracle(
		t.Context(), worktree, requirement, examples)

	if !outcome.Held {
		t.Fatalf("the oracle did not hold for an example that genuinely "+
			"fails against an empty repository: %s", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "discriminate") {
		t.Errorf("the detail does not say the example was shown to "+
			"discriminate: %q", outcome.Detail)
	}
}

// TestPIPE020_NoExampleIsSkippedRatherThanHeld keeps the oracle from claiming
// to have proven anything when PIPE-019's own gate has already failed.
func TestPIPE020_NoExampleIsSkippedRatherThanHeld(t *testing.T) {
	worktree := writeWorktree(t, nil)
	execution := &AgentExecution{settings: pipeline.DefaultSettings()}
	outcome := execution.checkAcceptanceOracle(
		t.Context(), worktree, "Build the thing.", nil)

	if !outcome.Skipped {
		t.Errorf("outcome = %+v, want skipped: with no example there is "+
			"nothing to prove discriminates", outcome)
	}
	if outcome.Held {
		t.Error("a skipped oracle must not also read as held")
	}
}

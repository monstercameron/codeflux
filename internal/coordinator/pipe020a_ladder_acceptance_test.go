package coordinator

import (
	"strings"
	"testing"
)

// PIPE-020a: TestTheEngineProducesProgramsThatBuildAndRun is gated behind
// CODEFLUX_LADDER and must never run in an ordinary `go test ./...`, because
// it calls a real provider on every rung and costs real money. That gate is
// exactly why a defect in what it sends the engine can sit invisible until
// somebody spends money on the ladder: PIPE-019 made an executable acceptance
// example mandatory at the instructions stage, and PIPE-020 made the
// acceptance-oracle stage require that example to fail against the
// repository before any work happens. Neither of those can be checked by
// running the ladder here — that is the one thing this file must not do.
//
// What can be checked without a provider is the two predicates themselves,
// applied to exactly the text the ladder would send. Both tests below build
// program.requirement+program.acceptanceBlock() — the identical expression
// buildAndRun passes to carryOut — for every one of ladderRungs(), and run it
// through the real, unexported functions the engine gates on:
// parseAcceptanceExamples/parseNamedTestExamples for the instructions stage
// (agent_execution.go, around the totalExamples check that guards
// pipeline.StageInstructions), and (*AgentExecution).checkAcceptanceOracle
// for the acceptance-oracle stage (agent_acceptance.go, PIPE-020). Neither
// function is reimplemented here; the same code the engine runs is called
// directly, against a worktree instrumented to look exactly like the one an
// isolated ladder run starts from: a bare module with nothing produced yet.
//
// What this does NOT prove: that a real model, shown this exact text, goes on
// to satisfy pipeline.StageEndToEndTests — that still depends on the model's
// output, which nothing offline can stand in for. It also does not prove the
// ladder's other 36+ stages hold, or that reportFlow logs no StateFailed row
// for reasons unrelated to acceptance. Those remain unverified until someone
// deliberately runs the ladder with CODEFLUX_LADDER set, which this file does
// not do and must not do.

// pipe020aFullRequirement is exactly what buildAndRun sends the engine for one
// rung: program.requirement with program.acceptanceBlock() appended. Kept as
// its own helper so both tests below build the string identically to
// buildAndRun (engine_produces_program_test.go) and to each other.
func pipe020aFullRequirement(program generatedProgram) string {
	return program.requirement + program.acceptanceBlock()
}

// TestPIPE020a_EveryLadderRequirementSatisfiesTheInstructionsStagePredicate
// reproduces, offline, the exact check pipeline.StageInstructions runs
// (agent_execution.go: `totalExamples := len(examples) + len(namedExamples)`,
// gated on totalExamples > 0). If this fails, the real ladder's first stage
// fails on every rung before a single provider call is made — which is
// precisely the defect PIPE-020a exists to catch before anyone pays for it.
func TestPIPE020a_EveryLadderRequirementSatisfiesTheInstructionsStagePredicate(t *testing.T) {
	rungs := ladderRungs()
	if len(rungs) == 0 {
		t.Fatal("the ladder is empty; nothing to check")
	}
	for _, rung := range rungs {
		full := pipe020aFullRequirement(rung)
		examples := parseAcceptanceExamples(full)
		named := parseNamedTestExamples(full)
		total := len(examples) + len(named)
		if total == 0 {
			t.Errorf("rung %q carries no executable acceptance example: the "+
				"instructions stage would fail this rung before the engine "+
				"attempted anything", rung.name)
			continue
		}
		// A count alone would pass a rung whose parsed example lost the
		// requirement's own answer along the way — the acceptance block
		// escaped or truncated, say. Tie the count to content: the command
		// form the ladder emits carries exactly one example per rung, and its
		// Expected field is exactly rung.expected.
		if len(named) != 0 {
			t.Errorf("rung %q produced a named-test example, which no rung in "+
				"this ladder is written to use: %v", rung.name, named)
		}
		if len(examples) != 1 {
			t.Errorf("rung %q produced %d command-form examples, want exactly "+
				"1", rung.name, len(examples))
			continue
		}
		if got := examples[0].Expected; got != rung.expected {
			t.Errorf("rung %q parsed back a different answer than it declared: "+
				"got %q, want %q", rung.name, got, rung.expected)
		}
	}
}

// TestPIPE020a_EveryLadderRequirementsExampleDiscriminatesAgainstAnUntouchedWorktree
// reproduces, offline, PIPE-020's acceptance-oracle stage
// ((*AgentExecution).checkAcceptanceOracle) for every rung, run against a
// worktree instrumented to look exactly like the one an isolated ladder run
// starts from: a bare Go module, git-initialised, nothing produced. That is
// the state checkAcceptanceOracle runs against in the real flow too — it runs
// before scope.worktree is touched by anything this run does.
//
// checkAcceptanceOracle needs no repositories, event hub, model, or
// redaction pipeline: it only builds and runs the program its own example
// names, so a zero-value *AgentExecution calls the same code path the real
// engine runs without needing a provider, a database, or CODEFLUX_LADDER.
//
// A ladder rung whose example already holds against an empty module (an
// empty Expected being trivially satisfied, say) would report broke here
// exactly as it would in the real oracle stage; TestPIPE020a_ANonDiscriminatingExampleIsCaught
// below is the control that proves this test would actually notice one.
func TestPIPE020a_EveryLadderRequirementsExampleDiscriminatesAgainstAnUntouchedWorktree(t *testing.T) {
	// One bare module shared across every rung. checkAcceptance only builds
	// and runs a command when producedCommands finds one this run wrote — and
	// on a worktree with nothing but go.mod, it never does, so nothing here
	// leaves anything behind between rungs.
	worktree := writeWorktree(t, nil)
	execution := &AgentExecution{}

	rungs := ladderRungs()
	if len(rungs) == 0 {
		t.Fatal("the ladder is empty; nothing to check")
	}
	for _, rung := range rungs {
		full := pipe020aFullRequirement(rung)
		examples := parseAcceptanceExamples(full)
		if len(examples) == 0 {
			// Already reported by the instructions-stage test above; this test
			// answers a different question and would only obscure that one by
			// repeating it.
			continue
		}
		outcome := execution.checkAcceptanceOracle(t.Context(), worktree, full, examples)
		switch {
		case outcome.Skipped:
			t.Errorf("rung %q: the oracle found nothing to prove: %s",
				rung.name, outcome.Detail)
		case !outcome.Held:
			t.Errorf("rung %q: the oracle did not hold — its example does not "+
				"discriminate against an untouched repository: %s",
				rung.name, outcome.Detail)
		}
	}
}

// TestPIPE020a_ANonDiscriminatingExampleIsCaught is the control for the test
// above: it proves checkAcceptanceOracle would actually report a bad example
// as bad, rather than the previous test passing because "Held" is what it
// always returns regardless of input.
//
// A worktree already carrying a program that satisfies the example — as if
// the work had already been done before this run started — must make the
// oracle report broke, because an example true before the work happened is
// not evidence the work happened.
func TestPIPE020a_ANonDiscriminatingExampleIsCaught(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/greet/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func main() { fmt.Println(\"already done\") }\n",
	})
	already := generatedProgram{
		name: "control: already satisfied",
		requirement: "Write a command-line program that prints exactly this " +
			"one line and nothing else: already done",
		expected: "already done",
	}
	full := pipe020aFullRequirement(already)
	examples := parseAcceptanceExamples(full)
	if len(examples) != 1 {
		t.Fatalf("the control example did not parse: got %d examples", len(examples))
	}
	execution := &AgentExecution{}
	outcome := execution.checkAcceptanceOracle(t.Context(), worktree, full, examples)
	if outcome.Held {
		t.Fatalf("the oracle held for an example a pre-existing program "+
			"already satisfies, which means it cannot be trusted to catch "+
			"a real one: %s", outcome.Detail)
	}
	if outcome.Skipped {
		t.Fatalf("the oracle skipped instead of examining the pre-existing "+
			"program: %s", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "already hold") {
		t.Errorf("the oracle broke for an unexpected reason: %s", outcome.Detail)
	}
}

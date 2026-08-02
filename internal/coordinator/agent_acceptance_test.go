package coordinator

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnAcceptanceBlockIsReadBackExactly is the parser's own check.
//
// The block is the only ground truth in the flow. If it is read back even
// slightly wrong, every run is judged against something the person did not
// ask for, which is worse than not checking at all.
func TestAnAcceptanceBlockIsReadBackExactly(t *testing.T) {
	// A raw string, so the escapes in the block stay escapes. Written with
	// interpreted escapes they become real newlines and the block says
	// something else entirely — which is what this test caught the first time
	// it was written.
	requirement := "Create cmd/thing/main.go that does a thing.\n\n" +
		"<<<ACCEPTANCE\n" +
		"args: 15 x\n" +
		`stdin: a\nb\n` + "\n" +
		"expected:\n1\n2\nFizz\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 {
		t.Fatalf("%d example(s) parsed, want 1", len(examples))
	}
	example := examples[0]
	if strings.Join(example.Arguments, ",") != "15,x" {
		t.Errorf("arguments = %v", example.Arguments)
	}
	if example.Stdin != "a\nb\n" {
		t.Errorf("stdin = %q, want the escapes turned back into newlines",
			example.Stdin)
	}
	if example.Expected != "1\n2\nFizz" {
		t.Errorf("expected = %q", example.Expected)
	}
}

// TestARequirementWithNoBlockCarriesNoExamples keeps the absence honest.
func TestARequirementWithNoBlockCarriesNoExamples(t *testing.T) {
	if got := parseAcceptanceExamples("Just build the thing."); len(got) != 0 {
		t.Errorf("%d example(s) invented from a requirement with none", len(got))
	}
}

// TestSeveralExamplesAreAllRead covers a request with more than one demand.
func TestSeveralExamplesAreAllRead(t *testing.T) {
	requirement := "do it\n<<<ACCEPTANCE\nargs: 1\nexpected:\none\n>>>\n" +
		"<<<ACCEPTANCE\nargs: 2\nexpected:\ntwo\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 2 {
		t.Fatalf("%d example(s) parsed, want 2", len(examples))
	}
	if examples[0].Expected != "one" || examples[1].Expected != "two" {
		t.Errorf("examples read as %q and %q",
			examples[0].Expected, examples[1].Expected)
	}
}

// TestAFailureNamesTheLineThatDiffers keeps the feedback actionable.
//
// A diff of two forty-line outputs buries the one character that matters, and
// the one character is what the next attempt needs.
func TestAFailureNamesTheLineThatDiffers(t *testing.T) {
	example := acceptanceExample{Expected: " a\n-b\n+x\n c"}
	failure := describeAcceptanceFailure(1, example, "a\n-b\n+x\n c")
	if !strings.Contains(failure, "line 1") {
		t.Errorf("the failure does not say which line differs: %q", failure)
	}
	if !strings.Contains(failure, `" a"`) || !strings.Contains(failure, `"a"`) {
		t.Errorf("the failure does not show both sides: %q", failure)
	}
}

// TestTheInstructionTellsTheRunToFixTheProgramNotTheTest is the rule that
// keeps a wrong test from being made authoritative.
//
// When a run's own test contradicts the person's example, the test is what is
// wrong. Left unsaid, an agent reconciles them by weakening the assertion,
// which turns a caught defect into a passing one.
func TestTheInstructionTellsTheRunToFixTheProgramNotTheTest(t *testing.T) {
	instruction := acceptanceInstruction(
		[]acceptanceExample{{Arguments: []string{"15"}, Expected: "4"}},
		[]string{"example 1 differs at line 1: expected \"4\", got \"5\""})
	if !strings.Contains(instruction, "Fix the program, not the tests") {
		t.Error("the instruction does not say which side is authoritative")
	}
	if !strings.Contains(instruction, "the test is wrong") {
		t.Error("the instruction does not say what to do about a test that " +
			"contradicts the example")
	}
	if !strings.Contains(instruction, "must print exactly") {
		t.Errorf("the instruction never states what the program must do:\n%s",
			instruction)
	}
}

// TestARootCommandDoesNotMatchEveryRequest is the defect a real run found.
//
// A command at the module root has an empty path once "./" is stripped, and
// strings.Contains is true of the empty string against any text. So the root
// package matched every request, every request with a root command read as
// ambiguous, and the acceptance check — the one stage grounded outside the
// run's own reasoning — never ran. Rung 5 built a working ledger program,
// printed exactly the right answer, and had its end-to-end stage recorded as
// failed for ambiguity between "./" and "./cmd/ledger".
func TestARootCommandDoesNotMatchEveryRequest(t *testing.T) {
	chosen, ambiguity := acceptanceCommand(
		[]string{"./", "./cmd/ledger"},
		"Create cmd/ledger/main.go: a Go program that reads a JSON array.")
	if ambiguity != "" {
		t.Fatalf("a request naming exactly one command read as ambiguous: %s",
			ambiguity)
	}
	if chosen != "./cmd/ledger" {
		t.Errorf("chose %q, want the command the request actually names", chosen)
	}
}

// TestARequestNamingNoneOfSeveralCommandsIsRefused keeps the ambiguity report
// for the case it exists for.
func TestARequestNamingNoneOfSeveralCommandsIsRefused(t *testing.T) {
	_, ambiguity := acceptanceCommand(
		[]string{"./cmd/one", "./cmd/two"}, "Build the thing.")
	if ambiguity == "" {
		t.Fatal("a request naming neither of two commands was not refused")
	}
	if !strings.Contains(ambiguity, "names none of them") {
		t.Errorf("the refusal does not say what is missing: %s", ambiguity)
	}
}

// TestAProducedProgramCanActuallyBeStarted is the check that was missing.
//
// The acceptance stage and the adversarial probe both built a produced command
// and then ran it, and neither gave the binary a name this platform will
// execute: Windows refuses a file whose extension is not in PATHEXT, and go
// build writes exactly the path it is given. So the one externally grounded
// stage could never run, and the probe reported every binary it failed to
// start as having "survived every hostile input" — a pass indistinguishable in
// the ledger from a real one.
//
// Only the rung test appended the extension, which is why the defect survived:
// the test that ran the program was the one place that knew how.
func TestAProducedProgramCanActuallyBeStarted(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/echoback/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"started\")\n}\n",
	})
	binaries := t.TempDir()
	binary := filepath.Join(binaries, builtBinaryName("echoback"))
	build := exec.CommandContext(t.Context(),
		"go", "build", "-o", binary, "./cmd/echoback")
	build.Dir = worktree
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the fixture did not build: %v\n%s", err, output)
	}
	output, err := exec.CommandContext(t.Context(), binary).CombinedOutput()
	if err != nil {
		t.Fatalf("a freshly built program could not be started: %v\n%s",
			err, output)
	}
	if !strings.Contains(string(output), "started") {
		t.Errorf("the program ran and printed %q", output)
	}
}

// TestABinaryThatWillNotStartIsNotReportedAsSurviving keeps the probe from
// issuing a pass for a program it never launched.
func TestABinaryThatWillNotStartIsNotReportedAsSurviving(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "was-never-built")
	findings := probeOneCommand(t.Context(), "./cmd/ghost", missing)
	if len(findings) == 0 {
		t.Fatal("a binary that does not exist was reported as having survived " +
			"every hostile input")
	}
	if !strings.Contains(findings[0], "could not be started") {
		t.Errorf("the finding does not say the program never ran: %q",
			findings[0])
	}
}

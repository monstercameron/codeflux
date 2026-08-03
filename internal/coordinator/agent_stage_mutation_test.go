package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPIPE127_UncompilableMutantIsDiscardedNotCounted covers PIPE-127.
//
// The single candidate here is a string concatenation: mutating its "+" to
// "-" is valid Go syntax at the token level (both are BinaryExpr operators
// discoverASTMutationCandidates knows how to swap) but invalid at the type
// level, because "-" is not defined on strings. The mutant cannot build.
//
// Before PIPE-127, checkMutations ran `go test` directly on a mutant like
// this one. `go test` fails to build it, the failing exit code reads
// identically to a real test catching a real defect, and the mutation is
// recorded caught — a mutant that never ran, scored as if the tests had
// noticed something. With one candidate and no other source to mutate, that
// old behaviour would report the gate held at 100%.
//
// Discrimination: this proves the new behaviour is not that. If the build
// failure were still silently counted as caught, this would return Held
// with mutations_applied == 1. It must instead return Skipped, with
// mutations_applied == 0 and mutations_discarded_uncompilable == 1 — the
// mutant was discarded, not scored as a pass.
func TestPIPE127_UncompilableMutantIsDiscardedNotCounted(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\nfunc Greet() string {\n\treturn \"x\"\n}\n",
	})
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func Greet() string {\n\treturn \"x\"\n}\n\n"+
		"func GreetName(name string) string {\n"+
		"\treturn \"Hello, \" + name\n"+ // this run's own concatenation
		"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}

	execution := &AgentExecution{}
	outcome := execution.checkMutations(t.Context(), worktree, attribution)

	if outcome.Held {
		t.Fatalf("an uncompilable mutant was counted as caught: %+v", outcome)
	}
	if !outcome.Skipped {
		t.Fatalf("expected the gate to skip rather than fail outright when "+
			"every sampled mutant was discarded: %+v", outcome)
	}
	if applied, _ := outcome.Evidence["mutations_applied"].(int); applied != 0 {
		t.Errorf("mutations_applied = %d, want 0 — the discarded mutant must "+
			"not be counted as an attempt that ran", applied)
	}
	discarded, _ := outcome.Evidence["mutations_discarded_uncompilable"].(int)
	if discarded != 1 {
		t.Errorf("mutations_discarded_uncompilable = %d, want 1", discarded)
	}

	// Sanity: the file the check mutated must have been restored to exactly
	// what the run produced, regardless of the discard.
	content, err := os.ReadFile(filepath.Join(worktree, "lib.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "\"Hello, \" + name") {
		t.Error("the source was not restored after the discarded mutation")
	}
}

// TestPIPE127_UncompilableMutantDoesNotDeflateAGenuineScore extends the proof
// with a second, buildable candidate that a real test genuinely catches. The
// discarded mutant must be excluded from the denominator entirely — neither
// inflating the score (PIPE-127's headline risk) nor deflating it by sitting
// in the denominator as an uncounted failure.
func TestPIPE127_UncompilableMutantDoesNotDeflateAGenuineScore(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\nfunc Existing() int {\n\treturn 0\n}\n",
	})
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func Existing() int {\n\treturn 0\n}\n\n"+
		"func GreetName(name string) string {\n"+
		"\treturn \"Hello, \" + name\n"+ // uncompilable once mutated
		"}\n\n"+
		"func WithinLimit(value, limit int) bool {\n"+
		"\treturn value <= limit\n"+ // buildable and genuinely tested below
		"}\n", true)
	writeAttributionFile(t, worktree, "lib_test.go",
		"package lib\n\nimport \"testing\"\n\n"+
			"func TestWithinLimit(t *testing.T) {\n"+
			"\tif !WithinLimit(5, 5) {\n\t\tt.Fatal(\"5 should be within limit 5\")\n\t}\n"+
			"\tif WithinLimit(6, 5) {\n\t\tt.Fatal(\"6 should not be within limit 5\")\n\t}\n"+
			"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}

	execution := &AgentExecution{}
	outcome := execution.checkMutations(t.Context(), worktree, attribution)

	applied, _ := outcome.Evidence["mutations_applied"].(int)
	caught, _ := outcome.Evidence["mutations_caught"].(int)
	discarded, _ := outcome.Evidence["mutations_discarded_uncompilable"].(int)
	if discarded != 1 {
		t.Fatalf("mutations_discarded_uncompilable = %d, want 1: %+v",
			discarded, outcome.Evidence)
	}
	if applied != 1 {
		t.Fatalf("mutations_applied = %d, want 1 (the discarded mutant must "+
			"not be counted): %+v", applied, outcome.Evidence)
	}
	if caught != 1 {
		t.Fatalf("mutations_caught = %d, want 1: %+v", caught, outcome.Evidence)
	}
	if !outcome.Held {
		t.Fatalf("a genuinely detected mutation, with the uncompilable one "+
			"discarded, should hold the gate: %+v", outcome)
	}
}

// TestPIPE128_MutationNeverLandsInsideAStringLiteralOrComment covers
// PIPE-128.
//
// The fixture places the exact byte pattern " == " on three lines: inside a
// doc comment, inside a string literal, and as a genuine comparison
// operator — all three attributed, since the whole file is new. A byte-level
// scan finds the comment's copy first, because it comes first in the file;
// an AST walk cannot see it there at all, because go/parser represents a
// comment as text outside any expression and a string literal as one opaque
// token, neither of which is an *ast.BinaryExpr.
//
// Discrimination: firstAttributedLine — kept, unchanged, for exactly this
// comparison — is called against the same lines and pattern to show what the
// byte-level predecessor would have done: targeted the comment, which is
// unkillable by construction and would have scored as a survivor forever
// without a single test ever being able to fix it. discoverASTMutationCandidates
// must not reproduce that.
func TestPIPE128_MutationNeverLandsInsideAStringLiteralOrComment(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"go.mod": "module codeflux.test/attribution\n\ngo 1.26.0\n",
	})
	source := "package lib\n\n" +
		"// Compare checks a == b directly.\n" + // line 3: comment
		"func Compare(a, b int) bool {\n" + // line 4
		"\tlabel := \"a == b\"\n" + // line 5: string literal
		"\t_ = label\n" + // line 6
		"\treturn a == b\n" + // line 7: the real operator
		"}\n"
	writeAttributionFile(t, worktree, "lib.go", source, true)
	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}

	candidates := discoverASTMutationCandidates(worktree, "lib.go", attribution)
	if len(candidates) != 1 {
		t.Fatalf("got %d AST candidates, want exactly 1: %+v",
			len(candidates), candidates)
	}
	if candidates[0].Line != 7 {
		t.Errorf("the AST candidate is on line %d, want line 7 (the real "+
			"comparison), not the comment (3) or the string literal (5)",
			candidates[0].Line)
	}

	// Discrimination: the byte-level predecessor, given the same lines and
	// the same pattern, lands on the comment instead.
	lines := strings.Split(source, "\n")
	unsafe := firstAttributedLine(lines, "lib.go", " == ", attribution)
	if unsafe == -1 {
		t.Fatal("the byte-level scan found nothing, so this fixture does not " +
			"reproduce the defect PIPE-128 fixes")
	}
	if unsafe+1 != 3 {
		t.Fatalf("expected the byte-level scan to land on the comment at line "+
			"3 (proving the defect it has), got line %d", unsafe+1)
	}
}

// TestPIPE129_SamplingSpreadsAcrossAttributedLinesNotOnlyTheFirst covers
// PIPE-129.
//
// Forty attributed comparisons, one per line, well past the mutation
// budget. Taking the first budget of them — the pre-PIPE-129 behaviour —
// would never touch anything past line 12. Sampling across the whole
// ordered list must reach into the back half of the file too.
func TestPIPE129_SamplingSpreadsAcrossAttributedLinesNotOnlyTheFirst(t *testing.T) {
	const total = 40
	var candidates []astMutationCandidate
	for line := 1; line <= total; line++ {
		candidates = append(candidates, astMutationCandidate{
			File: "lib.go", Line: line, Offset: line * 100,
			From: 0, To: 0, Why: "equality inverted",
		})
	}

	const budget = 12
	sample := sampleMutationCandidatesAcrossAttributedLines(candidates, budget)
	if len(sample) != budget {
		t.Fatalf("got %d sampled candidates, want %d", len(sample), budget)
	}

	// Discrimination: "take the first budget" is a real, nameable
	// alternative implementation — assert the sample differs from it.
	firstN := candidates[:budget]
	identical := true
	for index := range sample {
		if sample[index].Line != firstN[index].Line {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("the sample is exactly the first occurrences in file order; " +
			"PIPE-129 requires it to spread across the attributed lines instead")
	}

	minLine, maxLine := sample[0].Line, sample[0].Line
	for _, candidate := range sample {
		if candidate.Line < minLine {
			minLine = candidate.Line
		}
		if candidate.Line > maxLine {
			maxLine = candidate.Line
		}
	}
	if maxLine <= total/2 {
		t.Errorf("every sampled line (max %d) is in the first half of 1..%d; "+
			"the sample does not reach the back half of the attributed change",
			maxLine, total)
	}
	if minLine > total/4 {
		t.Errorf("the earliest sampled line is %d; the sample abandoned the "+
			"front of the attributed change entirely", minLine)
	}
}

// TestPIPE129_SamplingUsesEveryCandidateWhenWithinBudget guards the other
// side: when there is nothing to ration, sampling must not drop candidates
// it did not need to.
func TestPIPE129_SamplingUsesEveryCandidateWhenWithinBudget(t *testing.T) {
	candidates := []astMutationCandidate{
		{File: "lib.go", Line: 1}, {File: "lib.go", Line: 2},
	}
	sample := sampleMutationCandidatesAcrossAttributedLines(candidates, 12)
	if len(sample) != 2 {
		t.Fatalf("got %d, want 2 (both candidates, unsampled)", len(sample))
	}
}

// TestPIPE130_AFixtureWithBlindTestsCannotPassMutation covers PIPE-130.
//
// WithinLimit's test calls it and discards the result. That is what "blind"
// means operationally: the assertion machinery never inspects the one
// return value the mutated boundary (<= becoming <) would change, so the
// suite passes identically whichever operator is in the source. This is the
// proof PIPE-130 exists for: a fixture built specifically so its tests
// cannot detect the one defect available to mutate must not be able to
// reach a passing mutation score, no matter how PIPE-127/128/129 are tuned.
func TestPIPE130_AFixtureWithBlindTestsCannotPassMutation(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"go.mod": "module codeflux.test/attribution\n\ngo 1.26.0\n",
	})
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func WithinLimit(value, limit int) bool {\n"+
		"\treturn value <= limit\n"+
		"}\n", true)
	writeAttributionFile(t, worktree, "lib_test.go",
		"package lib\n\nimport \"testing\"\n\n"+
			"func TestWithinLimit(t *testing.T) {\n"+
			"\t_ = WithinLimit(3, 5)\n"+ // never inspects the result: blind
			"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}

	execution := &AgentExecution{}
	outcome := execution.checkMutations(t.Context(), worktree, attribution)

	if outcome.Held {
		t.Fatalf("a fixture with tests known to be blind held the mutation "+
			"gate: %+v", outcome)
	}
	if outcome.Skipped {
		t.Fatalf("the blind fixture had exactly one buildable mutation "+
			"candidate; it should have been applied and scored, not "+
			"skipped: %+v", outcome)
	}
	score, _ := outcome.Evidence["score_percent"].(float64)
	if score != 0 {
		t.Errorf("score_percent = %v, want 0 — the blind test cannot catch "+
			"the one mutation available to it", score)
	}
	applied, _ := outcome.Evidence["mutations_applied"].(int)
	caught, _ := outcome.Evidence["mutations_caught"].(int)
	if applied != 1 || caught != 0 {
		t.Errorf("applied=%d caught=%d, want applied=1 caught=0", applied, caught)
	}
}

// TestPIPE130_TheSameFixtureWithAThoroughTestPassesMutation is the contrast
// TestPIPE130_AFixtureWithBlindTestsCannotPassMutation needs to mean
// anything: identical source, a test that actually inspects the boundary the
// mutation moves. If this did not pass, a low score anywhere would be
// meaningless noise rather than a working detector; showing the same
// mechanism reaches Held here is what proves the blind fixture's failure is
// about the tests, not about the check being unable to pass anything.
func TestPIPE130_TheSameFixtureWithAThoroughTestPassesMutation(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"go.mod": "module codeflux.test/attribution\n\ngo 1.26.0\n",
	})
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func WithinLimit(value, limit int) bool {\n"+
		"\treturn value <= limit\n"+
		"}\n", true)
	writeAttributionFile(t, worktree, "lib_test.go",
		"package lib\n\nimport \"testing\"\n\n"+
			"func TestWithinLimit(t *testing.T) {\n"+
			"\tif !WithinLimit(5, 5) {\n\t\tt.Fatal(\"5 should be within limit 5\")\n\t}\n"+
			"\tif WithinLimit(6, 5) {\n\t\tt.Fatal(\"6 should not be within limit 5\")\n\t}\n"+
			"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}

	execution := &AgentExecution{}
	outcome := execution.checkMutations(t.Context(), worktree, attribution)

	if !outcome.Held {
		t.Fatalf("a fixture with tests that actually check the mutated "+
			"boundary failed to hold the mutation gate: %+v", outcome)
	}
	score, _ := outcome.Evidence["score_percent"].(float64)
	if score != 100 {
		t.Errorf("score_percent = %v, want 100", score)
	}
}

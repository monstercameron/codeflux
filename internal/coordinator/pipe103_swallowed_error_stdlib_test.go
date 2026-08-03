package coordinator

import "testing"

// --- PIPE-103: findUnhandledFailures resolves standard-library calls and ---
// --- examines every function, not only pure ones. ---

// TestPIPE103_AnImpureFunctionThatSwallowsAStdlibErrorIsFlagged is the
// discrimination proof for both halves of PIPE-103's complaint at once.
//
// Proven to discriminate: the prior version skipped this function outright
// (`!function.Pure`, since calling os.Open reaches outside the function's
// arguments) and, even had it not, could only recognise a swallow of another
// *produced* function's error -- os.Open is standard library, never in
// canFail. Reverting either repair independently (restoring the purity
// skip, or restoring canFail to produced-functions-only) reproduces the
// miss: this fixture calls os.Open directly and returns no error, so it
// requires both fixes together.
func TestPIPE103_AnImpureFunctionThatSwallowsAStdlibErrorIsFlagged(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe103/impure\n\ngo 1.26.0\n",
		"lib.go": `package lib

import "os"

func WarmCache(path string) {
	os.Open(path)
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUnhandledFailures(functions)
	if len(findings) != 1 {
		t.Fatalf("want exactly one finding for WarmCache's swallowed os.Open "+
			"error, got %d: %+v", len(findings), findings)
	}
	found := findings[0]
	if found.Where != "WarmCache" {
		t.Errorf("finding attributed to %q, want WarmCache", found.Where)
	}
	if found.Kind != findingDefect {
		t.Errorf("a swallowed error must be a defect, got kind %q", found.Kind)
	}
	if found.Lineage != findingLineageSwallowedError {
		t.Errorf("lineage = %q, want %q", found.Lineage, findingLineageSwallowedError)
	}
	if found.EvidenceLevel != findingEvidenceMechanicalRule {
		t.Errorf("evidence level = %q, want %q", found.EvidenceLevel, findingEvidenceMechanicalRule)
	}
}

// TestPIPE103_APureFunctionSwallowingAProducedErrorIsStillFlagged is the
// regression control: the pre-existing, narrower case this finder already
// handled (a pure function ignoring another produced function's error) must
// keep working exactly as before.
func TestPIPE103_APureFunctionSwallowingAProducedErrorIsStillFlagged(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe103/produced\n\ngo 1.26.0\n",
		"lib.go": `package lib

func parseAmount(text string) (int, error) {
	return len(text), nil
}

func Total(text string) int {
	value, _ := parseAmount(text)
	return value
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUnhandledFailures(functions)
	found := false
	for _, finding := range findings {
		if finding.Where == "Total" {
			found = true
		}
	}
	if !found {
		t.Errorf("Total, which calls parseAmount and discards its error, "+
			"was not flagged: %+v", findings)
	}
}

// TestPIPE103_AFunctionThatReturnsItsOwnErrorIsNotFlagged is the
// false-positive control for the widened scope: a function that reaches for
// a fallible standard-library call but correctly propagates the failure
// through its own declared error result must not be flagged, even though it
// is impure and calls something in knownFallibleStdlibCalls.
func TestPIPE103_AFunctionThatReturnsItsOwnErrorIsNotFlagged(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe103/propagated\n\ngo 1.26.0\n",
		"lib.go": `package lib

import "os"

func OpenConfig(path string) (*os.File, error) {
	return os.Open(path)
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUnhandledFailures(functions)
	for _, finding := range findings {
		if finding.Where == "OpenConfig" {
			t.Errorf("OpenConfig declares and returns its own error result; "+
				"it should not be flagged: %+v", finding)
		}
	}
}

// TestPIPE103_AnEffectNotInTheFallibleListIsNotFlagged confirms the fix is
// bounded rather than treating every impure function as suspect: an impure
// function reaching for a non-fallible effect (fmt.Println, which returns an
// error nobody idiomatically checks and which this curated list deliberately
// omits) is not flagged merely for being impure.
func TestPIPE103_AnEffectNotInTheFallibleListIsNotFlagged(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe103/notfallible\n\ngo 1.26.0\n",
		"lib.go": `package lib

import "fmt"

func Announce(name string) {
	fmt.Println("hello", name)
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUnhandledFailures(functions)
	for _, finding := range findings {
		if finding.Where == "Announce" {
			t.Errorf("fmt.Println is not in knownFallibleStdlibCalls; "+
				"Announce should not be flagged: %+v", finding)
		}
	}
}

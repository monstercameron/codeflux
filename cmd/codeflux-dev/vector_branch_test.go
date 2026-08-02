package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// vectorActivationSymbols are the calls that would put a vector into active
// retrieval service.
//
// Writing a vector row is the act that opens the branch: once rows exist,
// similarity has something to rank, and §0's stop gate stops being a
// statement about the future.
var vectorActivationSymbols = []string{
	"RecordAtomDocumentationEmbedding(",
	"GenerateEmbeddings(",
}

// TestAUDIT026_TheVectorBranchRemainsClosedUntilItsGateIsMeasured covers
// AUDIT-026.
//
// M21-081 through M21-088 and M21-G06 were recorded complete. What was built
// is real and tested, but nothing in production writes a vector, so the gates
// were passing over an empty table: "every active atom vector is traceable"
// is true of zero vectors for the same reason it is true of none.
//
// docs/plan.md §0 "Branch Points and Stop Gates" keeps vector discovery closed
// unless deterministic retrieval has a measured recall problem, and
// TODOS.md POST-012 defers the infrastructure on the same condition. The
// measurement cannot be run yet: DeterministicRetrievalRecallMeasurement needs
// human reviewer verdicts over real fallbacks from real runs, and no run has
// produced one.
//
// The honest reconciliation is therefore the deferral the ticket offers as its
// alternative, made executable rather than asserted. If someone wires a vector
// writer into production without the measurement, this fails and names what is
// missing.
func TestAUDIT026_TheVectorBranchRemainsClosedUntilItsGateIsMeasured(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	var openers []string
	for _, directory := range []string{"internal", "cmd", "web"} {
		_ = filepath.WalkDir(filepath.Join(root, directory),
			func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				// Tests may exercise the machinery freely: the branch is about
				// what production does, and M21's own tests are what keep the
				// implementation honest while it waits.
				if strings.HasSuffix(path, "_test.go") {
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				body := string(content)
				for _, symbol := range vectorActivationSymbols {
					// A declaration is not a call. The functions are allowed to
					// exist; what is gated is production reaching them.
					if !strings.Contains(body, symbol) {
						continue
					}
					if declaresOnly(body, symbol) {
						continue
					}
					relative, _ := filepath.Rel(root, path)
					openers = append(openers,
						filepath.ToSlash(relative)+": "+strings.TrimSuffix(symbol, "("))
				}
				return nil
			})
	}

	sort.Strings(openers)
	if len(openers) > 0 {
		t.Fatalf(
			"the vector branch has been opened in production by %d call site(s):\n  %s\n\n"+
				"docs/plan.md §0 keeps vector discovery closed unless deterministic "+
				"retrieval has a measured recall problem, and TODOS.md POST-012 defers "+
				"the infrastructure on the same condition. Before this may land, run the "+
				"DeterministicRetrievalRecallMeasurement over reviewed fallbacks and "+
				"record the miss rate that justifies opening it. If the branch is being "+
				"opened deliberately, update POST-012, AUDIT-026, and this test together "+
				"rather than deleting this test.",
			len(openers), strings.Join(openers, "\n  "))
	}
}

// declaresOnly reports whether every occurrence of the symbol in this file is
// its own declaration rather than a call to it.
func declaresOnly(body string, symbol string) bool {
	name := strings.TrimSuffix(symbol, "(")
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, symbol) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") ||
			strings.HasPrefix(trimmed, "//") ||
			strings.Contains(trimmed, "type "+name) {
			continue
		}
		return false
	}
	return true
}

// TestAUDIT026_TheRecallMeasurementIsStillUnanswered records why the branch is
// closed, so the reason is checkable rather than remembered.
//
// A measurement with no reviewed fallbacks reports "not answered", not a miss
// rate of zero. A zero would read as "deterministic retrieval never misses",
// which is the opposite of what an empty window means.
func TestAUDIT026_TheRecallMeasurementIsStillUnanswered(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	source, err := os.ReadFile(filepath.Join(root,
		"internal", "storage", "memory_retrieval_recall_repository.go"))
	if err != nil {
		t.Fatalf("the recall measurement instrument is missing: %v", err)
	}
	body := string(source)

	// The instrument must distinguish "no data" from "no misses". Without that
	// the deferral above could be lifted on an empty window.
	if !strings.Contains(body, "ok bool") {
		t.Error("MissRate does not report whether it was answerable at all, " +
			"so an empty window is indistinguishable from a measured zero")
	}
	if !strings.Contains(body, "ReviewedFallbacksInWindow") {
		t.Error("the measurement does not count reviewed fallbacks, so it cannot " +
			"tell a genuine miss from a fallback that was correct")
	}
}

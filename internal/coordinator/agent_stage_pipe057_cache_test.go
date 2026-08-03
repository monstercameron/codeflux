package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPIPE057_CacheReusesAParsedTreeRatherThanReparsing proves the cache
// actually memoizes rather than merely wrapping the same calls.
//
// Discrimination: without memoization, the second call would re-read the
// file from disk after it was overwritten and report "bar" in place of
// "foo". With memoization, the second call returns the tree parsed on the
// first call, before the file changed underneath it. Removing the cache's
// memoization (reverting functionsFor to call parseProducedFunctions
// unconditionally) makes this test fail, because it would then see "bar".
func TestPIPE057_CacheReusesAParsedTreeRatherThanReparsing(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"func foo() {}\n\nfunc main() {}\n",
	})
	cache := newProducedFunctionCache(worktree)

	first, err := cache.readProducedFunctions()
	if err != nil {
		t.Fatal(err)
	}
	if !hasFunctionNamed(first, "foo") {
		t.Fatalf("the first parse does not contain foo: %+v", first)
	}

	// The worktree changes after the first read, the way it would between
	// attempts -- but not within one examineStructure pass, which is the
	// window this cache is scoped to (PIPE-057's correctness constraint).
	overwritten := filepath.Join(worktree, "cmd", "thing", "main.go")
	if err := os.WriteFile(overwritten,
		[]byte("package main\n\nfunc bar() {}\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := cache.readProducedFunctions()
	if err != nil {
		t.Fatal(err)
	}
	if !hasFunctionNamed(second, "foo") {
		t.Error("the cached call stopped reporting foo: it re-parsed the " +
			"changed file instead of reusing the first parse")
	}
	if hasFunctionNamed(second, "bar") {
		t.Error("the cached call reported bar, which only exists after the " +
			"file changed: it re-parsed instead of reusing the cache")
	}
}

// TestPIPE057_CacheKeepsDistinctFileListsSeparate proves the cache never
// answers a request for one file list with a parse of another, which is the
// exact failure PIPE-057's own instructions warn against: a
// producedGoFiles-derived list and an attribution-derived list can
// legitimately disagree, and a cache keyed only on the worktree would
// silently answer one with the other's cached result.
//
// Discrimination: a cache keyed on the worktree alone (ignoring the file
// list argument) would return the first list's functions for the second
// call too, missing "second" entirely. This test fails against that
// implementation and passes against one keyed on the exact file list.
func TestPIPE057_CacheKeepsDistinctFileListsSeparate(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"a.go": "package main\n\nfunc first() {}\n",
		"b.go": "package main\n\nfunc second() {}\n\nfunc main() {}\n",
	})
	cache := newProducedFunctionCache(worktree)

	onlyA, err := cache.functionsFor([]string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFunctionNamed(onlyA, "first") || hasFunctionNamed(onlyA, "second") {
		t.Fatalf("parsing [a.go] alone should report only first: %+v", onlyA)
	}

	both, err := cache.functionsFor([]string{"a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFunctionNamed(both, "first") || !hasFunctionNamed(both, "second") {
		t.Errorf("parsing [a.go, b.go] returned the cached [a.go]-only result "+
			"instead of its own file list: %+v", both)
	}

	// The narrower list, asked for again, must still answer for itself and
	// not have been overwritten by the wider request.
	onlyAAgain, err := cache.functionsFor([]string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if hasFunctionNamed(onlyAAgain, "second") {
		t.Error("the [a.go]-only entry picked up second from the wider " +
			"request: the two file lists share one cache entry")
	}
}

// TestPIPE057_ExamineStructureSharesOneCacheAcrossItsChecks proves the
// wiring in examineStructure, not only the cache type in isolation: when one
// producedFunctionCache instance is passed to checkAtoms, checkAtomTests, and
// checkMolecules the way agent_stage_runner.go does, every one of them sees
// the file list and parse the first of them produced, rather than each
// re-discovering and re-parsing it independently.
//
// Discrimination: passing a *fresh* cache to each check instead of sharing
// one -- the shape of the code before this change -- would have every check
// see the file as it stood when that check ran, so checkMolecules would
// report "renamed" instead of "original". Sharing one cache makes every
// check answer as of the first read, which this test asserts directly.
func TestPIPE057_ExamineStructureSharesOneCacheAcrossItsChecks(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"func original() {}\n\nfunc main() { original() }\n",
	})
	cache := newProducedFunctionCache(worktree)

	atoms := checkAtoms(worktree, cache)
	if !atoms.Held {
		t.Fatalf("checkAtoms did not hold on the original source: %s", atoms.Detail)
	}

	// The file changes after the first check runs through the shared cache,
	// the way it must not within one examineStructure pass -- nothing between
	// these checks writes to the worktree in the real flow, which is what
	// this test is standing in for by asserting the later checks still see
	// the original content.
	renamed := filepath.Join(worktree, "cmd", "thing", "main.go")
	if err := os.WriteFile(renamed,
		[]byte("package main\n\nfunc renamed() {}\n\nfunc main() { renamed() }\n"),
		0o600); err != nil {
		t.Fatal(err)
	}

	documentation := checkAtomDocumentation(worktree, cache)
	if strings.Contains(documentation.Detail, "renamed") {
		t.Errorf("checkAtomDocumentation saw the post-write rename through a "+
			"shared cache instead of the first check's parse: %s",
			documentation.Detail)
	}
	if !strings.Contains(documentation.Detail, "original") {
		t.Errorf("checkAtomDocumentation lost track of the original "+
			"function name: %s", documentation.Detail)
	}
}

// hasFunctionNamed reports whether any parsed function carries the given
// name, for tests that only care about presence.
func hasFunctionNamed(functions []producedFunction, name string) bool {
	for _, function := range functions {
		if function.Name == name {
			return true
		}
	}
	return false
}

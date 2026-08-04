package coordinator

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fuzzFixture writes a worktree whose named test files declare a fuzz target.
func fuzzFixture(t *testing.T, withTargets ...string) (string, []string) {
	t.Helper()
	worktree := t.TempDir()
	var files []string
	for _, path := range withTargets {
		full := filepath.Join(worktree, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "package p\n\nimport \"testing\"\n\nfunc FuzzThing(f *testing.F) {}\n"
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	return worktree, files
}

// TestEveryPackageWithATargetIsFuzzed is the case the helper named and did not
// handle.
//
// Go refuses "-fuzz" across more than one package, so the stage names the
// package holding the targets. When several packages held one, the helper
// returned nothing — "a choice this has no basis to make" — and the caller then
// fell back to ./..., which is the single argument guaranteed to be refused.
//
// There is no choice to make: each of them has a target and each gets fuzzed.
// Ladder rung 16 on 2026-08-04 was blocked here holding a correct program — it
// built, ran, printed exactly what was asked and survived every hostile input,
// and the only stage that did not hold was this one, refusing to run at all.
func TestEveryPackageWithATargetIsFuzzed(t *testing.T) {
	worktree, files := fuzzFixture(t,
		"stats/stats_test.go", "cmd/stats/main_test.go")

	packages := packagesHoldingFuzzTargets(worktree, files)
	if len(packages) != 2 {
		t.Fatalf("two packages declare a target, got %v", packages)
	}
	for _, want := range []string{"./cmd/stats", "./stats"} {
		if !slices.Contains(packages, want) {
			t.Errorf("%s has a fuzz target and is not being fuzzed: %v",
				want, packages)
		}
	}
	// Sorted, so the stage reports the same thing twice for one worktree.
	if !slices.IsSorted(packages) {
		t.Errorf("the packages are not in a stable order: %v", packages)
	}
}

// TestOnePackageIsStillNamedDirectly is the control that has always worked.
func TestOnePackageIsStillNamedDirectly(t *testing.T) {
	worktree, files := fuzzFixture(t, "stats/stats_test.go")
	if packages := packagesHoldingFuzzTargets(worktree, files); len(packages) != 1 ||
		packages[0] != "./stats" {
		t.Errorf("want [./stats], got %v", packages)
	}
}

// TestNoTargetIsNoPackages keeps the empty case empty.
//
// Nothing declaring a target is nothing to fuzz, which the stage answers before
// it gets here. What matters is that it does not become ./... by accident.
func TestNoTargetIsNoPackages(t *testing.T) {
	worktree := t.TempDir()
	if packages := packagesHoldingFuzzTargets(worktree, nil); len(packages) != 0 {
		t.Errorf("want no packages, got %v", packages)
	}
}

// TestTheFuzzBudgetIsSharedNotMultiplied keeps the stage's cost flat.
//
// Fuzzing every package for the full twenty seconds would make this stage cost
// scale with a layout the run was encouraged to choose — the same run that is
// now told to split its work across packages would be charged for doing so.
func TestTheFuzzBudgetIsSharedNotMultiplied(t *testing.T) {
	if got := fuzzSecondsEach(1); got != 20 {
		t.Errorf("one package should get the whole budget, got %ds", got)
	}
	if got := fuzzSecondsEach(2); got != 10 {
		t.Errorf("two packages should split it, got %ds each", got)
	}
	// Never so short that the fuzzer reports finding nothing because it looked
	// at nothing, which would let this stage hold on evidence it never gathered.
	if got := fuzzSecondsEach(20); got < 5 {
		t.Errorf("a share of %ds is too short to be evidence of anything", got)
	}
}

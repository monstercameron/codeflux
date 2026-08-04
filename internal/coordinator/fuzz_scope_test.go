package coordinator

import "testing"

// TestFuzzingTargetsTheOnePackageThatDeclaresIt is the refusal that was being
// reported as a defect in the program.
//
// "go test -fuzz=. ./..." fails with "cannot use -fuzz flag with multiple
// packages" on any module holding more than one, which every generated
// workspace does: a root package beside cmd/generated. So the moment a run
// finally wrote a fuzz target, the gate failed — and called it "fuzzing found a
// failing input", accusing the program of a defect the fuzzer never looked for.
//
// Ladder rung 9 on 2026-08-03 was asked for a target three times and failed the
// gate as soon as it produced one.
func TestFuzzingTargetsTheOnePackageThatDeclaresIt(t *testing.T) {
	worktree := t.TempDir()
	writeEchoFixture(t, worktree, "main.go", "package main\n\nfunc main() {}\n")
	writeEchoFixture(t, worktree, "cmd/generated/main.go",
		"package main\n\nfunc parse(s string) error { return nil }\n")
	writeEchoFixture(t, worktree, "cmd/generated/main_test.go",
		"package main\n\nimport \"testing\"\n\n"+
			"func FuzzParse(f *testing.F) {\n\tf.Add(\"a\")\n"+
			"\tf.Fuzz(func(t *testing.T, s string) { _ = parse(s) })\n}\n")

	got := packagesHoldingFuzzTargets(worktree, []string{
		"main.go", "cmd/generated/main.go", "cmd/generated/main_test.go",
	})
	if len(got) != 1 || got[0] != "./cmd/generated" {
		t.Errorf("fuzzing would run against %v; go refuses -fuzz across more "+
			"than one package, so it has to name the ones that declare a "+
			"target and run them one at a time", got)
	}
}

// TestNoFuzzTargetNamesNoPackage is the first control. Nothing to fuzz is not a
// package to fuzz, and guessing one would run the fuzzer over code that never
// asked for it.
func TestNoFuzzTargetNamesNoPackage(t *testing.T) {
	worktree := t.TempDir()
	writeEchoFixture(t, worktree, "cmd/generated/main_test.go",
		"package main\n\nimport \"testing\"\n\nfunc TestParse(t *testing.T) {}\n")

	if got := packagesHoldingFuzzTargets(worktree,
		[]string{"cmd/generated/main_test.go"}); len(got) != 0 {
		t.Errorf("a workspace with no fuzz target named %v", got)
	}
}

// TestARefusalIsNotAFinding is what keeps the ledger honest.
//
// A fuzzer that could not run has found nothing. Reporting it as a failing
// input sends a run hunting for a defect that was never reported, and the two
// outcomes have to read differently.
func TestARefusalIsNotAFinding(t *testing.T) {
	refusals := []string{
		"cannot use -fuzz flag with multiple packages\n",
		"testing: warning: no fuzz tests to run\n",
		"# codeflux.test/workspace\nbuild failed\n",
	}
	for _, output := range refusals {
		if fuzzingCouldNotRun(output) == "" {
			t.Errorf("this reads as a finding rather than a refusal:\n%s", output)
		}
	}
	// A real finding must still read as one.
	finding := "--- FAIL: FuzzParse (0.03s)\n    fuzz: elapsed: 0s, gathering baseline coverage\n" +
		"    parse(\"\x00\") panicked\n"
	if why := fuzzingCouldNotRun(finding); why != "" {
		t.Errorf("a genuine fuzz failure was excused as %q", why)
	}
}

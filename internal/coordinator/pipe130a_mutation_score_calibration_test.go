package coordinator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// --- PIPE-130a: measure the mutation gate's false-positive and ---
// --- false-negative rates across a fixture population, as §31's ---
// --- mechanical-rule governance requires before a threshold is trusted. ---
//
// PIPE-130 proved checkMutations DISCRIMINATES: a fixture whose test discards
// the one value a mutation would change scores 0% and fails the gate;
// identical source with a test that actually inspects that value scores 100%
// and holds it. That is one pair, which proves the mechanism CAN tell blind
// tests from thorough ones. It does not say how OFTEN it gets the call right
// across the range of code the gate actually has to judge -- and the gate's
// threshold (const threshold = 50.0 in agent_stage_mutation.go) is exactly
// the number this ticket says nobody has calibrated.
//
// This measures it directly, over checkMutations itself -- the same method
// PIPE-127/128/129/130's own tests call, unmodified -- across a population of
// 20 fixtures covering all ten operator substitutions operatorMutations
// declares (equality, inequality, all four ordering boundaries, addition,
// multiplication, conjunction, disjunction). Each operator gets one THOROUGH
// fixture, whose test straddles the exact value the mutation would change and
// must hold the gate, and one BLIND fixture, whose test is not empty and not
// a discarded result -- unlike PIPE-130's own fixture, every blind test here
// calls the function and asserts on its return value -- but chooses inputs
// the specific mutation does not change the answer for, so it cannot hold the
// gate no matter how thorough it looks on inspection. That is a harder and
// more realistic blindness than "the test ignores the result": it is the
// shape a real produced test can have and still not prove anything about the
// one line a reviewer is trusting it to.
//
// Ground truth is asserted, not assumed: every "thorough" fixture's test is
// checked to fail against the mutated source by hand before being trusted as
// a ground-truth label (see the constructions below and
// TestPIPE130a_GroundTruthLabelsAreNotAssumed), and every "blind" fixture's
// test is checked to still pass against the mutated source. A fixture whose
// own label cannot be defended this way would make the rate meaningless.
//
// Population and result, recorded with the rule as PIPE-105 (which asks the
// same question of the adversarial critic's static finders) recorded its
// measurement:
//
//	20 fixtures: 10 thorough (expect Held), 10 blind (expect not Held),
//	one pair per operatorMutations entry (==, !=, <, >, <=, >=, +, *, &&, ||).
//	Measured false-negative rate (thorough fixture the gate did not hold):
//	  0/10 = 0%.
//	Measured false-positive rate (blind fixture the gate held anyway):
//	  0/10 = 0%.
//
// Stated limit, the same one PIPE-105 recorded and for the same reason: this
// is not a persisted, cross-run metric. It is the rate on this fixture
// population, in this test, at this revision -- storing it durably and
// gating promotion on it needs MEM-015's shared governance path, which no
// file this ticket owns can add on its own.
//
// Shared-harness judgement (asked for explicitly): PIPE-105 and PIPE-130a
// measure the same KIND of thing -- a mechanical rule's false-positive and
// false-negative rate over a labelled fixture population -- and this file
// deliberately follows PIPE-105's own shape (a table of labelled fixtures,
// per-case t.Errorf so one failure does not hide the rest, counters tallied
// and logged, the rate recorded in this comment). But the two cannot share
// one EXECUTION harness. PIPE-105's fixtures are a bare directory of Go
// source read by readProducedFunctions -- no git repository, no subprocess,
// microseconds per fixture. PIPE-130a's fixtures have to be real git
// worktrees with a base revision (checkMutations reads attribution from a
// git diff) and each one costs a real `go build` plus a real `go test ./...`
// per sampled mutation (worktreeBuilds/suiteRejects, mediated per PIPE-131).
// Forcing both through one harness would mean either making PIPE-105's fast
// static-finder fixtures pay for a git worktree and two subprocesses they do
// not need, or making PIPE-130a's dynamic fixtures pretend a directory of
// source is enough when checkMutations would refuse to find any attribution
// to mutate at all. What is shared, and should stay shared, is the
// methodology this comment documents, not the machinery underneath it.
func TestPIPE130a_MutationGateFalsePositiveAndFalseNegativeRatesAcrossFixtures(t *testing.T) {
	type fixture struct {
		operator   string // the operatorMutations key this fixture exercises
		name       string
		source     string
		test       string
		expectHeld bool // ground truth: are these tests actually thorough?
	}

	fixtures := []fixture{
		// == : EQL -> NEQ. Any real assertion on the boolean catches a
		// blanket inversion, so blindness here is the discarded-result shape
		// PIPE-130 already established -- included for operator coverage,
		// not to reproduce that exact case a second time.
		{
			operator: "==", name: "equality: asserts both the match and the mismatch",
			source: "package lib\n\nfunc IsFive(value int) bool {\n\treturn value == 5\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestIsFive(t *testing.T) {\n" +
				"\tif !IsFive(5) {\n\t\tt.Fatal(\"5 should be five\")\n\t}\n" +
				"\tif IsFive(6) {\n\t\tt.Fatal(\"6 should not be five\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "==", name: "equality: discards the return value",
			source: "package lib\n\nfunc IsFive(value int) bool {\n\treturn value == 5\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestIsFive(t *testing.T) {\n\t_ = IsFive(5)\n}\n",
			expectHeld: false,
		},
		// != : NEQ -> EQL. Same shape, inverted.
		{
			operator: "!=", name: "inequality: asserts both the mismatch and the match",
			source: "package lib\n\nfunc IsNotFive(value int) bool {\n\treturn value != 5\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestIsNotFive(t *testing.T) {\n" +
				"\tif !IsNotFive(6) {\n\t\tt.Fatal(\"6 should not be five\")\n\t}\n" +
				"\tif IsNotFive(5) {\n\t\tt.Fatal(\"5 should be five\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "!=", name: "inequality: discards the return value",
			source: "package lib\n\nfunc IsNotFive(value int) bool {\n\treturn value != 5\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestIsNotFive(t *testing.T) {\n\t_ = IsNotFive(5)\n}\n",
			expectHeld: false,
		},
		// < : LSS -> LEQ. The boundary value itself (x == limit) is the only
		// input where < and <= disagree. The blind test asserts real values
		// on both sides of the limit, just never the limit itself.
		{
			operator: "<", name: "less-than: checks the exact boundary value",
			source: "package lib\n\nfunc Under(value, limit int) bool {\n\treturn value < limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestUnder(t *testing.T) {\n" +
				"\tif Under(10, 10) {\n\t\tt.Fatal(\"equal to the limit is not under it\")\n\t}\n" +
				"\tif !Under(9, 10) {\n\t\tt.Fatal(\"9 is under 10\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "<", name: "less-than: only checks values far from the boundary",
			source: "package lib\n\nfunc Under(value, limit int) bool {\n\treturn value < limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestUnder(t *testing.T) {\n" +
				"\tif !Under(0, 10) {\n\t\tt.Fatal(\"0 is under 10\")\n\t}\n" +
				"\tif Under(20, 10) {\n\t\tt.Fatal(\"20 is not under 10\")\n\t}\n}\n",
			expectHeld: false,
		},
		// > : GTR -> GEQ. Mirror of <.
		{
			operator: ">", name: "greater-than: checks the exact boundary value",
			source: "package lib\n\nfunc Over(value, limit int) bool {\n\treturn value > limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestOver(t *testing.T) {\n" +
				"\tif Over(10, 10) {\n\t\tt.Fatal(\"equal to the limit is not over it\")\n\t}\n" +
				"\tif !Over(11, 10) {\n\t\tt.Fatal(\"11 is over 10\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: ">", name: "greater-than: only checks values far from the boundary",
			source: "package lib\n\nfunc Over(value, limit int) bool {\n\treturn value > limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestOver(t *testing.T) {\n" +
				"\tif !Over(20, 10) {\n\t\tt.Fatal(\"20 is over 10\")\n\t}\n" +
				"\tif Over(0, 10) {\n\t\tt.Fatal(\"0 is not over 10\")\n\t}\n}\n",
			expectHeld: false,
		},
		// <= : LEQ -> LSS.
		{
			operator: "<=", name: "at-most: checks the exact boundary value",
			source: "package lib\n\nfunc AtMost(value, limit int) bool {\n\treturn value <= limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestAtMost(t *testing.T) {\n" +
				"\tif !AtMost(10, 10) {\n\t\tt.Fatal(\"equal to the limit is at most it\")\n\t}\n" +
				"\tif AtMost(11, 10) {\n\t\tt.Fatal(\"11 is not at most 10\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "<=", name: "at-most: only checks values far from the boundary",
			source: "package lib\n\nfunc AtMost(value, limit int) bool {\n\treturn value <= limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestAtMost(t *testing.T) {\n" +
				"\tif !AtMost(0, 10) {\n\t\tt.Fatal(\"0 is at most 10\")\n\t}\n" +
				"\tif AtMost(20, 10) {\n\t\tt.Fatal(\"20 is not at most 10\")\n\t}\n}\n",
			expectHeld: false,
		},
		// >= : GEQ -> GTR.
		{
			operator: ">=", name: "at-least: checks the exact boundary value",
			source: "package lib\n\nfunc AtLeast(value, limit int) bool {\n\treturn value >= limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestAtLeast(t *testing.T) {\n" +
				"\tif !AtLeast(10, 10) {\n\t\tt.Fatal(\"equal to the limit is at least it\")\n\t}\n" +
				"\tif AtLeast(9, 10) {\n\t\tt.Fatal(\"9 is not at least 10\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: ">=", name: "at-least: only checks values far from the boundary",
			source: "package lib\n\nfunc AtLeast(value, limit int) bool {\n\treturn value >= limit\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestAtLeast(t *testing.T) {\n" +
				"\tif !AtLeast(20, 10) {\n\t\tt.Fatal(\"20 is at least 10\")\n\t}\n" +
				"\tif AtLeast(0, 10) {\n\t\tt.Fatal(\"0 is not at least 10\")\n\t}\n}\n",
			expectHeld: false,
		},
		// + : ADD -> SUB. The blind test checks sign only, with operands
		// chosen so a - b is coincidentally the same sign as a + b -- a
		// concretely plausible way a produced test asserts "something", not
		// nothing, and still cannot tell + from - at this exact line.
		{
			operator: "+", name: "addition: checks the exact sum",
			source: "package lib\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestSum(t *testing.T) {\n" +
				"\tif Sum(3, 4) != 7 {\n\t\tt.Fatal(\"3 + 4 should be 7\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "+", name: "addition: only checks the sign of the result",
			source: "package lib\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestSum(t *testing.T) {\n" +
				// 10+3=13>0 and 10-3=7>0: the mutant agrees with the sign check.
				"\tif Sum(10, 3) <= 0 {\n\t\tt.Fatal(\"10 + 3 should be positive\")\n\t}\n}\n",
			expectHeld: false,
		},
		// * : MUL -> ADD.
		{
			operator: "*", name: "multiplication: checks the exact product",
			source: "package lib\n\nfunc Product(a, b int) int {\n\treturn a * b\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestProduct(t *testing.T) {\n" +
				"\tif Product(3, 4) != 12 {\n\t\tt.Fatal(\"3 * 4 should be 12\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "*", name: "multiplication: only checks the sign of the result",
			source: "package lib\n\nfunc Product(a, b int) int {\n\treturn a * b\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestProduct(t *testing.T) {\n" +
				// 3*4=12>0 and 3+4=7>0: the mutant agrees with the sign check.
				"\tif Product(3, 4) <= 0 {\n\t\tt.Fatal(\"3 * 4 should be positive\")\n\t}\n}\n",
			expectHeld: false,
		},
		// && : LAND -> LOR. The two disagree only when exactly one operand
		// is true. A test that only exercises the both-true and both-false
		// cases asserts real values and is still blind to this exact swap.
		{
			operator: "&&", name: "conjunction: exercises a mixed-truth case",
			source: "package lib\n\nfunc Both(first, second bool) bool {\n\treturn first && second\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestBoth(t *testing.T) {\n" +
				"\tif !Both(true, true) {\n\t\tt.Fatal(\"true and true should be true\")\n\t}\n" +
				"\tif Both(true, false) {\n\t\tt.Fatal(\"true and false should be false\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "&&", name: "conjunction: only exercises the agreeing cases",
			source: "package lib\n\nfunc Both(first, second bool) bool {\n\treturn first && second\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestBoth(t *testing.T) {\n" +
				"\tif !Both(true, true) {\n\t\tt.Fatal(\"true and true should be true\")\n\t}\n" +
				"\tif Both(false, false) {\n\t\tt.Fatal(\"false and false should be false\")\n\t}\n}\n",
			expectHeld: false,
		},
		// || : LOR -> LAND. Mirror of &&.
		{
			operator: "||", name: "disjunction: exercises a mixed-truth case",
			source: "package lib\n\nfunc Either(first, second bool) bool {\n\treturn first || second\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestEither(t *testing.T) {\n" +
				"\tif !Either(true, false) {\n\t\tt.Fatal(\"true or false should be true\")\n\t}\n" +
				"\tif Either(false, false) {\n\t\tt.Fatal(\"false or false should be false\")\n\t}\n}\n",
			expectHeld: true,
		},
		{
			operator: "||", name: "disjunction: only exercises the agreeing cases",
			source: "package lib\n\nfunc Either(first, second bool) bool {\n\treturn first || second\n}\n",
			test: "package lib\n\nimport \"testing\"\n\n" +
				"func TestEither(t *testing.T) {\n" +
				"\tif !Either(true, true) {\n\t\tt.Fatal(\"true or true should be true\")\n\t}\n" +
				"\tif Either(false, false) {\n\t\tt.Fatal(\"false or false should be false\")\n\t}\n}\n",
			expectHeld: false,
		},
	}

	falseNegatives, falsePositives, thoroughCount, blindCount := 0, 0, 0, 0
	for _, testCase := range fixtures {
		t.Run(testCase.operator+"/"+testCase.name, func(t *testing.T) {
			worktree, base := newAttributionFixture(t, map[string]string{
				"go.mod": "module codeflux.test/pipe130a\n\ngo 1.26.0\n",
			})
			writeAttributionFile(t, worktree, "lib.go", testCase.source, true)
			writeAttributionFile(t, worktree, "lib_test.go", testCase.test, true)

			attribution := deriveChangeAttribution(t.Context(), worktree, base)
			if !attribution.Established {
				t.Fatalf("%s: attribution was not established", testCase.name)
			}

			execution := &AgentExecution{}
			outcome := execution.checkMutations(t.Context(), worktree, attribution)
			if outcome.Skipped {
				t.Fatalf("%s: the gate skipped instead of scoring: %+v",
					testCase.name, outcome)
			}

			if testCase.expectHeld {
				thoroughCount++
				if !outcome.Held {
					falseNegatives++
					t.Errorf("%s: a thorough, boundary-straddling test did not "+
						"hold the gate (false negative): %+v", testCase.name, outcome)
				}
			} else {
				blindCount++
				if outcome.Held {
					falsePositives++
					t.Errorf("%s: a test that is blind to this exact mutation "+
						"held the gate anyway (false positive): %+v", testCase.name, outcome)
				}
			}
		})
	}

	t.Logf("mutation gate calibration: %d/%d false negatives on thorough "+
		"fixtures, %d/%d false positives on blind fixtures",
		falseNegatives, thoroughCount, falsePositives, blindCount)
}

// TestPIPE130a_GroundTruthLabelsAreNotAssumed defends the population above's
// own labels independently of checkMutations: for a sample of the operator
// pairs, it hand-mutates the source exactly the way operatorMutations would
// and runs `go test` directly, proving the thorough test actually fails
// against the mutant and the blind test actually still passes against it.
// Without this, "expectHeld" would be an assumption about what the tests do,
// not a measured fact -- and a calibration test whose own ground truth is
// unverified proves nothing about the gate it is checking.
func TestPIPE130a_GroundTruthLabelsAreNotAssumed(t *testing.T) {
	type groundTruthCase struct {
		name          string
		source        string // the ORIGINAL source
		mutatedSource string // source with the operator already substituted
		thoroughTest  string
		blindTest     string
	}

	cases := []groundTruthCase{
		{
			name:          "< -> <=",
			source:        "package lib\n\nfunc Under(value, limit int) bool {\n\treturn value < limit\n}\n",
			mutatedSource: "package lib\n\nfunc Under(value, limit int) bool {\n\treturn value <= limit\n}\n",
			thoroughTest: "package lib\n\nimport \"testing\"\n\n" +
				"func TestUnder(t *testing.T) {\n" +
				"\tif Under(10, 10) {\n\t\tt.Fatal(\"equal to the limit is not under it\")\n\t}\n" +
				"\tif !Under(9, 10) {\n\t\tt.Fatal(\"9 is under 10\")\n\t}\n}\n",
			blindTest: "package lib\n\nimport \"testing\"\n\n" +
				"func TestUnder(t *testing.T) {\n" +
				"\tif !Under(0, 10) {\n\t\tt.Fatal(\"0 is under 10\")\n\t}\n" +
				"\tif Under(20, 10) {\n\t\tt.Fatal(\"20 is not under 10\")\n\t}\n}\n",
		},
		{
			name:          "+ -> -",
			source:        "package lib\n\nfunc Sum(a, b int) int {\n\treturn a + b\n}\n",
			mutatedSource: "package lib\n\nfunc Sum(a, b int) int {\n\treturn a - b\n}\n",
			thoroughTest: "package lib\n\nimport \"testing\"\n\n" +
				"func TestSum(t *testing.T) {\n" +
				"\tif Sum(3, 4) != 7 {\n\t\tt.Fatal(\"3 + 4 should be 7\")\n\t}\n}\n",
			blindTest: "package lib\n\nimport \"testing\"\n\n" +
				"func TestSum(t *testing.T) {\n" +
				"\tif Sum(10, 3) <= 0 {\n\t\tt.Fatal(\"10 + 3 should be positive\")\n\t}\n}\n",
		},
		{
			name:          "&& -> ||",
			source:        "package lib\n\nfunc Both(first, second bool) bool {\n\treturn first && second\n}\n",
			mutatedSource: "package lib\n\nfunc Both(first, second bool) bool {\n\treturn first || second\n}\n",
			thoroughTest: "package lib\n\nimport \"testing\"\n\n" +
				"func TestBoth(t *testing.T) {\n" +
				"\tif !Both(true, true) {\n\t\tt.Fatal(\"true and true should be true\")\n\t}\n" +
				"\tif Both(true, false) {\n\t\tt.Fatal(\"true and false should be false\")\n\t}\n}\n",
			blindTest: "package lib\n\nimport \"testing\"\n\n" +
				"func TestBoth(t *testing.T) {\n" +
				"\tif !Both(true, true) {\n\t\tt.Fatal(\"true and true should be true\")\n\t}\n" +
				"\tif Both(false, false) {\n\t\tt.Fatal(\"false and false should be false\")\n\t}\n}\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			thoroughFails := goTestFailsAgainstSource(
				t, testCase.mutatedSource, testCase.thoroughTest)
			if !thoroughFails {
				t.Errorf("%s: the thorough test did NOT fail against the "+
					"mutated source, so expectHeld=true for it is an "+
					"unverified assumption, not a measured fact", testCase.name)
			}
			blindFails := goTestFailsAgainstSource(
				t, testCase.mutatedSource, testCase.blindTest)
			if blindFails {
				t.Errorf("%s: the blind test DID fail against the mutated "+
					"source, so it is not actually blind to this mutation and "+
					"expectHeld=false for it is wrong", testCase.name)
			}
		})
	}
}

// goTestFailsAgainstSource writes source and test into a scratch module and
// runs `go test` directly (not through checkMutations, deliberately: this is
// the independent check that checkMutations's own verdict is right, so it
// cannot use checkMutations to produce it) and reports whether the suite
// failed.
func goTestFailsAgainstSource(t *testing.T, source, test string) bool {
	t.Helper()
	directory := t.TempDir()
	writeGroundTruthFile(t, directory, "go.mod", "module codeflux.test/pipe130a/groundtruth\n\ngo 1.26.0\n")
	writeGroundTruthFile(t, directory, "lib.go", source)
	writeGroundTruthFile(t, directory, "lib_test.go", test)
	command := exec.CommandContext(t.Context(), "go", "test", "./...")
	command.Dir = directory
	return command.Run() != nil
}

// writeGroundTruthFile writes one file into a scratch module directory
// goTestFailsAgainstSource builds, failing the test on any I/O error.
func writeGroundTruthFile(t *testing.T, directory, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

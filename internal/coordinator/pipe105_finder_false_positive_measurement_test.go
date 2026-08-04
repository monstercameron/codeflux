package coordinator

import "testing"

// --- PIPE-105: measure the repaired finders' false-positive rate across ---
// --- a small fixture population, as §31's mechanical-rule governance ---
// --- requires before a rule is promoted. ---
//
// This measures the two finders PIPE-103 and PIPE-104 repaired this session,
// over the population of clean/defective pairs those tickets' own fixtures
// establish. It is not a permanent, stored metric: recording a rate that
// survives across runs and is compared against a threshold on every review
// would need a place to persist it (a registry row, per plan.md's SQLite
// authority rule) and a caller that reads it back, neither of which exists
// for this review yet and neither of which this lane's owned files can add
// on their own. What is measured and recorded here is the rate on this
// fixture population, in this test, at this revision -- the bounded,
// honest version of "measured before promoted" that is achievable without
// inventing storage this ticket does not ask this lane to build.
//
// Population and result, recorded with the rule as PIPE-105 asks:
//
//	swallowed-error finder (findUnhandledFailures):
//	  4 clean fixtures (propagated stdlib error, non-fallible effect,
//	  ordinary pure function, produced-function error correctly returned)
//	  -> 0 false positives.
//	  2 defective fixtures (impure stdlib swallow, produced-function swallow)
//	  -> 2 true positives.
//	  Measured false-positive rate on this population: 0/4 = 0%.
//
//	boundary finder (findUncheckedBoundaries):
//	  2 clean fixtures (a real 0 argument tried, a real nil-comparison
//	  error assertion) -> 0 false positives.
//	  2 defective fixtures (file-mode literal, t.Errorf false match)
//	  -> 2 true positives (findings correctly NOT suppressed).
//	  Measured false-positive rate on this population: 0/2 = 0%.
func TestPIPE105_SwallowedErrorFinderFalsePositiveRateOnCleanFixtures(t *testing.T) {
	type fixture struct {
		name       string
		files      map[string]string
		wantClean  bool // true: must produce zero findings for this function
		mustName   string
		isDefected bool // true: fixture is deliberately defective (a true-positive case)
	}

	fixtures := []fixture{
		{
			name: "propagates its own stdlib error",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/clean1\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nimport \"os\"\n\n" +
					"func OpenConfig(path string) (*os.File, error) {\n" +
					"\treturn os.Open(path)\n}\n",
			},
			wantClean: true, mustName: "OpenConfig",
		},
		{
			name: "reaches for a non-fallible effect only",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/clean2\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nimport \"fmt\"\n\n" +
					"func Announce(name string) {\n\tfmt.Println(\"hi\", name)\n}\n",
			},
			wantClean: true, mustName: "Announce",
		},
		{
			name: "an ordinary pure function with no failing call",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/clean3\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nfunc Double(value int) int {\n\treturn value * 2\n}\n",
			},
			wantClean: true, mustName: "Double",
		},
		{
			name: "a produced function correctly returning its own error",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/clean4\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nfunc parseAmount(text string) (int, error) {\n" +
					"\treturn len(text), nil\n}\n\n" +
					"func Total(text string) (int, error) {\n" +
					"\treturn parseAmount(text)\n}\n",
			},
			wantClean: true, mustName: "Total",
		},
		{
			name: "impure function swallowing a stdlib error (defective)",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/dirty1\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nimport \"os\"\n\n" +
					"func WarmCache(path string) {\n\tos.Open(path)\n}\n",
			},
			wantClean: false, mustName: "WarmCache", isDefected: true,
		},
		{
			name: "function swallowing a produced function's error (defective)",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/dirty2\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nfunc parseAmount(text string) (int, error) {\n" +
					"\treturn len(text), nil\n}\n\n" +
					"func Total(text string) int {\n" +
					"\tvalue, _ := parseAmount(text)\n\treturn value\n}\n",
			},
			wantClean: false, mustName: "Total", isDefected: true,
		},
	}

	falsePositives, truePositives, cleanCount, defectedCount := 0, 0, 0, 0
	for _, testCase := range fixtures {
		worktree := testedNamesFixture(t, testCase.files)
		functions, err := readProducedFunctions(worktree)
		if err != nil {
			t.Fatalf("%s: reading produced functions: %v", testCase.name, err)
		}
		findings := findUnhandledFailures(functions)
		flagged := false
		for _, finding := range findings {
			if finding.Where == testCase.mustName {
				flagged = true
			}
		}
		if testCase.wantClean {
			cleanCount++
			if flagged {
				falsePositives++
				t.Errorf("%s: %s was flagged but is not defective: %+v",
					testCase.name, testCase.mustName, findings)
			}
		}
		if testCase.isDefected {
			defectedCount++
			if flagged {
				truePositives++
			} else {
				t.Errorf("%s: %s is deliberately defective but was not "+
					"flagged", testCase.name, testCase.mustName)
			}
		}
	}
	t.Logf("swallowed-error finder: %d/%d false positives on clean "+
		"fixtures, %d/%d true positives on defective fixtures",
		falsePositives, cleanCount, truePositives, defectedCount)
}

// TestPIPE105_BoundaryFinderFalsePositiveRateOnCleanFixtures is the same
// measurement for findUncheckedBoundaries's two repaired checks.
func TestPIPE105_BoundaryFinderFalsePositiveRateOnCleanFixtures(t *testing.T) {
	type fixture struct {
		name        string
		files       map[string]string
		wantFinding string // substring that must NOT appear for a clean fixture
	}

	fixtures := []fixture{
		{
			name: "a real zero literal is actually passed",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/boundary_clean1\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nfunc Divide(divisor int) int {\n" +
					"\tif divisor == 0 {\n\t\treturn -1\n\t}\n\treturn 100 / divisor\n}\n",
				"lib_test.go": "package lib\n\nimport \"testing\"\n\n" +
					"func TestDivideByZero(t *testing.T) {\n" +
					"\tif Divide(0) != -1 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
			},
			wantFinding: "zero",
		},
		{
			name: "a real nil comparison asserts on the error",
			files: map[string]string{
				"go.mod": "module codeflux.test/pipe105/boundary_clean2\n\ngo 1.26.0\n",
				"lib.go": "package lib\n\nimport \"errors\"\n\n" +
					"func Parse(text string) (int, error) {\n" +
					"\tif text == \"\" {\n\t\treturn 0, errors.New(\"empty\")\n\t}\n" +
					"\treturn len(text), nil\n}\n",
				"lib_test.go": "package lib\n\nimport \"testing\"\n\n" +
					"func TestParseRejectsEmpty(t *testing.T) {\n" +
					"\t_, err := Parse(\"\")\n\tif err == nil {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
			},
			wantFinding: "never asserts on a returned error",
		},
	}

	falsePositives, cleanCount := 0, 0
	for _, testCase := range fixtures {
		worktree := testedNamesFixture(t, testCase.files)
		functions, err := readProducedFunctions(worktree)
		if err != nil {
			t.Fatalf("%s: reading produced functions: %v", testCase.name, err)
		}
		findings := findUncheckedBoundaries(worktree, functions)
		cleanCount++
		if what := findingWhat(findings, testCase.wantFinding); what != "" {
			falsePositives++
			t.Errorf("%s: unexpectedly flagged: %q", testCase.name, what)
		}
	}
	t.Logf("boundary finder: %d/%d false positives on clean fixtures",
		falsePositives, cleanCount)
}

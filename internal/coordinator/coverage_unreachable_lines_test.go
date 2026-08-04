package coordinator

import (
	"strings"
	"testing"
)

// rungOneProgram is the program ladder rung 1 actually produced on 2026-08-03,
// reproduced verbatim in shape.
//
// Worth stating plainly, because it is the whole point of the fixture: the
// model wrote writeGreeting's io.Writer and error return itself, on its first
// attempt, before any gate had said anything. The adversarial review then
// found — correctly — that main called something fallible and exited zero
// regardless, and told it to write the message to os.Stderr and call
// os.Exit(1). This is the program after it complied.
const rungOneProgram = "package main\n\n" +
	"import (\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n)\n\n" +
	"// writeGreeting writes the greeting and its newline to w.\n" +
	"func writeGreeting(w io.Writer) error {\n" +
	"\t_, err := fmt.Fprintln(w, \"Hello from CodeFlux\")\n" +
	"\treturn err\n}\n\n" +
	"// main writes one greeting to stdout and reports failure on stderr.\n" +
	"func main() {\n" +
	"\tif err := writeGreeting(os.Stdout); err != nil {\n" +
	"\t\tfmt.Fprintln(os.Stderr, err)\n" +
	"\t\tos.Exit(1)\n" +
	"\t}\n}\n"

// rungOneTests are the tests that went with it: writeGreeting is examined on
// both its paths, including the failing writer. Nothing tests main, because
// nothing can.
const rungOneTests = "package main\n\n" +
	"import (\n\t\"bytes\"\n\t\"errors\"\n\t\"testing\"\n)\n\n" +
	"type failingWriter struct{}\n\n" +
	"func (failingWriter) Write([]byte) (int, error) {\n" +
	"\treturn 0, errors.New(\"refused\")\n}\n\n" +
	"func TestWriteGreetingWrites(t *testing.T) {\n" +
	"\tbuffer := &bytes.Buffer{}\n" +
	"\tif err := writeGreeting(buffer); err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
	"\tif buffer.String() != \"Hello from CodeFlux\\n\" {\n" +
	"\t\tt.Fatalf(\"got %q\", buffer.String())\n\t}\n}\n\n" +
	"func TestWriteGreetingReportsAFailedWrite(t *testing.T) {\n" +
	"\tif err := writeGreeting(failingWriter{}); err == nil {\n" +
	"\t\tt.Fatal(\"a refused write reported success\")\n\t}\n}\n"

// TestChangedLineCoverageExemptsWhatNoTestCanExecute is the contradiction the
// path-coverage gate needed removed.
//
// Two gates in this package disagreed about main. checkControlTests skips it
// ("no Go test can call it") and wholeModuleFunctionCoverage skips it; the
// changed-line measurement that replaced the latter did not, so whether main
// had to be covered depended on which gate asked and whether attribution
// happened to be established.
//
// That disagreement is not academic, because a third gate creates the code it
// then refuses. The adversarial review demands that a main calling something
// fallible report the error and call os.Exit(1) — a demand this repository has
// already established is the satisfiable one, after the unsatisfiable version
// consumed rung 3. Those lines land in main, and changed-line coverage then
// failed the run for not executing them. No test can execute them: a test that
// reached os.Exit(1) would take the test binary down with it, and the coverage
// profile is written after the process ends.
//
// Proven to discriminate: against the previous implementation this fixture
// recorded broke with "5 of 9 changed line(s) this run wrote are never
// executed by any test (44.4% covered): main.go:16, main.go:17, main.go:18,
// main.go:19, main.go:20" — every line of main, including the error branch the
// review had demanded on the attempt before. Ladder rung 1 on 2026-08-03
// recorded the same refusal against correct produced code.
func TestChangedLineCoverageExemptsWhatNoTestCanExecute(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n",
	})
	writeAttributionFile(t, worktree, "main.go", rungOneProgram, true)
	writeAttributionFile(t, worktree, "main_test.go", rungOneTests, true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established, so this measures the " +
			"whole-module fallback rather than the changed-line path")
	}

	outcome := checkFunctionCoverage(t.Context(), worktree, attribution)
	if !outcome.Held {
		t.Fatalf("a program whose every testable line is tested was failed for "+
			"not covering lines no test can reach: %s", outcome.Detail)
	}
	exempt, _ := outcome.Evidence["unreachable_by_any_test"].([]string)
	if len(exempt) == 0 {
		t.Error("main's body was not recorded as exempt, so the gate held for " +
			"some other reason and this proves nothing about the exemption")
	}
	for _, line := range exempt {
		if !strings.HasPrefix(line, "main.go:") {
			t.Errorf("something outside main.go was excused: %s", line)
		}
	}
}

// TestChangedLineCoverageStillFailsAReachableUncoveredLine is the control, and
// it is what keeps the exemption from being a hole rather than a correction.
//
// The exemption turns on the line being unreachable in the same process, not
// on the line being inconvenient. An ordinary function this run wrote, with a
// branch no test provokes, is the defect this gate exists to name, and it must
// still be named.
func TestChangedLineCoverageStillFailsAReachableUncoveredLine(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n",
	})
	writeAttributionFile(t, worktree, "main.go", "package main\n\n"+
		"import \"fmt\"\n\n"+
		"// classify names a number, and nothing tries a negative one.\n"+
		"func classify(value int) string {\n"+
		"\tif value < 0 {\n\t\treturn \"negative\"\n\t}\n"+
		"\treturn \"not negative\"\n}\n\n"+
		"func main() {\n\tfmt.Println(classify(1))\n}\n", true)
	writeAttributionFile(t, worktree, "main_test.go", "package main\n\n"+
		"import \"testing\"\n\n"+
		"func TestClassify(t *testing.T) {\n"+
		"\tif classify(1) != \"not negative\" {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
		true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}

	outcome := checkFunctionCoverage(t.Context(), worktree, attribution)
	if outcome.Held {
		t.Fatalf("a branch this run wrote that no test provokes was let "+
			"through, so the exemption is a hole: %s", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "never executed by any test") {
		t.Errorf("the reason should still name the uncovered changed line, "+
			"got %q", outcome.Detail)
	}
}

// TestProcessTerminatingBranchesAreExemptOutsideMainToo covers the other half
// of the rule, which main alone does not reach.
//
// A run told to report a failure and exit does not always put that branch in
// main. The reason a test cannot cover it — the process ends before the
// profile is written — has nothing to do with which function it sits in, so
// the exemption follows the termination rather than the name.
func TestProcessTerminatingBranchesAreExemptOutsideMainToo(t *testing.T) {
	source := "package main\n\n" +
		"import (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
		"// fail reports a message and ends the process.\n" +
		"func fail(message string) {\n" +
		"\tif message != \"\" {\n" +
		"\t\tfmt.Fprintln(os.Stderr, message)\n" +
		"\t\tos.Exit(1)\n\t}\n}\n\n" +
		"func main() {\n\tfail(\"\")\n}\n"
	worktree, _ := newAttributionFixture(t, map[string]string{"main.go": source})

	attribution := changeAttribution{
		Established: true,
		Ranges: map[string][]changedLineRange{
			"main.go": {{Start: 1, End: 18}},
		},
	}
	exempt := linesNoTestCanExecute(worktree, attribution)["main.go"]

	// The terminating branch inside fail, lines 11-13 of the source above.
	for _, line := range []int{11, 12, 13} {
		if !exempt[line] {
			t.Errorf("line %d ends the process but was not exempted", line)
		}
	}
	// fail's own signature and its guard are ordinary, reachable code.
	for _, line := range []int{9, 10} {
		if exempt[line] {
			t.Errorf("line %d is reachable by a test but was excused", line)
		}
	}
}

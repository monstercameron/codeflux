package coordinator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeWorktree lays out a small module to run the stage checks against.
func writeWorktree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{
		"go.mod": "module codeflux.test/workspace\n\ngo 1.26.0\n",
	}
	for path, body := range files {
		all[path] = body
	}
	for path, body := range all {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A Git repository, because the stage checks ask Git which files a run
	// produced rather than treating every Go file in the tree as its work.
	// Nothing is committed, so everything written here reads as produced —
	// which is what a fixture means.
	initialize := exec.CommandContext(t.Context(), "git", "init")
	initialize.Dir = root
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	return root
}

// TestPurityIsDecidedPerFunctionNotPerFile is the defect this check had.
//
// Purity was read off the file's imports, so one function that printed made
// every function in the file impure. In a single-file program that is every
// function, and the stage reported "0 pure atoms" for code that was almost
// entirely pure — a number that was not merely imprecise but backwards.
func TestPurityIsDecidedPerFunctionNotPerFile(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func double(value int) int {\n\treturn value * 2\n}\n\n" +
			"func main() {\n\tfmt.Println(double(2))\n}\n",
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]producedFunction{}
	for _, function := range functions {
		byName[function.Name] = function
	}
	if !byName["double"].Pure {
		t.Error("double touches nothing outside its arguments and was called impure")
	}
	if byName["main"].Pure {
		t.Error("main prints and was called pure")
	}
}

// TestFormattingAValueIsNotAnEffect draws the line where it belongs.
//
// fmt.Sprintf builds a string and fmt.Println changes the world. Treating the
// whole package as an effect would make every function that formats its own
// output impure, which is most of the pure ones.
func TestFormattingAValueIsNotAnEffect(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func label(value int) string {\n\treturn fmt.Sprintf(\"%d\", value)\n}\n\n" +
			"func main() {\n\tfmt.Println(label(2))\n}\n",
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	for _, function := range functions {
		if function.Name == "label" && !function.Pure {
			t.Error("a function that only formats a value was called impure")
		}
	}
}

// TestAContractCarriesTheSignatureItsGatePromises pins what the stage records.
//
// The gate asks for signature, inputs, outputs, effects and error cases. It
// used to record loop depth and branch counts, which are true facts about a
// function and none of the ones the gate asked for.
func TestAContractCarriesTheSignatureItsGatePromises(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"func parse(text string, limit int) ([]int, error) {\n" +
			"\treturn nil, nil\n}\n\nfunc main() {}\n",
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	for _, function := range functions {
		if function.Name != "parse" {
			continue
		}
		if got := strings.Join(function.Parameters, ", "); got != "string, int" {
			t.Errorf("inputs = %q, want the declared types", got)
		}
		if got := strings.Join(function.Results, ", "); got != "[]int, error" {
			t.Errorf("outputs = %q, want the declared types", got)
		}
		if !function.ReturnsError {
			t.Error("a function returning an error was recorded as unable to fail")
		}
		if !strings.Contains(function.Signature, "func parse(string, int)") {
			t.Errorf("signature = %q", function.Signature)
		}
	}
}

// TestAnAtomIsWhatCallsNothingElse fixes the distinction the flow rests on.
func TestAnAtomIsWhatCallsNothingElse(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"func base(value int) int {\n\treturn value + 1\n}\n\n" +
			"func composed(value int) int {\n\treturn base(value) + base(value)\n}\n\n" +
			"func main() {}\n",
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	atoms, molecules := atomsAndMolecules(functions)
	atomNames := map[string]bool{}
	for _, atom := range atoms {
		atomNames[atom.Name] = true
	}
	moleculeNames := map[string]bool{}
	for _, molecule := range molecules {
		moleculeNames[molecule.Name] = true
	}
	if !atomNames["base"] {
		t.Error("a function calling nothing was not counted as an atom")
	}
	if !moleculeNames["composed"] {
		t.Error("a function calling others was not counted as a composition")
	}
	if atomNames["composed"] {
		t.Error("a composition was also counted as an atom")
	}
}

// TestAnUntestedAtomIsReported is the check doing its job.
func TestAnUntestedAtomIsReported(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"func used(value int) int {\n\treturn value\n}\n\n" +
			"func ignored(value int) int {\n\treturn value\n}\n\nfunc main() {}\n",
		"cmd/thing/main_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestUsed(t *testing.T) {\n\tif used(1) != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	})
	outcome := checkAtomTests(worktree)
	if outcome.Held {
		t.Error("an atom no test mentions was reported as covered")
	}
	if !strings.Contains(outcome.Detail, "ignored") {
		t.Errorf("the finding does not name the untested atom: %q", outcome.Detail)
	}
	if strings.Contains(outcome.Detail, "used,") {
		t.Errorf("a tested atom was reported as untested: %q", outcome.Detail)
	}
}

// TestASingleExampleSuiteIsNotAPropertySuite keeps the two apart.
func TestASingleExampleSuiteIsNotAPropertySuite(t *testing.T) {
	single := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\nfunc main() {}\n",
		"cmd/thing/main_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestOne(t *testing.T) {\n\tif 1 != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	})
	if checkPropertyTests(single).Held {
		t.Error("a suite of one example was accepted as examining a property")
	}

	tabular := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\nfunc main() {}\n",
		"cmd/thing/main_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestMany(t *testing.T) {\n" +
			"\tfor _, value := range []int{1, 2, 3} {\n" +
			"\t\tif value < 1 {\n\t\t\tt.Fatal(\"no\")\n\t\t}\n\t}\n}\n",
	})
	if !checkPropertyTests(tabular).Held {
		t.Error("a table-driven suite was not recognised as examining a set")
	}
}

// TestCapabilitiesAreReportedAndOnlyDeclaredOnesAreRefused pins the rule.
//
// The check used to forbid a fixed list of imports, which encoded an
// assumption that every program is a filter reading input and writing output.
// A program asked to serve HTTP would have failed a gate for doing exactly
// what it was asked. What generalises is the report, not the prohibition.
func TestCapabilitiesAreReportedAndOnlyDeclaredOnesAreRefused(t *testing.T) {
	quiet := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"hello\")\n}\n",
	})
	if !checkGlobalInvariants(quiet, nil).Held {
		t.Error("a program that only prints was reported as overreaching")
	}

	reaching := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\nimport \"net/http\"\n\n" +
			"func main() {\n\t_ = http.DefaultClient\n}\n",
	})
	reported := checkGlobalInvariants(reaching, nil)
	if !reported.Held {
		t.Errorf("a program was refused for a capability nothing forbade: %s",
			reported.Detail)
	}
	if !strings.Contains(reported.Detail, "network") {
		t.Errorf("the report does not say what the program can now do: %q",
			reported.Detail)
	}

	// Declared forbidden, the same program fails, and the failure says the
	// restriction was declared rather than assumed.
	refused := checkGlobalInvariants(reaching, []string{"network"})
	if refused.Held {
		t.Error("a declared restriction was not enforced")
	}
	if !strings.Contains(refused.Detail, "declared") {
		t.Errorf("the refusal does not say the restriction was declared: %q",
			refused.Detail)
	}
}

// TestComplexityFollowsLoopNesting keeps the label tied to the structure.
func TestComplexityFollowsLoopNesting(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"func flat(values []int) int {\n\ttotal := 0\n" +
			"\tfor _, value := range values {\n\t\ttotal += value\n\t}\n\treturn total\n}\n\n" +
			"func nested(values []int) int {\n\ttotal := 0\n" +
			"\tfor _, outer := range values {\n\t\tfor _, inner := range values {\n" +
			"\t\t\ttotal += outer * inner\n\t\t}\n\t}\n\treturn total\n}\n\nfunc main() {}\n",
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	for _, function := range functions {
		switch function.Name {
		case "flat":
			if complexityLabel(function.LoopDepth) != "O(n)" {
				t.Errorf("one loop labelled %s",
					complexityLabel(function.LoopDepth))
			}
		case "nested":
			if complexityLabel(function.LoopDepth) != "O(n^2)" {
				t.Errorf("two nested loops labelled %s",
					complexityLabel(function.LoopDepth))
			}
		}
	}
}

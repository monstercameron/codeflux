package coordinator

import (
	"strings"
	"testing"
)

// TestADeclaredUnreachableBranchIsNotAskedForAgain is the promise the gate was
// not keeping.
//
// The coverage instruction ends by telling a run that a line it genuinely
// cannot reach should be explained and left alone. Nothing read that
// explanation, so the run wrote it, left the branch, and was asked for the same
// lines again. Ladder rung 7 on 2026-08-03 was asked three times for lines
// whose branch carried exactly that comment, and spent eight attempts and ten
// minutes on it.
func TestADeclaredUnreachableBranchIsNotAskedForAgain(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n",
	})
	writeAttributionFile(t, worktree, "main.go", "package main\n\n"+
		"import \"fmt\"\n\n"+
		"// total adds the values it is given.\n"+
		"func total(values []int) (int, error) {\n"+
		"\tsum := 0\n"+
		"\tfor _, value := range values {\n\t\tsum += value\n\t}\n"+
		"\treturn sum, nil\n}\n\n"+
		"func main() {\n"+
		"\tsum, err := total([]int{1, 2})\n"+
		"\t//codeflux:unreachable total cannot fail for a slice of ints; the "+
		"branch preserves the error contract\n"+
		"\tif err != nil {\n\t\tfmt.Println(err)\n\t\treturn\n\t}\n"+
		"\tfmt.Println(sum)\n}\n", true)
	writeAttributionFile(t, worktree, "main_test.go", "package main\n\n"+
		"import \"testing\"\n\n"+
		"func TestTotal(t *testing.T) {\n"+
		"\tsum, err := total([]int{1, 2})\n"+
		"\tif err != nil || sum != 3 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}
	outcome := checkFunctionCoverage(t.Context(), worktree, attribution)
	if !outcome.Held && !outcome.Skipped {
		t.Fatalf("a branch the run declared unreachable, with its reason, was "+
			"asked for again: %s", outcome.Detail)
	}
}

// TestAMarkerWithNoReasonExemptsNothing is the control.
//
// The reason is what makes the claim reviewable. A bare marker is a way to
// switch the gate off, and a gate that can be switched off by writing a comment
// is not a gate.
func TestAMarkerWithNoReasonExemptsNothing(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n",
	})
	writeAttributionFile(t, worktree, "main.go", "package main\n\n"+
		"// classify names a number.\n"+
		"func classify(value int) string {\n"+
		"\t//codeflux:unreachable\n"+
		"\tif value < 0 {\n\t\treturn \"negative\"\n\t}\n"+
		"\treturn \"not negative\"\n}\n\n"+
		"func main() {\n\t_ = classify(1)\n}\n", true)
	writeAttributionFile(t, worktree, "main_test.go", "package main\n\n"+
		"import \"testing\"\n\n"+
		"func TestClassify(t *testing.T) {\n"+
		"\tif classify(1) != \"not negative\" {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
		true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	outcome := checkFunctionCoverage(t.Context(), worktree, attribution)
	if outcome.Held {
		t.Error("a marker with no reason silenced the gate")
	}
	if !strings.Contains(outcome.Detail, "never executed by any test") {
		t.Errorf("the reason should still name the uncovered line, got %q",
			outcome.Detail)
	}
}

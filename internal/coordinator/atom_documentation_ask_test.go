package coordinator

import (
	"strings"
	"testing"
)

// TestAnUndocumentedAtomIsNamedSoItCanBeRegistered is the link that was
// missing from the compounding loop.
//
// Registration refuses a declaration carrying no //codeflux:atom schema
// comment, and nothing ever asked for one. Every run of this session recorded
// "0 of N produced atom(s) carried schema-v1 documentation eligible for
// registration", atom_names held zero rows, and the recall stage searched an
// empty project every time. A run cannot reuse what no run registered.
func TestAnUndocumentedAtomIsNamedSoItCanBeRegistered(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/atoms\n\ngo 1.26.0\n",
		"lib.go": "package atoms\n\n" +
			"// Total adds what it is given.\n" +
			"func Total(values []int) int {\n\tsum := 0\n" +
			"\tfor _, value := range values {\n\t\tsum += value\n\t}\n" +
			"\treturn sum\n}\n",
	})
	produced, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("read produced functions: %v", err)
	}
	missing := atomsWithoutRegistrableDocumentation(worktree, produced)
	if len(missing) != 1 || missing[0] != "Total" {
		t.Fatalf("an atom with only an ordinary doc comment is not "+
			"registrable and must be named, got %v", missing)
	}

	instruction := atomDocumentationInstruction(missing)
	for _, wanted := range []string{
		"Total", "//codeflux:atom", "Purpose", "Retrieval concepts",
		"already exists here",
	} {
		if !strings.Contains(instruction, wanted) {
			t.Errorf("the instruction should contain %q", wanted)
		}
	}
}

// TestMainAndComposingFunctionsAreNotAsked is the control that keeps this from
// costing a run its attempts.
//
// Nineteen fields per declaration is the most expensive instruction in the
// set. main is not an atom, and a composing function is registered by the
// molecule stage on its own terms, so asking either for it spends attempts on
// documentation registration would refuse anyway.
func TestMainAndComposingFunctionsAreNotAsked(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/atoms\n\ngo 1.26.0\n",
		"main.go": "package main\n\nimport \"fmt\"\n\n" +
			"// leaf is an atom.\n" +
			"func leaf(value int) int {\n\treturn value + 1\n}\n\n" +
			"// composes calls leaf, so it is not a leaf itself.\n" +
			"func composes(value int) int {\n\treturn leaf(value) + leaf(value)\n}\n\n" +
			"// main wires it together.\n" +
			"func main() {\n\tfmt.Println(composes(1))\n}\n",
	})
	produced, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("read produced functions: %v", err)
	}
	missing := atomsWithoutRegistrableDocumentation(worktree, produced)
	for _, name := range missing {
		if name == "main" {
			t.Error("main is not an atom and must not be asked for atom docs")
		}
		if name == "composes" {
			t.Error("a composing function is the molecule stage's business")
		}
	}
	if len(missing) != 1 || missing[0] != "leaf" {
		t.Errorf("only the leaf should be asked, got %v", missing)
	}
}

// TestNothingIsAskedWhenTheSchemaIsAlreadyThere pins the satisfied case.
func TestNothingIsAskedWhenTheSchemaIsAlreadyThere(t *testing.T) {
	if got := atomDocumentationInstruction(nil); got != "" {
		t.Errorf("with nothing missing there is nothing to ask, got %q", got)
	}
}

// TestATaskLocalProcedureIsNotWorthRegistering covers the admission gate.
//
// printFizzBuzz was queued for nineteen fields of documentation alongside a CLI
// argument parser, and ladder rung 2 spent its attempts alternating between
// that ask and the completeness gate on the lowest rung of the model ladder.
func TestATaskLocalProcedureIsNotWorthRegistering(t *testing.T) {
	for _, refused := range []producedFunction{
		{Name: "printFizzBuzz", Parameters: []string{"int"}, Pure: false},
		{Name: "run", Parameters: []string{"io.Reader", "io.Writer"},
			Results: []string{"error"}, ReturnsError: true, Pure: false},
		{Name: "banner", Results: []string{"string"}, Pure: true},
	} {
		if worthAdmitting(refused) {
			t.Errorf("%s was queued for the full atom schema", refused.Name)
		}
	}
	parser := producedFunction{
		Name:       "parseArguments",
		Parameters: []string{"[]string"},
		Results:    []string{"[]int", "error"},
		ReturnsError: true,
		Pure:       true,
	}
	if !worthAdmitting(parser) {
		t.Error("a pure parser returning a value was refused registration")
	}
}

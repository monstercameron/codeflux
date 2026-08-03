package coordinator

import "testing"

// TestPIPE141_AUnitIsNamedOnlyWhenATestCallsIt is the discrimination proof
// for PIPE-141: testsNamingInFiles used to sweep every *ast.Ident in a test
// function's body, so a produced function counted as verified by a test that
// merely happened to reuse its name as a local variable — never called it at
// all. checkAtomVerification (StageAtomVerification) reads this map to
// decide which tests to run for a unit, so that coincidence let an unit be
// reported proven by a test that examined nothing about it.
//
// This is the same defect PIPE-008 repairs at testedNames
// (agent_stage_structure.go), reached at the separate site testsNamingInFiles
// parses for a different stage; PIPE-008's own fix does not touch this
// function at all.
//
// Proven to discriminate: reverting testsNamingInFiles to the identifier
// sweep (visiting every *ast.Ident rather than only call-expression callees)
// makes "Coincidence" appear in the returned map, naming TestThings even
// though TestThings never calls it — exactly the false verification this
// ticket exists to remove.
func TestPIPE141_AUnitIsNamedOnlyWhenATestCallsIt(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/naming\n\ngo 1.26.0\n",
		"lib.go": `package lib

func Called(value int) int { return value }

func Coincidence(value int) int { return value }

func ViaMethod() int { return 3 }

type helper struct{}

func (helper) ViaMethod() int { return 3 }
`,
		"lib_test.go": `package lib

import "testing"

type holder struct{ Coincidence int }

func TestThings(t *testing.T) {
	// A real call.
	if Called(1) != 1 {
		t.Fatal("no")
	}
	// Only ever a field name and a local variable, never a call.
	Coincidence := holder{Coincidence: 2}
	_ = Coincidence
	// A method call whose selector matches a produced function name.
	var value helper
	if value.ViaMethod() != 3 {
		t.Fatal("no")
	}
}
`,
	})

	naming, err := testsNaming(worktree)
	if err != nil {
		t.Fatalf("reading test naming: %v", err)
	}

	if tests := naming["Called"]; len(tests) != 1 || tests[0] != "TestThings" {
		t.Errorf("a function the test calls is not named: %v", naming["Called"])
	}
	if tests := naming["Coincidence"]; len(tests) != 0 {
		t.Errorf("a name that only ever appears as a field and a local "+
			"variable is reported named by a test, so verification is "+
			"satisfiable by coincidence: %v", tests)
	}
	if tests := naming["ViaMethod"]; len(tests) != 1 || tests[0] != "TestThings" {
		t.Errorf("a method call's selector is not named, so a unit reached "+
			"only through a method reads as unverified: %v", naming["ViaMethod"])
	}
}

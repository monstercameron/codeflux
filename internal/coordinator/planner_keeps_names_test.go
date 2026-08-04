package coordinator

import (
	"strings"
	"testing"
)

// TestThePlannerIsToldToKeepTheNamesTheRequestGives is the instruction that
// cost rung 17 a run it had otherwise finished.
//
// The behaviours become the instruction a run works from: the plan step's
// summary is the numbered list of them and nothing else. So a paraphrase loses
// the one part of a request nobody can infer. Rung 17 on 2026-08-04 asks for a
// function called Totals and a struct called Entry; the decomposition rendered
// those as "Aggregate amounts independently for each name" and "Expose entries
// with a name and integer amount", and the run built the right shape around a
// function it called Calculate — one package reaching nothing outside itself,
// built, run, printing exactly what was asked, surviving every hostile input,
// and failed for the name alone.
func TestThePlannerIsToldToKeepTheNamesTheRequestGives(t *testing.T) {
	instruction := planningInstruction(
		"One package must be pure and exports an Entry struct and a function " +
			"Totals taking a slice of Entry.")

	if !strings.Contains(instruction, "spelled exactly") {
		t.Errorf("nothing asks the planner to keep the request's own names, "+
			"so a behaviour may paraphrase away the only thing a caller "+
			"depends on:\n%s", instruction)
	}
	// The request itself still travels, because the instruction is about how to
	// read it and the planner needs the thing being read.
	if !strings.Contains(instruction, "Totals") {
		t.Error("the request is not in the instruction at all")
	}
}

// TestThePlanningInstructionStillAsksForBehaviours is the control.
//
// The addition must not turn the decomposition into a naming exercise. What a
// run needs before it starts is the list of things that can independently be
// got wrong, and the names are an attribute of those, not a replacement.
func TestThePlanningInstructionStillAsksForBehaviours(t *testing.T) {
	instruction := planningInstruction("Print the sum of the arguments.")
	for _, wanted := range []string{
		"distinct behaviours", "needs its own test", "one behaviour per line",
	} {
		if !strings.Contains(instruction, wanted) {
			t.Errorf("the instruction no longer asks for %q:\n%s",
				wanted, instruction)
		}
	}
	// And it still asks for the layout, which is the other thing the planner
	// alone can decide.
	if !strings.Contains(instruction, "module-relative paths") {
		t.Errorf("the instruction stopped asking for the layout:\n%s",
			instruction)
	}
}

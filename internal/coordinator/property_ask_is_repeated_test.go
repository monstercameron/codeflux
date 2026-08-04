package coordinator

import (
	"strings"
	"testing"
)

// TestThePropertyAskSurvivesBeingBuried is the gap between what a run is told
// and what it is judged on.
//
// atom-property-tests is a hard gate: a run that never states a property fails,
// however well everything else went. It was asked exactly once per run, on the
// reasoning that a run told this precisely and not doing it will not do it on
// the third telling. That holds when the tellings are consecutive. It does not
// hold when the demand is raised and then buried.
//
// Ladder rung 18 on 2026-08-04 was asked at attempt four, spent attempts five
// through nine on integration-tests, path-coverage and atom-documentation, was
// never reminded, and failed the run on this gate two hundred seconds later —
// having been told, in that same message, "every one of these is checked again,
// together, on the next attempt". That was true of the checking and false of
// the asking.
func TestThePropertyAskSurvivesBeingBuried(t *testing.T) {
	if propertyRoundBackstop < 2 {
		t.Fatalf("a hard gate asked %d time(s) per run fails a run for a "+
			"demand it may have been given before six other gates buried it",
			propertyRoundBackstop)
	}
	if propertyRoundBackstop > 4 {
		t.Errorf("asked %d times, this becomes the thing that spends the "+
			"attempts rather than the thing that saves them",
			propertyRoundBackstop)
	}
}

// TestThePropertyInstructionSaysWhatAPropertyIs keeps the repetition worth
// making.
//
// Repeating an instruction only helps if the instruction was actionable to
// begin with. This one has to name the shape wanted — a table in plain Go over
// chosen inputs — rather than only naming the gate that is unhappy.
func TestThePropertyInstructionSaysWhatAPropertyIs(t *testing.T) {
	instruction := propertyTestInstruction(
		"all 20 test(s) check a single example")
	for _, wanted := range []string{"table", "across a range of inputs"} {
		if !strings.Contains(instruction, wanted) {
			t.Errorf("the instruction never mentions %q, so telling a run "+
				"again tells it nothing new:\n%s", wanted, instruction)
		}
	}
	// And it carries the gate's own finding, so the run knows what was counted.
	if !strings.Contains(instruction, "all 20 test(s) check a single example") {
		t.Errorf("the instruction drops the detail it was given:\n%s",
			instruction)
	}
}

package coordinator

import "testing"

// TestRegistrationJudgesTheSameAtomsTheAskAsksFor is the disagreement the
// registry ledger was reporting as a failure.
//
// The documentation ask narrows to what is worth remembering. worthAdmitting
// exists because asking for nineteen fields about a task-local procedure that
// prints and returns nothing put it in the same queue as a reusable parser, and
// ladder rung 2 spent its attempts oscillating over it.
//
// Registration did not narrow the same way, so it counted declarations nobody
// had ever been asked to document and reported them as unregistered. Ladder
// rung 4 on 2026-08-03 read "0 of 3 produced atom(s) reached the registry" for
// a run in which one atom had been asked for, documented, and refused on a
// missing field, and the other two had never been mentioned at all.
//
// Two places deciding one fact must decide it the same way, or the ledger
// reports whichever is stricter as the truth.
func TestRegistrationJudgesTheSameAtomsTheAskAsksFor(t *testing.T) {
	// A leaf that returns a value another task could want: worth remembering,
	// and therefore both asked for and registered.
	reusable := producedFunction{
		Name: "orderedCounts", File: "cmd/generated/main.go",
		Parameters: []string{"map[string]int"}, Results: []string{"[]wordCount"},
		Pure: true,
	}
	// A leaf that returns nothing: nothing a later caller could reuse, so the
	// ask deliberately skips it and registration must skip it too.
	local := producedFunction{
		Name: "printBanner", File: "cmd/generated/main.go",
	}

	if !worthAdmitting(reusable) {
		t.Fatal("a leaf returning a value is what the registry is for; if this " +
			"is not admitted the fixture no longer proves anything")
	}
	if worthAdmitting(local) {
		t.Error("a procedure that returns nothing was judged worth nineteen " +
			"fields and a permanent row, which is the ask this narrowing exists " +
			"to prevent")
	}
}

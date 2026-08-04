package coordinator

import (
	"strings"
	"testing"
)

// TestAnUnchangedSuiteIsRefusedRatherThanRepeated is the loop that spent a whole
// attempt budget.
//
// The unchanged note already says the files have not moved and says not to run
// the suite again until one does. A run that asks anyway has not read it, and
// handing back the same output once more is the pipeline agreeing the call was
// worth making. Ladder rung 16 on 2026-08-04 spent 40 of its 73 rounds this
// way across 14 attempts, wrote six times in all, and exhausted its budget
// without ever reaching the compile error its last real suite run reported.
//
// Two answers, then a refusal. The first is the run checking, which is
// reasonable. The second is the note being read and acted on anyway. The third
// is a loop.
func TestAnUnchangedSuiteIsRefusedRatherThanRepeated(t *testing.T) {
	if maximumUnchangedSuiteAnswers < 1 {
		t.Fatal("a run has to be allowed to ask at least once")
	}
	if maximumUnchangedSuiteAnswers > 3 {
		t.Errorf("%d answers before refusing is most of an attempt spent on a "+
			"question that cannot be answered differently",
			maximumUnchangedSuiteAnswers)
	}
}

// TestAWriteMakesTheSuiteWorthAskingAgain is the control, and it is what keeps
// the refusal from being a wall.
//
// The refusal exists because the answer cannot differ. The moment a file
// differs it can, so the count has to start over — otherwise a run that
// checked, wrote, and checked again would be refused for the write that fixed
// things.
func TestAWriteMakesTheSuiteWorthAskingAgain(t *testing.T) {
	narrator := &narratingExecutor{unchangedTestsInARow: 9}
	// The same reset the write path performs.
	narrator.writesSinceLastTest++
	narrator.unchangedTestsInARow = 0
	if narrator.unchangedTestsInARow != 0 {
		t.Fatal("a write must make the suite worth asking about again")
	}
}

// TestTheRefusalSaysWhatWouldMakeTheCallWorthMaking keeps it from being noise.
//
// A refusal that only says no costs a round and teaches nothing. This one has
// to name the condition — a file has to differ — because that is the single
// action that turns the refusal back into an answer.
func TestTheRefusalSaysWhatWouldMakeTheCallWorthMaking(t *testing.T) {
	for count, want := range map[int]string{
		3: "third", 4: "fourth", 12: "12th",
	} {
		if got := ordinal(count); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", count, got, want)
		}
	}
	note := unchangedTestNote(0)
	if !strings.Contains(note, "do not run the tests again until a file differs") {
		t.Errorf("the note the refusal follows should already say the "+
			"condition, got %q", note)
	}
}

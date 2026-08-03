package coordinator

import (
	"strings"
	"testing"
)

// TestALessonIsAnInstructionNotANarration is what makes a lesson worth the
// context it costs.
//
// "An earlier run was sent back at the assembly gate because it did not
// compile" tells the next run nothing it can act on. One lesson is one thing
// to do differently, so two lessons cannot half-overlap into a paragraph
// nobody reads.
func TestALessonIsAnInstructionNotANarration(t *testing.T) {
	statement := lessonStatement("assembly", "it did not compile", "some detail")
	if strings.Contains(statement, "earlier run") ||
		strings.Contains(statement, "was sent back") {
		t.Errorf("a lesson must say what to do, not what happened: %q", statement)
	}
	if !strings.Contains(statement, "compile") {
		t.Errorf("the lesson should name the thing to do: %q", statement)
	}
}

// TestTheSameMistakeFromTwoRunsIsOneLesson is what keeps the context from
// filling with restatements of one idea.
//
// The lesson is keyed on the gate, not on the failure text, because the text
// carries file names and line numbers that differ between runs of the same
// mistake. Two runs failing to compile must produce one lesson.
func TestTheSameMistakeFromTwoRunsIsOneLesson(t *testing.T) {
	first := lessonStatement("assembly", "it did not compile",
		"cmd/a/main.go:12: undefined: foo")
	second := lessonStatement("assembly", "it did not compile",
		"cmd/b/other.go:98: undefined: bar")
	if first != second {
		t.Errorf("the same mistake must render as one lesson, got:\n%q\n%q",
			first, second)
	}
}

// TestAnUnrecognisedGateIsObservedRatherThanInvented is the control.
//
// Inventing an imperative for a failure this does not recognise would be
// guessing at a fix, and a confident wrong rule is worse in a prompt than a
// plain observation. The fallback must still be usable, and must not pretend.
func TestAnUnrecognisedGateIsObservedRatherThanInvented(t *testing.T) {
	statement := lessonStatement("some-future-gate", "the widget was blue", "")
	if statement == "" {
		t.Fatal("an unrecognised gate still taught the project something")
	}
	if !strings.Contains(statement, "the widget was blue") {
		t.Errorf("the fallback should carry what the gate actually said: %q",
			statement)
	}
}

// TestALessonWithNothingToSayIsNotRecorded pins the empty case.
//
// A refusal with no gate and no reason is not a lesson, and writing it down
// would spend a slot in every later run's context on nothing.
func TestALessonWithNothingToSayIsNotRecorded(t *testing.T) {
	for _, testCase := range []struct{ gate, because string }{
		{"", "something"}, {"assembly", ""}, {"", ""}, {"  ", "  "},
	} {
		if got := lessonStatement(testCase.gate, testCase.because, ""); got != "" {
			t.Errorf("gate=%q because=%q should record nothing, got %q",
				testCase.gate, testCase.because, got)
		}
	}
}

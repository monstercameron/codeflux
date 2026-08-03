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

// TestAToolRefusalBecomesALesson covers the mistakes that never reach a gate.
//
// Ladder rung 3 made all four of these and the gate vocabulary has a word for
// none of them: the run fixed each one and arrived at the gate clean, so
// nothing downstream would ever have seen them.
func TestAToolRefusalBecomesALesson(t *testing.T) {
	for _, want := range []struct {
		output string
		key    string
	}{
		{"this is a .go file and the content does not parse as Go: " +
			"main.go:1:1: expected 'package', found '*'", "unparsable-write"},
		{"cmd/generated/main_test.go:72:23: too many arguments in call to run",
			"signature-drift"},
		{"cmd/generated/main.go:85:13: undefined: strings", "missing-import"},
		{"main.go:12:2: declared and not used: total", "unused-declaration"},
	} {
		key, statement, ok := toolLessonFor(want.output)
		if !ok {
			t.Errorf("no lesson was drawn from %q", want.output)
			continue
		}
		if key != want.key {
			t.Errorf("%q was read as %s, wanted %s", want.output, key, want.key)
		}
		if statement == "" {
			t.Errorf("%s produced an empty lesson", key)
		}
	}
}

// TestAnUnrecognisedFailureTeachesNothing guards against the bloat: a lesson
// invented from arbitrary output carries the file and line it happened at, so
// it never merges with the same mistake made elsewhere.
func TestAnUnrecognisedFailureTeachesNothing(t *testing.T) {
	if _, _, ok := toolLessonFor(
		"main.go:41:9: cannot use x (variable of type int) as string value",
	); ok {
		t.Error("a failure this does not recognise produced a lesson anyway")
	}
}

// TestAMarkdownFenceIsReadBeforeAMissingImport covers the ordering: a
// markdown-wrapped file also fails to compile, and telling a run to check its
// imports would send it looking in the wrong place.
func TestAMarkdownFenceIsReadBeforeAMissingImport(t *testing.T) {
	key, _, ok := toolLessonFor(
		"does not parse as Go: undefined: strings")
	if !ok || key != "unparsable-write" {
		t.Errorf("an unparsable write was read as %q", key)
	}
}

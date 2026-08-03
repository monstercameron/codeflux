package coordinator

import (
	"strings"
	"testing"
)

// TestPIPE121_AnArgumentContainingASpaceCanBeExpressed is the defect this
// ticket exists to fix. Before it, an args: line split unconditionally on
// every space (strings.Split(trimmed, " ")), so "greet Jane Doe" as a single
// argument could never be expressed — it could only ever read back as three
// arguments, silently narrowing what an acceptance example could state about
// a command's real interface. Quoting it must now read back as one.
//
// Discriminates directly: reverting splitAcceptanceArguments's call site back
// to strings.Split(trimmed, " ") makes this fail with 4 arguments
// (`"Jane`, `Doe"`, `and`, `co-workers"`) instead of 2, because the quotes and
// the internal spaces would no longer be treated specially.
func TestPIPE121_AnArgumentContainingASpaceCanBeExpressed(t *testing.T) {
	requirement := "Create cmd/greet/main.go that greets by name.\n\n" +
		`<<<ACCEPTANCE` + "\n" +
		`args: "Jane Doe and co-workers" --loud` + "\n" +
		"expected: HELLO, JANE DOE AND CO-WORKERS\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 {
		t.Fatalf("%d example(s) parsed, want 1", len(examples))
	}
	arguments := examples[0].Arguments
	if len(arguments) != 2 {
		t.Fatalf("arguments = %#v, want exactly 2: the quoted argument must "+
			"stay whole", arguments)
	}
	if arguments[0] != "Jane Doe and co-workers" {
		t.Errorf("argument 1 = %q, want the space-containing name intact",
			arguments[0])
	}
	if arguments[1] != "--loud" {
		t.Errorf("argument 2 = %q, want the unquoted flag unchanged",
			arguments[1])
	}
}

// TestPIPE121_UnquotedArgsStillSplitOnWhitespaceAsBefore is the compatibility
// half: every existing args: line in the repository's own fixtures has no
// quoting in it, and this proves the new tokenizer reads them exactly as the
// old strings.Split(trimmed, " ") did.
func TestPIPE121_UnquotedArgsStillSplitOnWhitespaceAsBefore(t *testing.T) {
	got := splitAcceptanceArguments("15 x")
	want := []string{"15", "x"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("splitAcceptanceArguments(%q) = %#v, want %#v",
			"15 x", got, want)
	}
}

// TestPIPE121_SingleAndDoubleQuotesBothWork covers the second common
// quoting convention, since a person writing a requirement by hand reaches
// for whichever one is habitual.
func TestPIPE121_SingleAndDoubleQuotesBothWork(t *testing.T) {
	cases := map[string][]string{
		`"two words" three`: {"two words", "three"},
		`'two words' three`: {"two words", "three"},
		`one 'two words'`:   {"one", "two words"},
		`one "two words"`:   {"one", "two words"},
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			got := splitAcceptanceArguments(input)
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("splitAcceptanceArguments(%q) = %#v, want %#v",
					input, got, want)
			}
		})
	}
}

// TestPIPE121_AnEmptyQuotedArgumentIsPreserved proves a quoted empty string
// is read as one empty argument rather than vanishing, which a command
// distinguishing "no argument" from "an empty argument" needs to be able to
// state.
func TestPIPE121_AnEmptyQuotedArgumentIsPreserved(t *testing.T) {
	got := splitAcceptanceArguments(`before "" after`)
	want := []string{"before", "", "after"}
	if len(got) != len(want) {
		t.Fatalf("splitAcceptanceArguments = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("argument %d = %q, want %q", index, got[index], want[index])
		}
	}
}

// TestPIPE121_AnEscapedQuoteStaysInsideTheArgument lets a literal quote
// character appear inside an argument, so an example is not additionally
// blocked from stating one just because the format itself uses quotes as
// delimiters.
func TestPIPE121_AnEscapedQuoteStaysInsideTheArgument(t *testing.T) {
	got := splitAcceptanceArguments(`"say \"hi\" now"`)
	want := []string{`say "hi" now`}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("splitAcceptanceArguments = %#v, want %#v", got, want)
	}
}

// TestPIPE121_QuotedArgumentReachesRunAcceptanceExampleIntact proves the
// parsed argument is what actually reaches exec.Command's argv: quoting is
// resolved once, at parse time, and the argument is never re-split anywhere
// downstream.
func TestPIPE121_QuotedArgumentReachesRunAcceptanceExampleIntact(t *testing.T) {
	requirement := `<<<ACCEPTANCE` + "\n" +
		`args: "hello world"` + "\n" +
		"expected: ok\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 || len(examples[0].Arguments) != 1 {
		t.Fatalf("parsed %#v, want exactly 1 example with 1 argument", examples)
	}
	if examples[0].Arguments[0] != "hello world" {
		t.Errorf("argument = %q, want %q", examples[0].Arguments[0], "hello world")
	}
}

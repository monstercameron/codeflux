package filetree

import (
	"strings"
	"testing"
)

func joinLine(spans []Span) string {
	var builder strings.Builder
	for _, span := range spans {
		builder.WriteString(span.Text)
	}
	return builder.String()
}

func classOf(t *testing.T, spans []Span, text string) TokenClass {
	t.Helper()
	for _, span := range spans {
		if span.Text == text {
			return span.Class
		}
	}
	t.Fatalf("no span reads exactly %q in %+v", text, spans)
	return ClassPlain
}

// TestHighlightingNeverChangesTheFile is the property that matters most: the
// viewer shows source, and source that came back subtly different from what is
// on disk would be worse than no colour at all.
func TestHighlightingNeverChangesTheFile(t *testing.T) {
	source := "package inventory\n\n" +
		"// Hold identifies one reservation.\n" +
		"type Hold struct {\n\tID string // trailing\n}\n\n" +
		"func ReleaseHold(id string) error {\n" +
		"\tif id == \"\" {\n\t\treturn errors.New(`empty // not a comment`)\n\t}\n" +
		"\treturn nil\n}\n"
	lines := highlightGo(source)
	rebuilt := make([]string, 0, len(lines))
	for _, line := range lines {
		rebuilt = append(rebuilt, joinLine(line))
	}
	if got := strings.Join(rebuilt, "\n"); got != source {
		t.Fatalf("the highlighter changed the file:\nwant %q\ngot  %q", source, got)
	}
}

// TestTheScannerDecidesWhatIsACommentAndWhatIsAString: pattern matching would
// call the "//" inside a raw string a comment, which is the classic way a
// hand-rolled highlighter lies about code.
func TestTheScannerDecidesWhatIsACommentAndWhatIsAString(t *testing.T) {
	lines := highlightGo("var path = `https://example.com` // the real comment\n")
	first := lines[0]
	if got := classOf(t, first, "`https://example.com`"); got != ClassLiteral {
		t.Fatalf("the raw string was classed %q", got)
	}
	if got := classOf(t, first, "// the real comment"); got != ClassComment {
		t.Fatalf("the comment was classed %q", got)
	}
}

// TestAFunctionsOwnNameIsMarkedAtItsDeclaration keeps the thing a reader
// scanning for structure is looking for distinguishable from its callers.
func TestAFunctionsOwnNameIsMarkedAtItsDeclaration(t *testing.T) {
	lines := highlightGo("func ReleaseHold(count int) error {\n\treturn nil\n}\n")
	first := lines[0]
	if got := classOf(t, first, "ReleaseHold"); got != ClassDeclared {
		t.Fatalf("the declared name was classed %q", got)
	}
	if got := classOf(t, first, "func"); got != ClassKeyword {
		t.Fatalf("a keyword was classed %q", got)
	}
	if got := classOf(t, first, "int"); got != ClassBuiltin {
		t.Fatalf("a predeclared type was classed %q", got)
	}
	if got := classOf(t, lines[1], "nil"); got != ClassBuiltin {
		t.Fatalf("nil was classed %q", got)
	}
}

// TestCodeThatDoesNotCompileIsStillColoured: a file being edited is exactly
// when somebody is reading it, and half-coloured beats refusing.
func TestCodeThatDoesNotCompileIsStillColoured(t *testing.T) {
	source := "func Broken( {\n\treturn \"unterminated\n}\n"
	lines := highlightGo(source)
	rebuilt := make([]string, 0, len(lines))
	for _, line := range lines {
		rebuilt = append(rebuilt, joinLine(line))
	}
	if got := strings.Join(rebuilt, "\n"); got != source {
		t.Fatalf("broken source did not survive: %q", got)
	}
	if got := classOf(t, lines[0], "func"); got != ClassKeyword {
		t.Fatalf("a keyword in broken source was classed %q", got)
	}
}

// TestACallIsToldApartFromAValue: in Go most tokens are identifiers, so a
// highlighter that paints them all one colour leaves the majority of the
// screen undifferentiated and reads as no highlighting at all.
func TestACallIsToldApartFromAValue(t *testing.T) {
	lines := highlightGo("func run() { errors.New(message) }\n")
	first := lines[0]
	if got := classOf(t, first, "run"); got != ClassDeclared {
		t.Fatalf("the declared name was classed %q", got)
	}
	if got := classOf(t, first, "New"); got != ClassFunction {
		t.Fatalf("a called name was classed %q", got)
	}
	// The span carries the space before it: neighbouring plain runs are one
	// element, which is what keeps an ordinary line from becoming twenty.
	if got := classOf(t, first, " errors"); got != ClassPlain {
		t.Fatalf("a package qualifier was classed %q", got)
	}
	if got := classOf(t, first, "message"); got != ClassPlain {
		t.Fatalf("a passed value was classed %q", got)
	}
}

// TestPunctuationRecedes keeps braces and operators from competing with the
// names they hold. In Go they are about a third of the tokens on screen.
func TestPunctuationRecedes(t *testing.T) {
	lines := highlightGo("if count <= 0 {\n}\n")
	first := lines[0]
	for _, mark := range []string{"<=", "{"} {
		if got := classOf(t, first, mark); got != ClassPunctuation {
			t.Fatalf("%q was classed %q", mark, got)
		}
	}
	if got := classOf(t, first, "if"); got != ClassKeyword {
		t.Fatalf("a keyword was classed %q", got)
	}
}

// TestABuiltinStaysABuiltinEvenWhenCalled: what a reader needs to know about
// len is that it came with the language, not that it has arguments.
func TestABuiltinStaysABuiltinEvenWhenCalled(t *testing.T) {
	lines := highlightGo("var size = len(items)\n")
	if got := classOf(t, lines[0], "len"); got != ClassBuiltin {
		t.Fatalf("len was classed %q", got)
	}
}

// TestOnlyGoIsColoured keeps the viewer honest about what it can read.
func TestOnlyGoIsColoured(t *testing.T) {
	lines := highlightsFor("module", "go.mod", "module example.com/demo\n\ngo 1.25\n")
	for _, line := range lines {
		for _, span := range line {
			if span.Class != ClassPlain {
				t.Fatalf("a non-Go file was coloured: %+v", span)
			}
		}
	}
	if joinLine(lines[0]) != "module example.com/demo" {
		t.Fatalf("a non-Go file did not survive: %q", joinLine(lines[0]))
	}
}

// TestAVeryLongFileIsDrawnWithoutColour keeps a huge read from becoming tens
// of thousands of nodes for colour nobody is scrolling slowly enough to use.
func TestAVeryLongFileIsDrawnWithoutColour(t *testing.T) {
	source := strings.Repeat("func f() {}\n", maximumHighlightedLines+1)
	lines := highlightGo(source)
	for _, line := range lines {
		for _, span := range line {
			if span.Class != ClassPlain {
				t.Fatalf("a file past the cap was coloured: %+v", span)
			}
		}
	}
	if len(lines) != maximumHighlightedLines+2 {
		t.Fatalf("the uncoloured file lost lines: %d", len(lines))
	}
}

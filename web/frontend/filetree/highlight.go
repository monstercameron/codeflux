package filetree

import (
	"go/scanner"
	"go/token"
	"strings"
)

// TokenClass is what a run of source text is, as far as colouring it goes.
type TokenClass string

const (
	// ClassPlain is an identifier or punctuation with nothing to say about
	// itself. Most of a file is this.
	ClassPlain TokenClass = "plain"
	// ClassComment is prose the compiler ignores.
	ClassComment TokenClass = "comment"
	// ClassKeyword is the language's own vocabulary.
	ClassKeyword TokenClass = "keyword"
	// ClassLiteral is a string, rune, or number written into the source.
	ClassLiteral TokenClass = "literal"
	// ClassBuiltin is a predeclared name from the universe block.
	ClassBuiltin TokenClass = "builtin"
	// ClassDeclared is the name a func or method is being given here, which is
	// what a reader scanning for structure is looking for.
	ClassDeclared TokenClass = "declared"
	// ClassFunction is a name being called.
	ClassFunction TokenClass = "function"
	// ClassPunctuation is a brace, bracket, comma, or operator.
	ClassPunctuation TokenClass = "punctuation"
)

// marked is one span of the file the scanner recognised.
type marked struct {
	start, end int
	class      TokenClass
}

// Span is one run of text drawn in one colour.
type Span struct {
	Text  string
	Class TokenClass
}

// predeclared is the universe block: names that are always in scope and are
// worth marking because they are the vocabulary a reader already knows.
var predeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true,
	"complex64": true, "complex128": true, "error": true, "float32": true,
	"float64": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "rune": true, "string": true, "uint": true, "uint8": true,
	"uint16": true, "uint32": true, "uint64": true, "uintptr": true,
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
	"true": true, "false": true, "iota": true, "nil": true,
}

// maximumHighlightedLines is where colouring stops paying for itself.
//
// Every span is a node, and a file long enough to need scrolling for a minute
// is long enough that building tens of thousands of them costs more than the
// colour returns. Past this the file is still drawn, in one colour.
const maximumHighlightedLines = 2000

// highlightGo splits Go source into coloured runs, one slice per line.
//
// It runs the language's own scanner rather than matching patterns, so a "//"
// inside a string stays a string and a keyword inside an identifier stays an
// identifier. The scanner's errors are ignored on purpose: a file mid-edit
// does not parse, and half-coloured code is more useful than none.
func highlightGo(text string) [][]Span {
	lines := strings.Split(text, "\n")
	if len(lines) > maximumHighlightedLines {
		return plainLines(lines)
	}
	set := token.NewFileSet()
	file := set.AddFile("source.go", set.Base(), len(text))
	var source scanner.Scanner
	source.Init(file, []byte(text), func(token.Position, string) {}, scanner.ScanComments)

	// The whole file is scanned before anything is coloured, because what an
	// identifier is depends on what comes after it: a name followed by "(" is
	// a call, and the same name alone is a value.
	var scanned []scannedToken
	for {
		position, tok, literal := source.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && literal == "\n" {
			// The scanner inserts these where the source has a newline. They
			// occupy no text, so colouring one would shift every later span.
			continue
		}
		scanned = append(scanned, scannedToken{
			offset: file.Offset(position), tok: tok, literal: literal,
		})
	}

	marks := make([]marked, 0, len(scanned))
	for index, entry := range scanned {
		width, class := classify(scanned, index)
		if width <= 0 || class == ClassPlain || entry.offset+width > len(text) {
			continue
		}
		marks = append(marks, marked{
			start: entry.offset, end: entry.offset + width, class: class,
		})
	}
	return spanLines(text, marksToSpans(text, marks))
}

// scannedToken is one token with where it starts.
type scannedToken struct {
	offset  int
	tok     token.Token
	literal string
}

// tokenAt answers what is at an index, or ILLEGAL past either end.
func tokenAt(scanned []scannedToken, index int) token.Token {
	if index < 0 || index >= len(scanned) {
		return token.ILLEGAL
	}
	return scanned[index].tok
}

// classify answers how wide a token is and what colour it takes.
//
// The rules are the ones a scanner can be sure of. A name after "func" is
// being declared; a name before "(" is being called; a predeclared name is
// the universe block's. Everything else stays plain rather than guessed at,
// because a highlighter that colours a variable as a type is lying about the
// code it is showing.
func classify(scanned []scannedToken, index int) (int, TokenClass) {
	entry := scanned[index]
	tok, literal := entry.tok, entry.literal
	switch {
	case tok == token.COMMENT:
		return len(literal), ClassComment
	case tok == token.STRING, tok == token.CHAR:
		return len(literal), ClassLiteral
	case tok == token.INT, tok == token.FLOAT, tok == token.IMAG:
		return len(literal), ClassLiteral
	case tok == token.IDENT:
		switch {
		case tokenAt(scanned, index-1) == token.FUNC:
			return len(literal), ClassDeclared
		case predeclared[literal]:
			// Before the call test on purpose: len and make are calls, but
			// what a reader needs to know about them is that they came with
			// the language, not that they have arguments.
			return len(literal), ClassBuiltin
		case tokenAt(scanned, index+1) == token.LPAREN:
			// A conversion reads as a call and is coloured as one; both are a
			// name with arguments after it, which is what the colour means.
			return len(literal), ClassFunction
		}
		return len(literal), ClassPlain
	case tok.IsKeyword():
		return len(tok.String()), ClassKeyword
	case tok.IsOperator():
		return len(tok.String()), ClassPunctuation
	}
	return 0, ClassPlain
}

// marksToSpans fills the gaps between coloured tokens with plain text, so the
// spans together are the file exactly.
func marksToSpans(text string, marks []marked) []Span {
	spans := make([]Span, 0, len(marks)*2+1)
	cursor := 0
	for _, mark := range marks {
		if mark.start < cursor {
			// A token that overlaps the one before it cannot be drawn twice.
			continue
		}
		if mark.start > cursor {
			spans = append(spans, Span{Text: text[cursor:mark.start], Class: ClassPlain})
		}
		spans = append(spans, Span{Text: text[mark.start:mark.end], Class: mark.class})
		cursor = mark.end
	}
	if cursor < len(text) {
		spans = append(spans, Span{Text: text[cursor:], Class: ClassPlain})
	}
	return spans
}

// spanLines cuts spans at newlines so each line can be drawn as its own row,
// which is what the line numbers beside them require.
func spanLines(text string, spans []Span) [][]Span {
	lines := make([][]Span, 0, strings.Count(text, "\n")+1)
	current := []Span{}
	for _, span := range spans {
		parts := strings.Split(span.Text, "\n")
		for index, part := range parts {
			if index > 0 {
				lines = append(lines, current)
				current = []Span{}
			}
			if part != "" {
				current = append(current, Span{Text: part, Class: span.Class})
			}
		}
	}
	return append(lines, current)
}

func plainLines(lines []string) [][]Span {
	result := make([][]Span, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			result = append(result, nil)
			continue
		}
		result = append(result, []Span{{Text: line, Class: ClassPlain}})
	}
	return result
}

// highlightsFor colours a file if the language is one this viewer reads, and
// otherwise hands back its lines unchanged.
func highlightsFor(kind, path, text string) [][]Span {
	if kind == "go" || kind == "go_test" || strings.HasSuffix(path, ".go") {
		return highlightGo(text)
	}
	return plainLines(strings.Split(text, "\n"))
}

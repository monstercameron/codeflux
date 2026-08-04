package executor

import (
	"strings"
	"testing"
)

// TestABareHunkIsToldItIsBare is the message that sent runs looking in the
// wrong place.
//
// "Does not match anything" reads as "your text is wrong", and a run that has
// copied the line correctly then re-checks characters instead of adding the
// context that would locate it. The likelier fault in a hunk of one line is
// that it is one line: nothing around it, so it matches wherever that text
// happens to occur or nowhere at all.
//
// Measured: every one of the 58 no-match failures on ladder rung 16 on
// 2026-08-04 carried exactly one line and no context. 58 of 58 is not a
// tendency, it is the whole failure mode.
func TestABareHunkIsToldItIsBare(t *testing.T) {
	request, err := ParsePatch("*** Update File: main_test.go\n@@\n" +
		"-\tgot := Max([]int{3, 1, 8, 2})\n" +
		"+\tgot := Max([]int{3, 1, 8, 2, 9})\n")
	if err != nil {
		t.Fatalf("this patch is well formed: %v", err)
	}
	if request.Hunks[0].Context != 0 {
		t.Fatalf("the fixture is meant to carry no context, got %d line(s)",
			request.Hunks[0].Context)
	}

	_, _, err = ApplyPatch("package main\n\nfunc TestMax(t *testing.T) {\n"+
		"\ttests := []struct{ values []int }{}\n\t_ = tests\n}\n", request)
	if err == nil {
		t.Fatal("a line the file does not contain must not apply")
	}
	if !strings.Contains(err.Error(), "no context") {
		t.Errorf("the message blames the text rather than the missing "+
			"context, so a run re-checks characters it copied correctly: %v",
			err)
	}
}

// TestAHunkWithContextKeepsTheOlderMessage is the control.
//
// When a hunk does carry context and still does not match, the text really is
// the thing to check — an indent, a renamed identifier, a line that moved. That
// message was right for that case and has to stay.
func TestAHunkWithContextKeepsTheOlderMessage(t *testing.T) {
	request, err := ParsePatch("*** Update File: main.go\n@@\n" +
		" func main() {\n-\tprintln(\"a\")\n+\tprintln(\"b\")\n }\n")
	if err != nil {
		t.Fatalf("this patch is well formed: %v", err)
	}
	if request.Hunks[0].Context == 0 {
		t.Fatal("the fixture is meant to carry context")
	}
	_, _, err = ApplyPatch("package main\n\nfunc other() {}\n", request)
	if err == nil {
		t.Fatal("this hunk matches nothing in that file")
	}
	if strings.Contains(err.Error(), "no context") {
		t.Errorf("a hunk that carries context was told it carries none: %v", err)
	}
	if !strings.Contains(err.Error(), "indentation included") {
		t.Errorf("the message should still point at the text: %v", err)
	}
}

// TestContextIsCountedBothWaysItIsWritten keeps the count honest.
//
// A context line is normally prefixed with one space, and models routinely omit
// it. Both are accepted as context already; both have to be counted as context,
// or the message would call a well-formed hunk bare.
func TestContextIsCountedBothWaysItIsWritten(t *testing.T) {
	spaced, err := ParsePatch("*** Update File: a.go\n@@\n" +
		" func f() {\n-\tx := 1\n+\tx := 2\n }\n")
	if err != nil {
		t.Fatal(err)
	}
	bare, err := ParsePatch("*** Update File: a.go\n@@\n" +
		"func f() {\n-\tx := 1\n+\tx := 2\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if spaced.Hunks[0].Context != bare.Hunks[0].Context {
		t.Errorf("context written without its leading space counted "+
			"differently: %d against %d",
			bare.Hunks[0].Context, spaced.Hunks[0].Context)
	}
	if spaced.Hunks[0].Context != 2 {
		t.Errorf("want 2 context lines, got %d", spaced.Hunks[0].Context)
	}
}

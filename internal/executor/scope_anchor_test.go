package executor

import (
	"strings"
	"testing"
)

// tableDrivenFile is the shape that makes a bare "}" ambiguous: several
// declarations whose closing lines are identical.
const tableDrivenFile = `package main

func TestOne(t *testing.T) {
	for _, c := range cases {
		check(c)
	}
}

func TestTwo(t *testing.T) {
	for _, c := range cases {
		check(c)
	}
}
`

// TestAContextOnlyHunkAnchorsTheHunkAfterIt is the format this tool was not
// reading.
//
// A patch says where a change goes by naming the scope first: a hunk of nothing
// but context, then the hunk that changes something inside it. Applied as an
// ordinary hunk the anchor replaces its own text with itself — no change — and
// the real hunk is then searched for from the top of the file, where "\t}\n}"
// occurs once per test function.
//
// Proven to discriminate: against the previous implementation this records
// "hunk 2 matches 2 places in main_test.go, so it does not say which is meant".
// Ladder rung 9 on 2026-08-03 sent the same patch six times in a row and was
// told to add more context each time, with the context it needed sitting in the
// hunk above; 103 of that run's 109 patch failures were this.
func TestAContextOnlyHunkAnchorsTheHunkAfterIt(t *testing.T) {
	raw := "*** Update File: main_test.go\n" +
		"@@\n" +
		" func TestTwo(t *testing.T) {\n" +
		"@@\n" +
		" \t}\n" +
		" }\n" +
		"+\n" +
		"+func TestThree(t *testing.T) {}\n"

	request, err := ParsePatch(raw)
	if err != nil {
		t.Fatalf("this patch is well formed: %v", err)
	}
	patched, outcome, err := ApplyPatch(tableDrivenFile, request)
	if err != nil {
		t.Fatalf("the anchor says which closing brace is meant: %v", err)
	}
	if !strings.Contains(patched, "func TestThree") {
		t.Fatalf("the change did not land: %q", patched)
	}
	// After TestTwo, which is what the anchor named — not after TestOne.
	if strings.Index(patched, "func TestThree") <
		strings.Index(patched, "func TestTwo") {
		t.Errorf("the change landed before the scope the anchor named:\n%s",
			patched)
	}
	if outcome.LinesAdded != 2 {
		t.Errorf("the anchor is not a change and must not be counted as one, "+
			"got %d line(s) added", outcome.LinesAdded)
	}
}

// TestAHeadingOnTheMarkerLineIsAnAnchorToo is the same anchor in the form the
// format actually documents.
//
// "@@ func TestTwo(t *testing.T) {" names the scope on the marker line itself.
// The whole line was discarded as envelope, so the heading — the one thing that
// disambiguates the hunk — was the one thing thrown away.
func TestAHeadingOnTheMarkerLineIsAnAnchorToo(t *testing.T) {
	raw := "*** Update File: main_test.go\n" +
		"@@ func TestTwo(t *testing.T) {\n" +
		" \t}\n" +
		" }\n" +
		"+\n" +
		"+func TestThree(t *testing.T) {}\n"

	request, err := ParsePatch(raw)
	if err != nil {
		t.Fatalf("this patch is well formed: %v", err)
	}
	patched, _, err := ApplyPatch(tableDrivenFile, request)
	if err != nil {
		t.Fatalf("the heading says which closing brace is meant: %v", err)
	}
	if strings.Index(patched, "func TestThree") <
		strings.Index(patched, "func TestTwo") {
		t.Errorf("the change landed before the scope the heading named:\n%s",
			patched)
	}
}

// TestALineRangeIsNotAnAnchor is the control, and it is what keeps the change
// from turning good patches into refusals.
//
// "@@ -3,7 +3,9 @@" is bookkeeping about line numbers in a file this tool does
// not have. Treating it as context would ask the file to contain a line no file
// contains, which fails every patch written in ordinary unified-diff form.
func TestALineRangeIsNotAnAnchor(t *testing.T) {
	for _, header := range []string{
		"@@", "@@ -3,7 +3,9 @@", "@@ -3,7 +3,9 @@ ", "@@ +1 @@",
	} {
		if got := hunkHeaderAnchor(header); got != "" {
			t.Errorf("%q names no file text, got anchor %q", header, got)
		}
	}
	if got := hunkHeaderAnchor("@@ -3,7 +3,9 @@ func TestTwo(t *testing.T) {"); got !=
		"func TestTwo(t *testing.T) {" {
		t.Errorf("a heading after a closed range is still a heading, got %q", got)
	}
	if got := hunkHeaderAnchor("@@ func TestTwo(t *testing.T) {"); got !=
		"func TestTwo(t *testing.T) {" {
		t.Errorf("a bare heading is a heading, got %q", got)
	}
}

// TestAnUnresolvableAnchorIsNotAFailure is the second control.
//
// An anchor only ever narrows where the next hunk is looked for. If it names
// something the file does not contain — a stale scope, a renamed function — the
// hunk it introduces must still be searched for exactly as it would have been
// without it. An anchor that could refuse a patch would be a new way to fail,
// which is the opposite of the point.
func TestAnUnresolvableAnchorIsNotAFailure(t *testing.T) {
	raw := "*** Update File: main_test.go\n" +
		"@@\n" +
		" func TestNoSuchFunction(t *testing.T) {\n" +
		"@@\n" +
		"-func TestOne(t *testing.T) {\n" +
		"+func TestRenamed(t *testing.T) {\n"

	request, err := ParsePatch(raw)
	if err != nil {
		t.Fatalf("this patch is well formed: %v", err)
	}
	patched, _, err := ApplyPatch(tableDrivenFile, request)
	if err != nil {
		t.Fatalf("an anchor that resolves to nothing must not refuse the "+
			"patch it introduces: %v", err)
	}
	if !strings.Contains(patched, "func TestRenamed") {
		t.Errorf("the change did not land: %q", patched)
	}
}

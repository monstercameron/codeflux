package executor

import (
	"strings"
	"testing"
)

const patchTarget = `package main

import "fmt"

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
	}
}

func run() error {
	return nil
}
`

// TestAPatchChangesOnlyWhatItNames is the whole point: a comment repair should
// move one line, not replace the file.
func TestAPatchChangesOnlyWhatItNames(t *testing.T) {
	request, err := ParsePatch(`*** Update File: cmd/generated/main.go
@@
-func main() {
+// main runs the command and reports any failure.
+func main() {
 	if err := run(); err != nil {
`)
	if err != nil {
		t.Fatalf("a well-formed patch was refused: %v", err)
	}
	if request.Path != "cmd/generated/main.go" {
		t.Errorf("the patch names %q", request.Path)
	}
	patched, outcome, err := ApplyPatch(patchTarget, request)
	if err != nil {
		t.Fatalf("applying failed: %v", err)
	}
	if !strings.Contains(patched, "// main runs the command") {
		t.Error("the comment was not added")
	}
	if !strings.Contains(patched, "func run() error {") {
		t.Error("the patch removed something it did not name")
	}
	if outcome.LinesAdded != 2 || outcome.LinesRemove != 1 {
		t.Errorf("reported +%d/-%d, wanted +2/-1",
			outcome.LinesAdded, outcome.LinesRemove)
	}
	if outcome.BeforeSHA == outcome.AfterSHA {
		t.Error("the outcome reports the same hash before and after")
	}
	// The reply must not be the file.
	if strings.Contains(outcome.Summary(), "package main") {
		t.Errorf("the summary echoes the file back:\n%s", outcome.Summary())
	}
}

// TestAnAmbiguousHunkIsRefused: guessing which of two matches was meant is how
// an edit lands in the wrong function.
func TestAnAmbiguousHunkIsRefused(t *testing.T) {
	repeated := "package main\n\nfunc a() {\n\treturn\n}\n\nfunc b() {\n\treturn\n}\n"
	request, err := ParsePatch("*** Update File: main.go\n@@\n-\treturn\n+\treturn // done\n")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ApplyPatch(repeated, request)
	if err == nil {
		t.Fatal("an ambiguous hunk was applied to one of two matches")
	}
	if !strings.Contains(err.Error(), "matches 2 places") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// TestAStaleHunkIsRefusedWithoutTouchingTheFile covers the case where the file
// moved under the model.
//
// The hunk carries context deliberately. A hunk with none fails for a different
// reason — it cannot be located at all — and is told so in different words, so
// exercising staleness through a bare hunk would have been asserting the wrong
// message about the wrong defect.
func TestAStaleHunkIsRefusedWithoutTouchingTheFile(t *testing.T) {
	request, err := ParsePatch("*** Update File: main.go\n@@\n" +
		" package main\n-func gone() {\n+func gone2() {\n }\n")
	if err != nil {
		t.Fatal(err)
	}
	patched, _, err := ApplyPatch(patchTarget, request)
	if err == nil {
		t.Fatal("a hunk matching nothing was applied")
	}
	if patched != "" {
		t.Error("a refused patch returned modified content")
	}
	if !strings.Contains(err.Error(), "does not match anything") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// TestEveryHunkAppliesOrNoneDoes: a half-applied patch leaves the file in a
// state nobody asked for.
func TestEveryHunkAppliesOrNoneDoes(t *testing.T) {
	request, err := ParsePatch(`*** Update File: main.go
@@
-func run() error {
+func run() (err error) {
@@
-func nothingLikeThis() {
+func norThis() {
`)
	if err != nil {
		t.Fatal(err)
	}
	patched, _, err := ApplyPatch(patchTarget, request)
	if err == nil {
		t.Fatal("a patch whose second hunk cannot apply was accepted")
	}
	if strings.Contains(patched, "(err error)") {
		t.Error("the first hunk was applied even though the second failed")
	}
}

// TestAPatchWithNoContextIsRefused: an insertion says what to add and not
// where, and where is the whole difficulty.
func TestAPatchWithNoContextIsRefused(t *testing.T) {
	if _, err := ParsePatch(
		"*** Update File: main.go\n@@\n+\tfmt.Println(\"hello\")\n",
	); err == nil {
		t.Fatal("a patch with no context was accepted")
	}
}

// TestAPatchMustNameItsFileAndItsHunks covers the frame.
func TestAPatchMustNameItsFileAndItsHunks(t *testing.T) {
	if _, err := ParsePatch("@@\n-a\n+b\n"); err == nil {
		t.Error("a patch naming no file was accepted")
	}
	if _, err := ParsePatch("*** Update File: main.go\nsome prose\n"); err == nil {
		t.Error("a patch with no hunks was accepted")
	}
}

// TestUnifiedDiffHeadersAreUnderstoodToo, because a model that reaches for
// git's spelling should not be punished for it.
func TestUnifiedDiffHeadersAreUnderstoodToo(t *testing.T) {
	request, err := ParsePatch(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -10,3 +10,3 @@
-func run() error {
+func run() (err error) {
`)
	if err != nil {
		t.Fatalf("a git-style patch was refused: %v", err)
	}
	if request.Path != "main.go" {
		t.Errorf("the path read as %q", request.Path)
	}
	if _, _, err := ApplyPatch(patchTarget, request); err != nil {
		t.Errorf("applying a git-style patch failed: %v", err)
	}
}

// TestAContextOnlyHunkIsSkippedNotRefused covers roughly fifteen patches rung 8
// lost.
//
// Models emit a context-only hunk for orientation — a block around the part
// they are about to describe, or a trailing block after the last change — and
// refusing the whole patch for one costs the round. Skipping it cannot change
// the file, which is what makes the forgiveness safe.
func TestAContextOnlyHunkIsSkippedNotRefused(t *testing.T) {
	request, err := ParsePatch(`*** Update File: main.go
@@
 func main() {
 	if err := run(); err != nil {
@@
-func run() error {
+func run() (err error) {
`)
	if err != nil {
		t.Fatalf("a patch with one orientation hunk was refused: %v", err)
	}
	if len(request.Hunks) != 1 {
		t.Fatalf("%d hunk(s) kept, wanted the one that changes something",
			len(request.Hunks))
	}
	patched, outcome, err := ApplyPatch(patchTarget, request)
	if err != nil {
		t.Fatalf("applying failed: %v", err)
	}
	if !strings.Contains(patched, "func run() (err error) {") {
		t.Error("the change did not land")
	}
	if outcome.Hunks != 1 {
		t.Errorf("the outcome reports %d hunk(s)", outcome.Hunks)
	}
}

// TestAPatchThatChangesNothingIsStillRefused keeps the forgiveness bounded: a
// patch describing a file without changing it is not a patch.
func TestAPatchThatChangesNothingIsStillRefused(t *testing.T) {
	if _, err := ParsePatch(
		"*** Update File: main.go\n@@\n func main() {\n@@\n func run() error {\n",
	); err == nil {
		t.Fatal("a patch with no changes at all was accepted")
	}
}

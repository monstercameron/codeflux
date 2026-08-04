package executor

import "testing"

// TestRepeatedContextIsPlacedByHunkOrder is the ambiguity that was refusing
// correct patches.
//
// A unified diff is a sequence: the second hunk belongs after the first. That
// is what makes context like "\t}," unambiguous in a table-driven test where it
// occurs many times. Searching the whole file for every hunk independently asks
// a question the diff does not pose, and answers "ambiguous" to a patch that
// says nothing wrong.
//
// Ladder rung 7 on 2026-08-03 failed twenty of twenty-three patches, and
// forty-seven of about fifty failures were "matches N places, so it does not
// say which is meant".
func TestRepeatedContextIsPlacedByHunkOrder(t *testing.T) {
	// Three identical cases, the shape a table-driven test has.
	existing := "cases := []c{\n" +
		"\t{in: 1, want: 1},\n" +
		"\t{in: 2, want: 2},\n" +
		"\t{in: 3, want: 3},\n" +
		"}\n"
	request := PatchRequest{
		Path: "main_test.go",
		Hunks: []PatchHunk{
			// Both hunks carry the same context line; only their order says
			// which occurrence each one means.
			{Before: "\t{in: 1, want: 1},\n", After: "\t{in: 1, want: 10},\n",
				Added: 1, Removed: 1},
			{Before: "\t{in: 3, want: 3},\n", After: "\t{in: 3, want: 30},\n",
				Added: 1, Removed: 1},
		},
	}

	patched, _, err := ApplyPatch(existing, request)
	if err != nil {
		t.Fatalf("an ordered patch over repeated context was refused: %v", err)
	}
	want := "cases := []c{\n" +
		"\t{in: 1, want: 10},\n" +
		"\t{in: 2, want: 2},\n" +
		"\t{in: 3, want: 30},\n" +
		"}\n"
	if patched != want {
		t.Errorf("patched to:\n%q\nwant:\n%q", patched, want)
	}
}

// TestAHunkTheRemainderCannotPlaceStillSearchesTheWholeFile is the first
// control.
//
// The cursor may only resolve ambiguity; it must never create a failure. A
// patch whose hunks arrive out of order applied before this change and has to
// keep applying.
func TestAHunkTheRemainderCannotPlaceStillSearchesTheWholeFile(t *testing.T) {
	existing := "alpha\nbeta\ngamma\n"
	request := PatchRequest{
		Path: "main.go",
		Hunks: []PatchHunk{
			// Later text first, then earlier: the second hunk is behind the
			// cursor the first one left.
			{Before: "gamma\n", After: "GAMMA\n", Added: 1, Removed: 1},
			{Before: "alpha\n", After: "ALPHA\n", Added: 1, Removed: 1},
		},
	}

	patched, _, err := ApplyPatch(existing, request)
	if err != nil {
		t.Fatalf("an out-of-order patch was refused: %v", err)
	}
	if patched != "ALPHA\nbeta\nGAMMA\n" {
		t.Errorf("patched to %q", patched)
	}
}

// TestGenuinelyAmbiguousContextIsStillRefused is the second control.
//
// Ordering resolves which occurrence a hunk means when the hunks are ordered.
// It does not license guessing: a single hunk whose context matches several
// places in what remains, with nothing to disambiguate it, is still refused
// rather than applied to whichever came first.
func TestGenuinelyAmbiguousContextIsStillRefused(t *testing.T) {
	existing := "same\nsame\nsame\n"
	request := PatchRequest{
		Path:  "main.go",
		Hunks: []PatchHunk{{Before: "same\n", After: "changed\n", Added: 1, Removed: 1}},
	}

	if _, _, err := ApplyPatch(existing, request); err == nil {
		t.Error("a hunk matching three identical places was applied to one of " +
			"them by guesswork")
	}
}

// TestAPatchSpanningTwoFilesIsRefusedNotMisapplied is the defect that made
// rung 7's patches fail in a way nothing could diagnose.
//
// The request carries one path and one flat list of hunks. A second
// "*** Update File:" header used to overwrite the path while its hunks joined
// the same list, so every hunk was searched for in whichever file was named
// last — a hunk written for main.go looked for in main_test.go, reported as
// matching thirty-nine places or nothing at all, with the message naming a file
// the hunk was never about.
func TestAPatchSpanningTwoFilesIsRefusedNotMisapplied(t *testing.T) {
	raw := "*** Begin Patch\n" +
		"*** Update File: cmd/generated/main.go\n@@\n" +
		"-func old() {}\n+func replacement() {}\n" +
		"*** Update File: cmd/generated/main_test.go\n@@\n" +
		"-func TestOld(t *testing.T) {}\n+func TestNew(t *testing.T) {}\n" +
		"*** End Patch\n"

	_, err := ParsePatch(raw)
	if err == nil {
		t.Fatal("a patch changing two files was accepted, so its hunks are " +
			"about to be applied to whichever file was named last")
	}
	// The message has to name both files and say what to do, or the run learns
	// only that something was wrong.
	for _, want := range []string{
		"more than one file",
		"cmd/generated/main.go",
		"cmd/generated/main_test.go",
		"one apply-patch call per file",
	} {
		if !containsText(err.Error(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, err)
		}
	}
}

// TestAPatchNamingOneFileTwiceIsStillOneFile is the control.
//
// Repeating the header for the same file is redundant, not a second file, and
// refusing it would reject patches that are perfectly applicable.
func TestAPatchNamingOneFileTwiceIsStillOneFile(t *testing.T) {
	raw := "*** Update File: cmd/generated/main.go\n@@\n" +
		"-func a() {}\n+func A() {}\n" +
		"*** Update File: cmd/generated/main.go\n@@\n" +
		"-func b() {}\n+func B() {}\n"

	request, err := ParsePatch(raw)
	if err != nil {
		t.Fatalf("a patch naming one file twice was refused: %v", err)
	}
	if request.Path != "cmd/generated/main.go" {
		t.Errorf("path = %q", request.Path)
	}
	if len(request.Hunks) != 2 {
		t.Errorf("%d hunk(s) parsed, want 2", len(request.Hunks))
	}
}

// containsText is strings.Contains, named so the assertions above read as
// assertions rather than as string plumbing.
func containsText(body, want string) bool {
	return len(want) == 0 ||
		len(body) >= len(want) && indexOfText(body, want) >= 0
}

func indexOfText(body, want string) int {
	for index := 0; index+len(want) <= len(body); index++ {
		if body[index:index+len(want)] == want {
			return index
		}
	}
	return -1
}

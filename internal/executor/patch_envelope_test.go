package executor

import (
	"strings"
	"testing"
)

// TestTrailingTextAfterTheEndMarkerIsNotContext is the parse defect that made
// most patches unmatchable.
//
// An unprefixed line inside a hunk is accepted as context written without its
// leading space, which is common and worth accepting. After "*** End Patch" it
// is not context at all: the patch has said it is over. A run that finished its
// patch and then wrote a stray "EOF" — the shape a shell heredoc leaves behind,
// and one models reproduce — had that EOF folded into the last hunk, so the
// hunk was searched for as the file's own text plus a line no file contains.
//
// Proven to discriminate: against the previous implementation the hunk's Before
// ends "...\nEOF" and the patch matches nothing. Ladder rung 9 on 2026-08-03
// failed 24 of its 34 patches, and the tool's own "it was looking for" text
// ends in EOF.
func TestTrailingTextAfterTheEndMarkerIsNotContext(t *testing.T) {
	raw := "*** Begin Patch\n" +
		"*** Update File: main.go\n" +
		"@@\n" +
		" func main() {\n" +
		"-\tprintln(\"old\")\n" +
		"+\tprintln(\"new\")\n" +
		" }\n" +
		"*** End Patch\n" +
		"EOF\n"

	request, err := ParsePatch(raw)
	if err != nil {
		t.Fatalf("this patch is well formed: %v", err)
	}
	if len(request.Hunks) != 1 {
		t.Fatalf("want one hunk, got %d", len(request.Hunks))
	}
	if strings.Contains(request.Hunks[0].Before, "EOF") ||
		strings.Contains(request.Hunks[0].After, "EOF") {
		t.Fatalf("text after the end marker became context, so the hunk asks "+
			"the file to contain a line no file contains:\nbefore=%q\nafter=%q",
			request.Hunks[0].Before, request.Hunks[0].After)
	}

	// And the hunk still says what it was always for.
	file := "package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n"
	patched, _, err := ApplyPatch(file, request)
	if err != nil {
		t.Fatalf("the hunk should apply: %v", err)
	}
	if !strings.Contains(patched, `println("new")`) ||
		strings.Contains(patched, `println("old")`) {
		t.Errorf("the patch did not land: %q", patched)
	}
}

// TestASecondEnvelopeReopensThePatch is the control on the end marker.
//
// Ignoring everything after "*** End Patch" is right for trailing junk and
// wrong for a run that wrapped each file in its own envelope. The second
// envelope's hunks are real, and dropping them silently would lose changes the
// run believes it made — which is the same failure the two-file refusal exists
// to prevent, arrived at from the other direction.
func TestASecondEnvelopeReopensThePatch(t *testing.T) {
	raw := "*** Begin Patch\n" +
		"*** Update File: main.go\n" +
		"@@\n" +
		"-\tprintln(\"one\")\n" +
		"+\tprintln(\"ONE\")\n" +
		"*** End Patch\n" +
		"*** Begin Patch\n" +
		"*** Update File: main.go\n" +
		"@@\n" +
		"-\tprintln(\"two\")\n" +
		"+\tprintln(\"TWO\")\n" +
		"*** End Patch\n"

	request, err := ParsePatch(raw)
	if err != nil {
		t.Fatalf("both envelopes name the same file: %v", err)
	}
	if len(request.Hunks) != 2 {
		t.Fatalf("the second envelope's hunk was dropped: got %d hunk(s)",
			len(request.Hunks))
	}

	file := "package main\n\nfunc main() {\n\tprintln(\"one\")\n\tprintln(\"two\")\n}\n"
	patched, _, err := ApplyPatch(file, request)
	if err != nil {
		t.Fatalf("both hunks should apply: %v", err)
	}
	if !strings.Contains(patched, `println("ONE")`) ||
		!strings.Contains(patched, `println("TWO")`) {
		t.Errorf("both changes should have landed: %q", patched)
	}
}

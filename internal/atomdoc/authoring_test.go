package atomdoc

import "testing"

// TestClassifyAtomDocumentationAuthoring proves M21-101: each plan.md §7
// tier maps to exactly one authoring mode, and Tier 1 graph-native atoms
// (which have no Go implementation of their own) are SQLite-authored rather
// than source-authored.
func TestClassifyAtomDocumentationAuthoring(t *testing.T) {
	cases := []struct {
		tier AtomTier
		want AuthoringMode
	}{
		{AtomTierKernel, AuthoringSourceAuthored},
		{AtomTierModeledGo, AuthoringSourceAuthored},
		{AtomTierExternal, AuthoringSourceAuthored},
		{AtomTierGraphNative, AuthoringSQLiteAuthored},
	}
	for _, testCase := range cases {
		got, err := ClassifyAtomDocumentationAuthoring(testCase.tier)
		if err != nil {
			t.Fatalf("classify %s: %v", testCase.tier, err)
		}
		if got != testCase.want {
			t.Fatalf("tier %s: got %s want %s", testCase.tier, got, testCase.want)
		}
	}
}

// TestClassifyAtomDocumentationAuthoringRejectsUnknownTier proves an unknown
// tier is rejected rather than silently defaulted to an authoring mode.
func TestClassifyAtomDocumentationAuthoringRejectsUnknownTier(t *testing.T) {
	if _, err := ClassifyAtomDocumentationAuthoring(AtomTier("unspecified")); err == nil {
		t.Fatal("expected an error for an unknown atom tier")
	}
}

// TestAtomTierIsValid proves the declared tier set matches plan.md §7
// exactly (four tiers, no more, no fewer).
func TestAtomTierIsValid(t *testing.T) {
	valid := []AtomTier{AtomTierKernel, AtomTierGraphNative, AtomTierModeledGo, AtomTierExternal}
	for _, tier := range valid {
		if !tier.IsValid() {
			t.Fatalf("expected %s to be valid", tier)
		}
	}
	if AtomTier("unspecified").IsValid() {
		t.Fatal("expected an unknown tier to be invalid")
	}
}

// TestAuthoringModeIsValid proves the declared authoring-mode set is closed.
func TestAuthoringModeIsValid(t *testing.T) {
	valid := []AuthoringMode{AuthoringSourceAuthored, AuthoringSQLiteAuthored, AuthoringGeneratedProjection}
	for _, mode := range valid {
		if !mode.IsValid() {
			t.Fatalf("expected %s to be valid", mode)
		}
	}
	if AuthoringMode("unspecified").IsValid() {
		t.Fatal("expected an unknown authoring mode to be invalid")
	}
}

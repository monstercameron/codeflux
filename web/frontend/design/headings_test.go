package design

import (
	"strings"
	"testing"
)

// TestEveryTextRoleSetsAColour backs the promise the repository lint makes.
//
// cmd/codeflux-dev accepts any heading styled through this package without
// reading the helper bodies. If a role stopped setting a colour, the lint would
// go on passing while the heading became legible by accident again — which is
// exactly the defect the lint exists to prevent.
func TestEveryTextRoleSetsAColour(t *testing.T) {
	for _, theme := range []Theme{ThemeLight, ThemeDark, ThemeHighContrast} {
		tokens, err := TokensFor(Options{Theme: theme})
		if err != nil {
			t.Fatal(err)
		}
		roles := append(AllHeadingRoles(),
			// An unrecognised role must also produce a coloured, sized
			// heading rather than an unstyled one: a typo in a role name
			// should degrade to something readable.
			HeadingRole("invented"))
		for _, role := range roles {
			assertReadable(t, theme, string(role), HeadingTreatment(tokens, role))
		}
		assertReadable(t, theme, "prose", ProseTreatment(tokens))
		assertReadable(t, theme, "readout", ReadoutTreatment(tokens))
	}
}

func assertReadable(t *testing.T, theme Theme, name string, treatment TextTreatment) {
	t.Helper()
	if treatment.Color == "" {
		t.Errorf("%s theme, %s sets no colour, so it would inherit one", theme, name)
	}
	if _, err := ParseColor(string(treatment.Color)); err != nil {
		t.Errorf("%s theme, %s colour is not usable: %v", theme, name, err)
	}
	if treatment.Font == "" {
		t.Errorf("%s theme, %s names no font stack", theme, name)
	}
	if treatment.Style.Size <= 0 {
		t.Errorf("%s theme, %s sets no size, so it would render at the browser default",
			theme, name)
	}
	if treatment.Style.LineHeight < treatment.Style.Size {
		t.Errorf("%s theme, %s line height clips its own type", theme, name)
	}
}

// TestTextRolesFollowTheTypeRule keeps the face split meaningful.
//
// Serif introduces material a person reads and judges; sans labels chrome;
// monospace carries what the machine measured. A role that took the wrong face
// would still look fine and would stop the face from telling a reader anything.
func TestTextRolesFollowTheTypeRule(t *testing.T) {
	tokens, err := TokensFor(Options{Theme: ThemeDark})
	if err != nil {
		t.Fatal(err)
	}
	for role, want := range map[HeadingRole]string{
		HeadingPage:      tokens.Fonts.Display,
		HeadingSection:   tokens.Fonts.Display,
		HeadingPanel:     tokens.Fonts.UI,
		HeadingRailLabel: tokens.Fonts.UI,
	} {
		if got := HeadingTreatment(tokens, role).Font; got != want {
			t.Errorf("role %q is set in %q, want %q", role, firstFamily(got), firstFamily(want))
		}
	}
	if got := ProseTreatment(tokens).Font; got != tokens.Fonts.Reading {
		t.Errorf("prose is set in %q, want the reading serif", firstFamily(got))
	}
	if got := ReadoutTreatment(tokens).Font; got != tokens.Fonts.Code {
		t.Errorf("a measured readout is set in %q, want the monospace face", firstFamily(got))
	}
}

// TestProseIsMeasured keeps prose from stretching the full width of a panel.
func TestProseIsMeasured(t *testing.T) {
	tokens, err := TokensFor(Options{})
	if err != nil {
		t.Fatal(err)
	}
	prose := ProseTreatment(tokens)
	if prose.MaxWidthCharacters < 60 || prose.MaxWidthCharacters > 85 {
		t.Errorf("prose measure is %.0f characters; outside 60-85 a reader loses the "+
			"line return", prose.MaxWidthCharacters)
	}
	// A heading is short by nature and does not need a measure; capping one
	// would wrap a title that had no reason to wrap.
	if HeadingTreatment(tokens, HeadingPage).MaxWidthCharacters != 0 {
		t.Error("a page heading was given a prose measure")
	}
}

// TestEveryRoleCompilesToADistinctClass proves the treatments reach CSS.
//
// The class itself is a content hash and says nothing, so what is checked is
// that each role produces one at all and that roles differing in treatment do
// not collapse onto the same class.
func TestEveryRoleCompilesToADistinctClass(t *testing.T) {
	tokens, err := TokensFor(Options{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]HeadingRole{}
	for _, role := range AllHeadingRoles() {
		class := HeadingClass(tokens, role)
		if strings.TrimSpace(class) == "" {
			t.Fatalf("role %q compiled to no class", role)
		}
		if previous, collided := seen[class]; collided {
			t.Errorf("roles %q and %q compiled to the same class, so they are "+
				"visually identical", previous, role)
		}
		seen[class] = role
	}
	if ProseClass(tokens) == ReadoutClass(tokens) {
		t.Error("prose and measured readouts compiled to the same class")
	}
}

// firstFamily returns the first family named in a font stack, for messages.
func firstFamily(stack string) string {
	first, _, _ := strings.Cut(stack, ",")
	return strings.Trim(strings.TrimSpace(first), `"`)
}

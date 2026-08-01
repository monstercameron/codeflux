package design

import "testing"

func TestKindPresentationNeverReliesOnColorAlone(t *testing.T) {
	tokens, err := TokensFor(Options{})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[Color]struct{}, len(KnownKinds()))
	for _, kind := range KnownKinds() {
		presentation, err := KindPresentationFor(kind, tokens)
		if err != nil {
			t.Fatal(err)
		}
		if presentation.Label == "" || presentation.Icon == "" ||
			presentation.Foreground == "" || presentation.Background == "" {
			t.Fatalf("%s presentation = %#v", kind, presentation)
		}
		seen[presentation.Background] = struct{}{}
	}
	if len(seen) != len(KnownKinds()) {
		t.Fatalf("kind backgrounds are not all visually distinct: %d unique of %d kinds", len(seen), len(KnownKinds()))
	}
}

func TestKindPresentationRejectsUnknownKind(t *testing.T) {
	tokens, err := TokensFor(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := KindPresentationFor(Kind("unknown"), tokens); err == nil {
		t.Fatal("unknown kind was accepted")
	}
}

func TestKindPresentationForEveryThemeMeetsWCAGAA(t *testing.T) {
	for _, theme := range []Theme{ThemeLight, ThemeDark, ThemeHighContrast} {
		tokens, err := TokensFor(Options{Theme: theme})
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range KnownKinds() {
			presentation, err := KindPresentationFor(kind, tokens)
			if err != nil {
				t.Fatal(err)
			}
			ratio, err := ContrastRatio(presentation.Foreground, presentation.Background)
			if err != nil {
				t.Fatal(err)
			}
			if ratio+0.000001 < MinimumNormalTextContrast {
				t.Fatalf("%s/%s on-kind contrast = %f", theme, kind, ratio)
			}
		}
	}
}

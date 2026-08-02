package design_test

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/design"
)

// TestEverySyntaxColorCarriesTextInEveryTheme holds the source viewer to the
// same contrast floor as the rest of the console. Code is the one surface a
// person reads for minutes at a time, so a pretty palette that fails at AA
// fails where it matters most.
func TestEverySyntaxColorCarriesTextInEveryTheme(t *testing.T) {
	for _, theme := range []design.Theme{
		design.ThemeDark, design.ThemeLight, design.ThemeHighContrast,
	} {
		tokens, err := design.TokensFor(design.Options{
			Theme: theme, Density: design.DensityComfortable,
		})
		if err != nil {
			t.Fatal(err)
		}
		pairs := design.SyntaxContrastPairs(tokens)
		if len(pairs) == 0 {
			t.Fatalf("%s reported no syntax pairs to check", theme)
		}
		for _, pair := range pairs {
			ratio, err := design.ContrastRatio(pair.Foreground, pair.Background)
			if err != nil {
				t.Fatalf("%s %s: %v", theme, pair.Name, err)
			}
			if ratio < pair.Minimum {
				t.Errorf("%s %s: %.2f is below the %.2f floor",
					theme, pair.Name, ratio, pair.Minimum)
			}
		}
	}
}

// TestSyntaxColorsLeaveTheLiveCyanAlone: Active owns cyan across the console
// so a running stream is identifiable at the edge of vision, and a source
// listing that borrowed it would take that away.
func TestSyntaxColorsLeaveTheLiveCyanAlone(t *testing.T) {
	for _, theme := range []design.Theme{
		design.ThemeDark, design.ThemeLight, design.ThemeHighContrast,
	} {
		tokens, err := design.TokensFor(design.Options{
			Theme: theme, Density: design.DensityComfortable,
		})
		if err != nil {
			t.Fatal(err)
		}
		colors := design.SyntaxColorsFor(tokens)
		for name, value := range map[string]design.Color{
			"comment": colors.Comment, "keyword": colors.Keyword,
			"literal": colors.Literal, "builtin": colors.Builtin,
			"declared": colors.Declared,
		} {
			if value == tokens.Colors.Active {
				t.Errorf("%s %s took the live cyan", theme, name)
			}
		}
	}
}

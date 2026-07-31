package design

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestEveryThemeAndDensityProducesValidCompleteTokens(t *testing.T) {
	for _, theme := range []Theme{
		ThemeLight,
		ThemeDark,
		ThemeHighContrast,
	} {
		for _, density := range []Density{
			DensityComfortable,
			DensityCompact,
		} {
			for _, reduced := range []bool{false, true} {
				tokens, err := TokensFor(Options{
					Theme: theme, Density: density, ReducedMotion: reduced,
				})
				if err != nil {
					t.Fatalf("%s/%s/reduced=%t: %v", theme, density, reduced, err)
				}
				if tokens.Theme != theme || tokens.Density != density ||
					tokens.ReducedMotion != reduced ||
					tokens.Interaction.MinimumPointerTarget != 44 {
					t.Fatalf("tokens = %#v", tokens)
				}
				colorFieldCount := reflect.TypeOf(ColorTokens{}).NumField()
				if len(tokenColorMap(tokens.Colors)) != colorFieldCount {
					t.Fatalf("%s maps %d of %d color tokens", theme, len(tokenColorMap(tokens.Colors)), colorFieldCount)
				}
				if tokens.Rhythm.ControlHeight < tokens.Interaction.MinimumPointerTarget {
					t.Fatalf("%s controls are only %dpx", density, tokens.Rhythm.ControlHeight)
				}
				if reduced && tokens.Motion.Control != 0 {
					t.Fatalf("reduced motion control duration = %s", tokens.Motion.Control)
				}
				if !reduced && tokens.Motion.Control == 0 {
					t.Fatal("ordinary motion unexpectedly disabled")
				}
			}
		}
	}
}

func TestSemanticThemeTokensAreCompleteAndPurposeful(t *testing.T) {
	for _, theme := range []Theme{ThemeLight, ThemeDark, ThemeHighContrast} {
		tokens, err := TokensFor(Options{Theme: theme})
		if err != nil {
			t.Fatal(err)
		}
		colors := tokens.Colors
		colorValue := reflect.ValueOf(colors)
		colorType := colorValue.Type()
		for index := 0; index < colorValue.NumField(); index++ {
			if colorValue.Field(index).String() == "" {
				t.Fatalf("%s theme leaves %s empty", theme, colorType.Field(index).Name)
			}
		}
		for name, color := range tokenColorMap(colors) {
			if color == "" {
				t.Fatalf("%s theme leaves %s empty", theme, name)
			}
			if _, err := ParseColor(string(color)); err != nil {
				t.Fatalf("%s/%s: %v", theme, name, err)
			}
		}
		if colors.Accent == colors.AccentHover ||
			colors.AccentHover == colors.AccentPressed {
			t.Fatalf("%s action states are not visually distinct", theme)
		}
		if colors.Selection == colors.OnSelection {
			t.Fatalf("%s selection foreground and background match", theme)
		}
	}
}

func TestTypeGeometryAndElevationScalesPreserveHierarchy(t *testing.T) {
	tokens, err := TokensFor(Options{})
	if err != nil {
		t.Fatal(err)
	}
	typeScale := []TypeStyle{
		tokens.Typography.WorkspaceTitle,
		tokens.Typography.TaskTitle,
		tokens.Typography.SectionTitle,
		tokens.Typography.PanelHeading,
		tokens.Typography.Body,
	}
	for index := 1; index < len(typeScale); index++ {
		if typeScale[index-1].Size <= typeScale[index].Size {
			t.Fatalf("type scale does not descend: %#v", typeScale)
		}
	}
	for _, style := range []TypeStyle{
		tokens.Typography.WorkspaceTitle,
		tokens.Typography.TaskTitle,
		tokens.Typography.SectionTitle,
		tokens.Typography.PanelHeading,
		tokens.Typography.Body,
		tokens.Typography.CompactBody,
		tokens.Typography.Metadata,
		tokens.Typography.ControlLabel,
		tokens.Typography.MetricValue,
		tokens.Typography.Code,
	} {
		if style.LineHeight < style.Size {
			t.Fatalf("line height clips type: %#v", style)
		}
	}
	if tokens.Geometry.RadiusSmall >= tokens.Geometry.ControlRadius ||
		tokens.Geometry.ControlRadius >= tokens.Geometry.PanelRadius ||
		tokens.Geometry.PanelRadius >= tokens.Geometry.DialogRadius {
		t.Fatalf("radius hierarchy = %#v", tokens.Geometry)
	}
	if tokens.Elevation.Resting.Blur >= tokens.Elevation.Floating.Blur ||
		tokens.Elevation.Floating.Blur >= tokens.Elevation.Modal.Blur {
		t.Fatalf("elevation hierarchy = %#v", tokens.Elevation)
	}
	if tokens.Spacing != (SpacingTokens{XS: 4, SM: 8, MD: 12, LG: 16, XL: 24, XXL: 32}) {
		t.Fatalf("spacing does not preserve the restrained four-pixel cadence: %#v", tokens.Spacing)
	}
	if tokens.Motion.EasingStandard == tokens.Motion.EasingEmphasized {
		t.Fatalf("motion roles share one easing: %#v", tokens.Motion)
	}
}

func TestFixedTokenPairsMeetWCAGAA(t *testing.T) {
	for _, theme := range []Theme{
		ThemeLight,
		ThemeDark,
		ThemeHighContrast,
	} {
		tokens, err := TokensFor(Options{Theme: theme})
		if err != nil {
			t.Fatal(err)
		}
		failures, err := VerifyFixedContrastPairs(tokens)
		if err != nil {
			t.Fatal(err)
		}
		if len(failures) != 0 {
			t.Fatalf("%s contrast failures = %#v", theme, failures)
		}
		if len(FixedContrastPairs(tokens)) < 25 {
			t.Fatalf("%s contrast matrix is incomplete", theme)
		}
	}
}

func TestContrastCalculationPinsWCAGReferenceValues(t *testing.T) {
	ratio, err := ContrastRatio("#000000", "#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if ratio != 21 {
		t.Fatalf("black/white contrast = %f", ratio)
	}
	if _, err := ContrastRatio("#fff", "#000000"); err == nil {
		t.Fatal("short hexadecimal color was accepted")
	}
}

func TestTokenValidationRejectsBrokenVisualHierarchy(t *testing.T) {
	tokens, err := TokensFor(Options{})
	if err != nil {
		t.Fatal(err)
	}
	undersized := tokens
	undersized.Rhythm.ControlHeight = 36
	if err := undersized.Validate(); err == nil {
		t.Fatal("undersized control height was accepted")
	}
	flatType := tokens
	flatType.Typography.SectionTitle.Size = flatType.Typography.TaskTitle.Size
	if err := flatType.Validate(); err == nil {
		t.Fatal("flat typography hierarchy was accepted")
	}
	reversedElevation := tokens
	reversedElevation.Elevation.Floating.Blur = reversedElevation.Elevation.Resting.Blur
	if err := reversedElevation.Validate(); err == nil {
		t.Fatal("reversed elevation hierarchy was accepted")
	}
}

func TestStatusPresentationNeverReliesOnColorAlone(t *testing.T) {
	tokens, err := TokensFor(Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []Status{
		StatusAccent, StatusSuccess, StatusWarning, StatusFailure,
		StatusActive, StatusBlocked, StatusInvalidated, StatusPending,
		StatusPlan, StatusEvidence,
	} {
		presentation, err := StatusPresentationFor(status, tokens)
		if err != nil {
			t.Fatal(err)
		}
		if presentation.Label == "" || presentation.Icon == "" ||
			presentation.Shape == "" || presentation.Foreground == "" ||
			presentation.Background == "" {
			t.Fatalf("%s presentation = %#v", status, presentation)
		}
	}
}

func TestTokenSpecimenIsDevelopmentOnlyAndUsesNoExternalAsset(t *testing.T) {
	disabled, err := ui.RenderToString(TokenSpecimen(SpecimenProps{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(disabled, `state="development-disabled"`) &&
		!strings.Contains(disabled, `data-state="development-disabled"`) {
		t.Fatalf("disabled specimen = %s", disabled)
	}
	ready, err := ui.RenderToString(TokenSpecimen(SpecimenProps{
		Development: true,
		Options: Options{
			Theme: ThemeHighContrast, Density: DensityCompact,
			ReducedMotion: true,
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Codeflux token specimen",
		"Semantic colors",
		"Status redundancy",
		"Elevation hierarchy",
		"Workspace title",
		"Control label",
		`data-theme="high-contrast"`,
		`data-reduced-motion="true"`,
		`data-external-assets="none"`,
	} {
		if !strings.Contains(ready, required) {
			t.Fatalf("specimen lacks %q:\n%s", required, ready)
		}
	}
	if strings.Contains(ready, "http://") || strings.Contains(ready, "https://") {
		t.Fatal("token specimen emitted an external URL")
	}
}

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

func TestInstrumentWorkspacePaletteUsesMineralSurfacesAndCoolSignals(t *testing.T) {
	dark, err := TokensFor(Options{Theme: ThemeDark})
	if err != nil {
		t.Fatal(err)
	}
	light, err := TokensFor(Options{Theme: ThemeLight})
	if err != nil {
		t.Fatal(err)
	}
	darkCanvas, _ := ParseColor(string(dark.Colors.Canvas))
	darkShell, _ := ParseColor(string(dark.Colors.Shell))
	darkSurface1, _ := ParseColor(string(dark.Colors.Surface1))
	darkSurface2, _ := ParseColor(string(dark.Colors.Surface2))
	darkAccent, _ := ParseColor(string(dark.Colors.Accent))
	lightText, _ := ParseColor(string(light.Colors.TextPrimary))
	if darkCanvas == (RGB{}) || darkShell == (RGB{}) {
		t.Fatal("dark mineral surfaces collapsed to pure black")
	}
	if !(relativeLuminance(darkCanvas) < relativeLuminance(darkShell) &&
		relativeLuminance(darkShell) < relativeLuminance(darkSurface1) &&
		relativeLuminance(darkSurface1) < relativeLuminance(darkSurface2)) {
		t.Fatalf("dark mineral surface ladder is not deliberate: %#v", dark.Colors)
	}
	if darkAccent.Blue <= darkAccent.Red || darkAccent.Green <= darkAccent.Red {
		t.Fatalf("signal accent is not in the cool cyan/cobalt family: %s", dark.Colors.Accent)
	}
	if dark.Colors.Accent == dark.Colors.Success || dark.Colors.Plan == dark.Colors.Blocked {
		t.Fatal("action and semantic state colors collapsed into one meaning")
	}
	if lightText.Blue <= lightText.Red || lightText.Green <= lightText.Red {
		t.Fatalf("light theme primary typography is not ink/navy: %s", light.Colors.TextPrimary)
	}
	for _, forbidden := range []Color{"#5ee27b", "#a76bfa", "#ee7bdc", "#147a32", "#6631ad"} {
		for name, color := range tokenColorMap(dark.Colors) {
			if color == forbidden {
				t.Fatalf("dark %s retained the superseded generic accent %s", name, forbidden)
			}
		}
		for name, color := range tokenColorMap(light.Colors) {
			if color == forbidden {
				t.Fatalf("light %s retained the superseded generic accent %s", name, forbidden)
			}
		}
	}
}

func TestInstrumentWorkspaceTypeMotionAndGeometryRemainPrecise(t *testing.T) {
	tokens, err := TokensFor(Options{Theme: ThemeDark})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tokens.Fonts.UI, "Segoe UI Variable Text") ||
		!strings.Contains(tokens.Fonts.Code, "Cascadia Mono") {
		t.Fatalf("system-safe type roles = %#v", tokens.Fonts)
	}
	if !tokens.Typography.MetricValue.Tabular || tokens.Typography.Code.Tabular {
		t.Fatalf("readout/code numeric roles = %#v", tokens.Typography)
	}
	if tokens.Geometry != (GeometryTokens{
		BorderWidth: 1, BorderStrongWidth: 2,
		RadiusSmall: 3, ControlRadius: 4, PanelRadius: 6, DialogRadius: 8,
		PillRadius: 999, FocusRingWidth: 3, FocusRingOffset: 2,
		Shadow: "0 10px 30px -10px rgba(3,12,20,0.34)",
	}) {
		t.Fatalf("instrument geometry drifted: %#v", tokens.Geometry)
	}
	reduced, err := TokensFor(Options{Theme: ThemeDark, ReducedMotion: true})
	if err != nil {
		t.Fatal(err)
	}
	if reduced.Motion.InstantFeedback != 0 || reduced.Motion.Control != 0 ||
		reduced.Motion.Pane != 0 || reduced.Motion.GraphPatch != 0 {
		t.Fatalf("reduced motion retained non-essential duration: %#v", reduced.Motion)
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
	// A role outranks the one below it by size, or by weight when the sizes
	// tie. The tie is deliberate: body prose is serif and set large enough to
	// read comfortably, while a panel heading is a small sans label that names
	// the panel rather than outranking the text inside it. What must never
	// happen is the two reading as equal.
	for index := 1; index < len(typeScale); index++ {
		above, below := typeScale[index-1], typeScale[index]
		if above.Size > below.Size {
			continue
		}
		if above.Size == below.Size && above.Weight > below.Weight {
			continue
		}
		t.Fatalf("type scale does not descend: %#v", typeScale)
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
	// The scale is asserted as a property rather than as literals, so it can
	// be widened for readability without rewriting the test that guards it.
	// What is guarded: every step lands on the four-pixel grid everything else
	// aligns to, each step is larger than the last, and the largest stays
	// bounded so "generous" cannot drift into "unusable".
	steps := []struct {
		name  string
		value int
	}{
		{"XS", tokens.Spacing.XS}, {"SM", tokens.Spacing.SM},
		{"MD", tokens.Spacing.MD}, {"LG", tokens.Spacing.LG},
		{"XL", tokens.Spacing.XL}, {"XXL", tokens.Spacing.XXL},
	}
	for index, step := range steps {
		if step.value%4 != 0 || step.value <= 0 {
			t.Fatalf("spacing %s = %d is off the four-pixel grid", step.name, step.value)
		}
		if index > 0 && step.value <= steps[index-1].value {
			t.Fatalf("spacing %s does not exceed %s: %#v",
				step.name, steps[index-1].name, tokens.Spacing)
		}
	}
	if tokens.Spacing.XXL > 64 {
		t.Fatalf("the largest spacing step is unrestrained: %#v", tokens.Spacing)
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

// TestSerifCarriesJudgementAndMonospaceCarriesMeasurement guards the type
// direction itself.
//
// The three faces are not variety: they tell a reader what kind of thing a
// value is before they read it. Serif means a person wrote it or must judge
// it; monospace means the machine measured it; sans means you act on it. A
// change that collapses the serif roles into the sans one would leave the
// product looking fine and remove the distinction, so it is asserted here
// rather than left to review.
func TestSerifCarriesJudgementAndMonospaceCarriesMeasurement(t *testing.T) {
	for _, theme := range []Theme{ThemeLight, ThemeDark, ThemeHighContrast} {
		tokens, err := TokensFor(Options{Theme: theme})
		if err != nil {
			t.Fatal(err)
		}
		fonts := tokens.Fonts
		for name, stack := range map[string]string{
			"display": fonts.Display,
			"reading": fonts.Reading,
		} {
			if !strings.HasSuffix(strings.TrimSpace(stack), "serif") ||
				strings.Contains(stack, "sans-serif") {
				t.Errorf("%s theme %s role is not a serif stack: %s", theme, name, stack)
			}
		}
		if !strings.HasSuffix(strings.TrimSpace(fonts.UI), "sans-serif") {
			t.Errorf("%s theme UI role is not a sans stack: %s", theme, fonts.UI)
		}
		if !strings.HasSuffix(strings.TrimSpace(fonts.Code), "monospace") {
			t.Errorf("%s theme code role is not a monospace stack: %s", theme, fonts.Code)
		}
		// The three must remain distinct. One stack doing two jobs is the
		// collapse this test exists to catch.
		if fonts.UI == fonts.Display || fonts.UI == fonts.Code || fonts.Display == fonts.Code {
			t.Errorf("%s theme type roles collapsed: %#v", theme, fonts)
		}
	}
}

// TestReadingSpaceIsGenerousEnoughToSeparateClaims guards the negative space.
//
// The console shows several independent claims at once — a forecast, a plan, a
// gate result. Space between them is what stops one being read as qualifying
// another, so the panel rhythm is a correctness property here, not a taste.
func TestReadingSpaceIsGenerousEnoughToSeparateClaims(t *testing.T) {
	comfortable, err := TokensFor(Options{Density: DensityComfortable})
	if err != nil {
		t.Fatal(err)
	}
	if comfortable.Rhythm.PanelInset < 20 {
		t.Errorf("comfortable panel inset is %dpx; panels read as crowded below 20",
			comfortable.Rhythm.PanelInset)
	}
	if comfortable.Rhythm.PanelGap < 16 {
		t.Errorf("comfortable panel gap is %dpx; adjacent claims blur together below 16",
			comfortable.Rhythm.PanelGap)
	}
	// Serif prose needs a looser line than sans of the same size, or the
	// ascenders and descenders of one line touch the next.
	body := comfortable.Typography.Body
	if body.LineHeight*10 < body.Size*15 {
		t.Errorf("body line height %d is tight for %dpx serif prose (want at least 1.5x)",
			body.LineHeight, body.Size)
	}

	compact, err := TokensFor(Options{Density: DensityCompact})
	if err != nil {
		t.Fatal(err)
	}
	if compact.Rhythm.PanelInset >= comfortable.Rhythm.PanelInset ||
		compact.Rhythm.PanelGap >= comfortable.Rhythm.PanelGap {
		t.Error("compact density does not actually tighten the reading rhythm")
	}
	// Density is a reading preference. It must never make a control harder to
	// hit.
	if compact.Rhythm.ControlHeight != comfortable.Rhythm.ControlHeight {
		t.Errorf("compact density changed the control height from %d to %d",
			comfortable.Rhythm.ControlHeight, compact.Rhythm.ControlHeight)
	}
}

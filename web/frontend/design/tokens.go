// Package design defines the versioned, Go-owned visual and interaction
// foundations used by the Codeflux frontend.
package design

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Theme identifies one complete semantic color treatment.
type Theme string

const (
	ThemeLight        Theme = "light"
	ThemeDark         Theme = "dark"
	ThemeHighContrast Theme = "high-contrast"
)

// Density selects a readable information rhythm without changing minimum
// interactive target sizes.
type Density string

const (
	DensityComfortable Density = "comfortable"
	DensityCompact     Density = "compact"
)

// Color is a validated six-digit sRGB color.
type Color string

// ColorTokens separates canvas, content, action, and state meanings so no
// component needs to assign semantic meaning to a literal color.
type ColorTokens struct {
	Canvas        Color
	Shell         Color
	Surface1      Color
	Surface2      Color
	Surface3      Color
	SurfaceRaised Color
	SurfaceInset  Color
	BorderSubtle  Color
	BorderStrong  Color
	TextPrimary   Color
	TextSecondary Color
	TextMuted     Color
	TextDisabled  Color

	Accent        Color
	AccentHover   Color
	AccentPressed Color
	OnAccent      Color
	Link          Color
	Selection     Color
	OnSelection   Color
	Success       Color
	OnSuccess     Color
	Warning       Color
	OnWarning     Color
	Failure       Color
	OnFailure     Color
	Active        Color
	OnActive      Color
	Blocked       Color
	OnBlocked     Color
	Invalidated   Color
	OnInvalidated Color
	Pending       Color
	OnPending     Color
	Plan          Color
	OnPlan        Color
	Evidence      Color
	OnEvidence    Color
	FocusRing     Color

	// Kind accents color-code timeline cards and graph nodes by the typed
	// content category they represent, independent from workflow Status.
	Code         Color
	OnCode       Color
	Test         Color
	OnTest       Color
	Memory       Color
	OnMemory     Color
	Forecast     Color
	OnForecast   Color
	Execution    Color
	OnExecution  Color
	Validation   Color
	OnValidation Color
}

// FontTokens uses only local system stacks.
type FontTokens struct {
	Display string
	UI      string
	Code    string
}

// TypeStyle is one fixed typography role in CSS pixels.
type TypeStyle struct {
	Size       int
	LineHeight int
	Weight     int
	Tabular    bool
}

// TypographyTokens provides the complete initial type scale.
type TypographyTokens struct {
	WorkspaceTitle TypeStyle
	TaskTitle      TypeStyle
	SectionTitle   TypeStyle
	PanelHeading   TypeStyle
	Body           TypeStyle
	CompactBody    TypeStyle
	Metadata       TypeStyle
	ControlLabel   TypeStyle
	MetricValue    TypeStyle
	Code           TypeStyle
}

// SpacingTokens provides the four-pixel-based spacing scale.
type SpacingTokens struct {
	XS  int
	SM  int
	MD  int
	LG  int
	XL  int
	XXL int
}

// GeometryTokens defines restrained boundaries and keyboard visibility.
type GeometryTokens struct {
	BorderWidth       int
	BorderStrongWidth int
	RadiusSmall       int
	ControlRadius     int
	PanelRadius       int
	DialogRadius      int
	PillRadius        int
	FocusRingWidth    int
	FocusRingOffset   int
	Shadow            string
}

// Elevation is a renderer-independent shadow recipe. Components can translate
// it through typed GWC APIs without carrying handwritten style strings.
type Elevation struct {
	OffsetY int
	Blur    int
	Spread  int
	Opacity float64
}

// ElevationTokens keep hierarchy quiet at rest and reserve stronger depth for
// transient layers.
type ElevationTokens struct {
	Flat     Elevation
	Resting  Elevation
	Floating Elevation
	Modal    Elevation
}

// MotionTokens keeps change-related motion bounded and can be reduced to zero.
type MotionTokens struct {
	InstantFeedback  time.Duration
	Control          time.Duration
	Pane             time.Duration
	GraphPatch       time.Duration
	EasingStandard   string
	EasingEmphasized string
}

// InteractionTokens records non-visual accessibility constraints.
type InteractionTokens struct {
	MinimumPointerTarget int
	MinimumBodyText      int
}

// DensityTokens changes list rhythm while preserving readable text and pointer
// targets.
type DensityTokens struct {
	RowHeight     int
	ControlHeight int
	PanelInset    int
	PanelGap      int
}

// Tokens is one complete, immutable design-system selection.
type Tokens struct {
	Theme         Theme
	Density       Density
	ReducedMotion bool
	Colors        ColorTokens
	Fonts         FontTokens
	Typography    TypographyTokens
	Spacing       SpacingTokens
	Geometry      GeometryTokens
	Elevation     ElevationTokens
	Motion        MotionTokens
	Interaction   InteractionTokens
	Rhythm        DensityTokens
}

// Options selects an explicit theme, density, and motion preference.
type Options struct {
	Theme         Theme
	Density       Density
	ReducedMotion bool
}

// TokensFor returns a complete validated semantic token set.
func TokensFor(options Options) (Tokens, error) {
	theme := options.Theme
	if theme == "" {
		theme = ThemeDark
	}
	density := options.Density
	if density == "" {
		density = DensityComfortable
	}
	var colors ColorTokens
	switch theme {
	case ThemeLight:
		colors = lightColors()
	case ThemeDark:
		colors = darkColors()
	case ThemeHighContrast:
		colors = highContrastColors()
	default:
		return Tokens{}, fmt.Errorf("unsupported frontend theme %q", theme)
	}
	var rhythm DensityTokens
	switch density {
	case DensityComfortable:
		rhythm = DensityTokens{
			RowHeight: 44, ControlHeight: 44, PanelInset: 16, PanelGap: 12,
		}
	case DensityCompact:
		rhythm = DensityTokens{
			RowHeight: 36, ControlHeight: 44, PanelInset: 12, PanelGap: 8,
		}
	default:
		return Tokens{}, fmt.Errorf("unsupported frontend density %q", density)
	}
	motion := MotionTokens{
		InstantFeedback:  100 * time.Millisecond,
		Control:          160 * time.Millisecond,
		Pane:             220 * time.Millisecond,
		GraphPatch:       200 * time.Millisecond,
		EasingStandard:   "cubic-bezier(0.2,0,0,1)",
		EasingEmphasized: "cubic-bezier(0.16,1,0.3,1)",
	}
	if options.ReducedMotion {
		motion.InstantFeedback = 0
		motion.Control = 0
		motion.Pane = 0
		motion.GraphPatch = 0
	}
	tokens := Tokens{
		Theme: theme, Density: density, ReducedMotion: options.ReducedMotion,
		Colors: colors,
		Fonts: FontTokens{
			Display: `Bahnschrift,"DIN Alternate","Segoe UI Variable Display","Segoe UI",sans-serif`,
			UI:      `"Segoe UI Variable Text","Segoe UI",ui-sans-serif,system-ui,-apple-system,sans-serif`,
			Code:    `"Cascadia Mono","SFMono-Regular",ui-monospace,Consolas,monospace`,
		},
		Typography: TypographyTokens{
			WorkspaceTitle: TypeStyle{Size: 28, LineHeight: 36, Weight: 600},
			TaskTitle:      TypeStyle{Size: 22, LineHeight: 30, Weight: 600},
			SectionTitle:   TypeStyle{Size: 18, LineHeight: 26, Weight: 600},
			PanelHeading:   TypeStyle{Size: 15, LineHeight: 22, Weight: 600},
			Body:           TypeStyle{Size: 14, LineHeight: 21, Weight: 400},
			CompactBody:    TypeStyle{Size: 13, LineHeight: 19, Weight: 400},
			Metadata:       TypeStyle{Size: 12, LineHeight: 17, Weight: 500},
			ControlLabel:   TypeStyle{Size: 13, LineHeight: 18, Weight: 600},
			MetricValue: TypeStyle{
				Size: 17, LineHeight: 23, Weight: 600, Tabular: true,
			},
			Code: TypeStyle{Size: 13, LineHeight: 19, Weight: 400},
		},
		Spacing: SpacingTokens{XS: 4, SM: 8, MD: 12, LG: 16, XL: 24, XXL: 32},
		Geometry: GeometryTokens{
			BorderWidth: 1, BorderStrongWidth: 2,
			RadiusSmall: 3, ControlRadius: 4, PanelRadius: 6,
			DialogRadius: 8, PillRadius: 999,
			FocusRingWidth: 3, FocusRingOffset: 2,
			Shadow: "0 10px 30px -10px rgba(3,12,20,0.34)",
		},
		Elevation: ElevationTokens{
			Flat:     Elevation{},
			Resting:  Elevation{OffsetY: 1, Blur: 2, Opacity: 0.08},
			Floating: Elevation{OffsetY: 10, Blur: 28, Spread: -8, Opacity: 0.24},
			Modal:    Elevation{OffsetY: 22, Blur: 56, Spread: -16, Opacity: 0.38},
		},
		Motion: motion,
		Interaction: InteractionTokens{
			MinimumPointerTarget: 44,
			MinimumBodyText:      12,
		},
		Rhythm: rhythm,
	}
	return tokens, tokens.Validate()
}

// Validate rejects incomplete, remote-font, or accessibility-unsafe tokens.
func (tokens Tokens) Validate() error {
	if tokens.Theme == "" || tokens.Density == "" {
		return errors.New("theme and density are required")
	}
	for name, value := range tokenColorMap(tokens.Colors) {
		if _, err := ParseColor(string(value)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if tokens.Interaction.MinimumPointerTarget < 44 {
		return errors.New("minimum pointer target must be at least 44 CSS pixels")
	}
	if tokens.Typography.CompactBody.Size <
		tokens.Interaction.MinimumBodyText {
		return errors.New("compact body type is below the minimum")
	}
	if err := validateTypography(tokens.Typography); err != nil {
		return err
	}
	if tokens.Rhythm.ControlHeight < tokens.Interaction.MinimumPointerTarget {
		return errors.New("density control height is below the pointer minimum")
	}
	if err := validateGeometry(tokens.Geometry, tokens.Elevation); err != nil {
		return err
	}
	for _, stack := range []string{tokens.Fonts.Display, tokens.Fonts.UI, tokens.Fonts.Code} {
		lower := strings.ToLower(stack)
		if strings.Contains(lower, "url(") ||
			strings.Contains(lower, "http://") ||
			strings.Contains(lower, "https://") {
			return errors.New("font stacks must not request remote assets")
		}
	}
	if tokens.ReducedMotion &&
		(tokens.Motion.InstantFeedback != 0 ||
			tokens.Motion.Control != 0 ||
			tokens.Motion.Pane != 0 ||
			tokens.Motion.GraphPatch != 0) {
		return errors.New("reduced motion must disable transition durations")
	}
	if !tokens.ReducedMotion &&
		(tokens.Motion.InstantFeedback < 80*time.Millisecond ||
			tokens.Motion.InstantFeedback > 120*time.Millisecond ||
			tokens.Motion.Control < 140*time.Millisecond ||
			tokens.Motion.Control > 180*time.Millisecond ||
			tokens.Motion.Pane < 180*time.Millisecond ||
			tokens.Motion.Pane > 240*time.Millisecond ||
			tokens.Motion.GraphPatch < 180*time.Millisecond ||
			tokens.Motion.GraphPatch > 240*time.Millisecond) {
		return errors.New("motion durations are outside the restrained interaction bands")
	}
	if tokens.Motion.EasingStandard == "" || tokens.Motion.EasingEmphasized == "" {
		return errors.New("motion easing tokens are required")
	}
	return nil
}

func darkColors() ColorTokens {
	return ColorTokens{
		Canvas: "#0a0f0d", Shell: "#0d1614",
		Surface1: "#111e1a", Surface2: "#16241f", Surface3: "#1c2c26",
		SurfaceRaised: "#223530", SurfaceInset: "#070c0a",
		BorderSubtle: "#253530", BorderStrong: "#6a8a7c",
		TextPrimary: "#eaf3ee", TextSecondary: "#aec4ba", TextMuted: "#86988e",
		TextDisabled: "#5e7268",
		Accent:       "#29c46a", AccentHover: "#4fda88", AccentPressed: "#1c9a52",
		OnAccent: "#05170d", Link: "#6fd1ff",
		Selection: "#163a28", OnSelection: "#dffbea",
		Success: "#34d399", OnSuccess: "#052015",
		Warning: "#e3b341", OnWarning: "#1d1400",
		Failure: "#f0667a", OnFailure: "#240509",
		Active: "#4fa8ff", OnActive: "#041526",
		Blocked: "#f0708a", OnBlocked: "#240509",
		Invalidated: "#c79aa8", OnInvalidated: "#1d0f14",
		Pending: "#93a79c", OnPending: "#0a1310",
		Plan: "#a78bfa", OnPlan: "#170b2e",
		Evidence: "#e3a83e", OnEvidence: "#1d1200",
		FocusRing: "#66e3a0",
		Code:      "#2fe0a0", OnCode: "#04160f",
		Test: "#4c9efa", OnTest: "#041526",
		Memory: "#93a0b0", OnMemory: "#0b1218",
		Forecast: "#d7cb4a", OnForecast: "#201c02",
		Execution: "#23b563", OnExecution: "#04160d",
		Validation: "#2bc8cb", OnValidation: "#02191a",
	}
}

func lightColors() ColorTokens {
	return ColorTokens{
		Canvas: "#f4f7f5", Shell: "#eef3ef",
		Surface1: "#ffffff", Surface2: "#f6f9f7", Surface3: "#eaf0ec",
		SurfaceRaised: "#ffffff", SurfaceInset: "#e3ebe6",
		BorderSubtle: "#d3e0d8", BorderStrong: "#5c7568",
		TextPrimary: "#10201a", TextSecondary: "#33453d", TextMuted: "#4c5f56",
		TextDisabled: "#71847a",
		Accent:       "#178246", AccentHover: "#146c3a", AccentPressed: "#0f5730",
		OnAccent: "#ffffff", Link: "#0b6fae",
		Selection: "#d6f3e2", OnSelection: "#0d3822",
		Success: "#167a41", OnSuccess: "#ffffff",
		Warning: "#8a5c00", OnWarning: "#ffffff",
		Failure: "#b23347", OnFailure: "#ffffff",
		Active: "#1c66c2", OnActive: "#ffffff",
		Blocked: "#a23a55", OnBlocked: "#ffffff",
		Invalidated: "#7a4f5c", OnInvalidated: "#ffffff",
		Pending: "#44584f", OnPending: "#ffffff",
		Plan: "#6748c7", OnPlan: "#ffffff",
		Evidence: "#8a6110", OnEvidence: "#ffffff",
		FocusRing: "#14824a",
		Code:      "#0e6f42", OnCode: "#ffffff",
		Test: "#1a63c9", OnTest: "#ffffff",
		Memory: "#4c5e6b", OnMemory: "#ffffff",
		Forecast: "#7a6c00", OnForecast: "#ffffff",
		Execution: "#106b3f", OnExecution: "#ffffff",
		Validation: "#0f7f83", OnValidation: "#ffffff",
	}
}

func highContrastColors() ColorTokens {
	return ColorTokens{
		Canvas: "#000000", Shell: "#000000",
		Surface1: "#000000", Surface2: "#0d0d0d", Surface3: "#181818",
		SurfaceRaised: "#000000", SurfaceInset: "#000000",
		BorderSubtle: "#ffffff", BorderStrong: "#ffffff",
		TextPrimary: "#ffffff", TextSecondary: "#ffffff", TextMuted: "#e6e6e6",
		TextDisabled: "#d0d0d0",
		Accent:       "#5ee8ff", AccentHover: "#9af1ff", AccentPressed: "#33cce8",
		OnAccent: "#000000", Link: "#8ed1ff",
		Selection: "#ffffff", OnSelection: "#000000",
		Success: "#71f2b5", OnSuccess: "#000000",
		Warning: "#ffd75e", OnWarning: "#000000",
		Failure: "#ff8fa3", OnFailure: "#000000",
		Active: "#79c7ff", OnActive: "#000000",
		Blocked: "#ff9caf", OnBlocked: "#000000",
		Invalidated: "#ffd0df", OnInvalidated: "#000000",
		Pending: "#e6e6e6", OnPending: "#000000",
		Plan: "#9eb7ff", OnPlan: "#000000",
		Evidence: "#ffe27a", OnEvidence: "#000000",
		FocusRing: "#ffff00",
		Code:      "#63ffbe", OnCode: "#000000",
		Test: "#7fc4ff", OnTest: "#000000",
		Memory: "#d6dfe9", OnMemory: "#000000",
		Forecast: "#fff176", OnForecast: "#000000",
		Execution: "#74ffb8", OnExecution: "#000000",
		Validation: "#6ff5ff", OnValidation: "#000000",
	}
}

func tokenColorMap(colors ColorTokens) map[string]Color {
	return map[string]Color{
		"canvas": colors.Canvas, "shell": colors.Shell,
		"surface-1": colors.Surface1, "surface-2": colors.Surface2,
		"surface-3": colors.Surface3, "surface-raised": colors.SurfaceRaised,
		"surface-inset": colors.SurfaceInset, "border-subtle": colors.BorderSubtle,
		"border-strong":  colors.BorderStrong,
		"text-primary":   colors.TextPrimary,
		"text-secondary": colors.TextSecondary, "text-muted": colors.TextMuted,
		"text-disabled": colors.TextDisabled,
		"accent":        colors.Accent, "accent-hover": colors.AccentHover,
		"accent-pressed": colors.AccentPressed, "on-accent": colors.OnAccent,
		"link": colors.Link, "selection": colors.Selection,
		"on-selection": colors.OnSelection,
		"success":      colors.Success, "on-success": colors.OnSuccess,
		"warning": colors.Warning, "on-warning": colors.OnWarning,
		"failure": colors.Failure, "on-failure": colors.OnFailure,
		"active": colors.Active, "on-active": colors.OnActive,
		"blocked": colors.Blocked, "on-blocked": colors.OnBlocked,
		"invalidated":    colors.Invalidated,
		"on-invalidated": colors.OnInvalidated,
		"pending":        colors.Pending, "on-pending": colors.OnPending,
		"plan": colors.Plan, "on-plan": colors.OnPlan,
		"evidence": colors.Evidence, "on-evidence": colors.OnEvidence,
		"focus-ring": colors.FocusRing,
		"code":       colors.Code, "on-code": colors.OnCode,
		"test": colors.Test, "on-test": colors.OnTest,
		"memory": colors.Memory, "on-memory": colors.OnMemory,
		"forecast": colors.Forecast, "on-forecast": colors.OnForecast,
		"execution": colors.Execution, "on-execution": colors.OnExecution,
		"validation": colors.Validation, "on-validation": colors.OnValidation,
	}
}

func validateTypography(typography TypographyTokens) error {
	styles := map[string]TypeStyle{
		"workspace title": typography.WorkspaceTitle,
		"task title":      typography.TaskTitle,
		"section title":   typography.SectionTitle,
		"panel heading":   typography.PanelHeading,
		"body":            typography.Body,
		"compact body":    typography.CompactBody,
		"metadata":        typography.Metadata,
		"control label":   typography.ControlLabel,
		"metric value":    typography.MetricValue,
		"code":            typography.Code,
	}
	for name, style := range styles {
		if style.Size < 11 || style.LineHeight < style.Size ||
			style.Weight < 400 || style.Weight > 700 {
			return fmt.Errorf("%s typography is outside the supported scale", name)
		}
	}
	if typography.WorkspaceTitle.Size <= typography.TaskTitle.Size ||
		typography.TaskTitle.Size <= typography.SectionTitle.Size ||
		typography.SectionTitle.Size <= typography.PanelHeading.Size ||
		typography.PanelHeading.Size <= typography.Body.Size {
		return errors.New("typography hierarchy must descend by semantic importance")
	}
	return nil
}

func validateGeometry(geometry GeometryTokens, elevations ElevationTokens) error {
	if geometry.BorderWidth != 1 ||
		geometry.RadiusSmall <= 0 ||
		geometry.ControlRadius < geometry.RadiusSmall ||
		geometry.PanelRadius < geometry.ControlRadius ||
		geometry.DialogRadius < geometry.PanelRadius ||
		geometry.PillRadius < geometry.DialogRadius ||
		geometry.FocusRingWidth < 2 ||
		geometry.FocusRingOffset < 1 {
		return errors.New("geometry scale is incomplete or inaccessible")
	}
	for _, elevation := range []Elevation{
		elevations.Flat,
		elevations.Resting,
		elevations.Floating,
		elevations.Modal,
	} {
		if elevation.OffsetY < 0 || elevation.Blur < 0 ||
			elevation.Opacity < 0 || elevation.Opacity > 1 {
			return errors.New("elevation recipe is invalid")
		}
	}
	if elevations.Resting.Blur >= elevations.Floating.Blur ||
		elevations.Floating.Blur >= elevations.Modal.Blur {
		return errors.New("elevation hierarchy must increase by layer")
	}
	return nil
}

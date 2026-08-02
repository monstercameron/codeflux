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
//
// The three roles encode a distinction this product depends on, rather than
// decorating with variety:
//
//   - Display and Reading are serif. They carry what a person wrote or must
//     judge: a task title, a plan, an assistant's account of what it did, an
//     evidence claim.
//   - UI is sans. It carries controls, labels, and navigation — the parts you
//     act on rather than read.
//   - Code is monospace. It carries what the machine measured: costs,
//     durations, counts, identities, log lines, diffs.
//
// So the face a value is set in tells you what kind of thing it is. A cost
// rendered in serif, or a claim rendered in monospace, would be a category
// error a reader can see before reading the words.
type FontTokens struct {
	// Display sets titles and other short serif headings.
	Display string
	// Reading sets serif prose: descriptions, plans, narration, claims.
	Reading string
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
		// Panels are inset generously and separated widely. The console shows
		// several independent claims at once — a forecast, a plan, a gate
		// result — and space between them is what stops one being read as
		// qualifying another.
		rhythm = DensityTokens{
			RowHeight: 48, ControlHeight: 44, PanelInset: 24, PanelGap: 20,
		}
	case DensityCompact:
		// Compact tightens the rhythm but never the pointer target: the whole
		// point of the separate control height is that density is a reading
		// preference, not a reason to make things harder to hit.
		rhythm = DensityTokens{
			RowHeight: 36, ControlHeight: 44, PanelInset: 16, PanelGap: 12,
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
			// Sitka is a humanist screen serif that ships with Windows. It was
			// drawn for reading at interface sizes, so it holds an even stroke
			// on a dark ground where a high-contrast display serif would lose
			// its hairlines, and it is optically sized: the Heading cut carries
			// titles, the Text cut carries prose.
			Display: `"Sitka Heading","Sitka Display",Constantia,` +
				`"Iowan Old Style",Georgia,"Noto Serif",serif`,
			Reading: `"Sitka Text",Sitka,Constantia,` +
				`"Iowan Old Style",Georgia,"Noto Serif",serif`,
			UI:   `"Segoe UI Variable Text","Segoe UI",ui-sans-serif,system-ui,-apple-system,sans-serif`,
			Code: `"Cascadia Mono","Cascadia Code","SFMono-Regular",ui-monospace,Consolas,monospace`,
		},
		Typography: TypographyTokens{
			// Serif roles are set lighter and looser than their sans
			// equivalents would be. A serif at weight 600 on a dark ground
			// reads as shouting; the size carries the hierarchy instead.
			WorkspaceTitle: TypeStyle{Size: 34, LineHeight: 42, Weight: 400},
			TaskTitle:      TypeStyle{Size: 26, LineHeight: 34, Weight: 400},
			SectionTitle:   TypeStyle{Size: 19, LineHeight: 26, Weight: 400},
			// A panel heading names the panel; it must not outrank the prose
			// inside it. It shares the body size and separates itself by
			// weight, which is also what keeps the hierarchy validator honest.
			PanelHeading: TypeStyle{Size: 15, LineHeight: 20, Weight: 700},
			Body:         TypeStyle{Size: 15, LineHeight: 25, Weight: 400},
			CompactBody:  TypeStyle{Size: 13, LineHeight: 20, Weight: 400},
			// Metadata is the small tracked sans eyebrow used for field labels
			// and section markers. It is the one role set below body size.
			Metadata:     TypeStyle{Size: 11, LineHeight: 16, Weight: 600},
			ControlLabel: TypeStyle{Size: 13, LineHeight: 18, Weight: 600},
			MetricValue: TypeStyle{
				Size: 22, LineHeight: 28, Weight: 500, Tabular: true,
			},
			Code: TypeStyle{Size: 13, LineHeight: 20, Weight: 400},
		},
		// The scale is wider than the four-pixel grid it grew from. A
		// supervision console is read under pressure, and the thing that makes
		// it readable is space between groups, not more information per row.
		Spacing: SpacingTokens{XS: 4, SM: 8, MD: 16, LG: 24, XL: 40, XXL: 64},
		Geometry: GeometryTokens{
			BorderWidth: 1, BorderStrongWidth: 2,
			// Boundaries are hairlines and the corners are tight. A console is
			// an instrument: a generous radius reads as soft where this needs
			// to read as machined, and at small sizes it eats the corner of
			// every dense row. Radius grows with the surface, never past the
			// point where a rectangle stops looking like one.
			RadiusSmall: 2, ControlRadius: 5, PanelRadius: 8,
			DialogRadius: 12, PillRadius: 999,
			FocusRingWidth: 3, FocusRingOffset: 2,
			Shadow: "0 24px 60px -24px rgba(2,6,14,0.60)",
		},
		Elevation: ElevationTokens{
			Flat:     Elevation{},
			Resting:  Elevation{OffsetY: 1, Blur: 3, Opacity: 0.10},
			Floating: Elevation{OffsetY: 16, Blur: 40, Spread: -12, Opacity: 0.34},
			Modal:    Elevation{OffsetY: 30, Blur: 78, Spread: -22, Opacity: 0.50},
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
	for _, stack := range []string{
		tokens.Fonts.Display, tokens.Fonts.Reading, tokens.Fonts.UI, tokens.Fonts.Code,
	} {
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

// darkColors is the instrument treatment: a cold graphite console where color
// is reserved for state.
//
// Two rules hold the palette together. Interactive surfaces are neutral — the
// primary action is a warm paper key, not a colored one — so that saturated
// color anywhere in the console means a machine state and nothing else. And
// live delivery owns cyan alone: it is the only place that hue appears, so a
// running stream is identifiable at the edge of vision.
func darkColors() ColorTokens {
	return ColorTokens{
		Canvas: "#090c11", Shell: "#0e121a",
		Surface1: "#141a24", Surface2: "#1a2130", Surface3: "#222b3a",
		SurfaceRaised: "#1b2331", SurfaceInset: "#05070b",
		BorderSubtle: "#263043", BorderStrong: "#7c8ca6",
		TextPrimary: "#e9eef7", TextSecondary: "#aab7ca", TextMuted: "#8a97ab",
		TextDisabled: "#5f6b7d",
		// The paper key: the one action surface, borrowed from the record
		// column so the thing you press belongs to the same material as the
		// thing you are reading.
		Accent: "#e8e3d6", AccentHover: "#f5f1e6", AccentPressed: "#cfc9bb",
		OnAccent: "#10141c", Link: "#7cc4ff",
		Selection: "#142334", OnSelection: "#dce9fa",
		Success: "#46d39a", OnSuccess: "#04150e",
		Warning: "#f5a524", OnWarning: "#1a1200",
		Failure: "#ff6f7d", OnFailure: "#23060b",
		Active: "#47d6e6", OnActive: "#04191d",
		Blocked: "#ff8a9b", OnBlocked: "#23060b",
		Invalidated: "#c9a6b4", OnInvalidated: "#1b1014",
		Pending: "#94a3b8", OnPending: "#0b1119",
		Plan: "#a79bf7", OnPlan: "#15102e",
		Evidence: "#e3b04b", OnEvidence: "#1b1200",
		FocusRing: "#58e0f0",
		Code:      "#7fd8a8", OnCode: "#04170e",
		Test: "#7fb6ff", OnTest: "#041526",
		Memory: "#a9b6c8", OnMemory: "#0b1119",
		Forecast: "#d9ce6a", OnForecast: "#1c1a02",
		Execution: "#58d6c4", OnExecution: "#031a17",
		Validation: "#63c9e8", OnValidation: "#02181f",
	}
}

// lightColors is the same instrument in daylight: the shell cools to pale
// graphite, the action key inverts to ink, and every state hue is darkened to
// carry text at AA on a light ground.
func lightColors() ColorTokens {
	return ColorTokens{
		Canvas: "#edf0f5", Shell: "#e4e9f0",
		Surface1: "#fbfcfe", Surface2: "#f2f5f9", Surface3: "#e7ecf3",
		SurfaceRaised: "#ffffff", SurfaceInset: "#dfe5ee",
		BorderSubtle: "#ccd5e1", BorderStrong: "#5a6879",
		TextPrimary: "#10151d", TextSecondary: "#38424f", TextMuted: "#4e5a69",
		TextDisabled: "#78838f",
		// The ink key: the daylight inverse of the paper key.
		Accent: "#16202c", AccentHover: "#22303f", AccentPressed: "#0c121a",
		OnAccent: "#f7f4ec", Link: "#0a63b0",
		Selection: "#dce8f7", OnSelection: "#0b2438",
		Success: "#10774b", OnSuccess: "#ffffff",
		Warning: "#8a5a00", OnWarning: "#ffffff",
		Failure: "#b3253c", OnFailure: "#ffffff",
		Active: "#0f6e86", OnActive: "#ffffff",
		Blocked: "#a3304a", OnBlocked: "#ffffff",
		Invalidated: "#7a5060", OnInvalidated: "#ffffff",
		Pending: "#4a5866", OnPending: "#ffffff",
		Plan: "#5b44c0", OnPlan: "#ffffff",
		Evidence: "#8a6110", OnEvidence: "#ffffff",
		FocusRing: "#0c6e86",
		Code:      "#0e6f42", OnCode: "#ffffff",
		Test: "#1a5fc0", OnTest: "#ffffff",
		Memory: "#4c5e6b", OnMemory: "#ffffff",
		Forecast: "#6f6300", OnForecast: "#ffffff",
		Execution: "#0d6f66", OnExecution: "#ffffff",
		Validation: "#0f6e86", OnValidation: "#ffffff",
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
	// Hierarchy descends by size, and where two roles share a size it descends
	// by weight instead.
	//
	// The tie case is real rather than a loophole. Body prose is set in a
	// serif at a size that makes it comfortable to read; a panel heading is a
	// small sans label whose job is to name the panel, not to outrank the
	// text inside it. Requiring the heading to be physically larger would
	// force it to shout. What must never happen is the two reading as equal,
	// which the weight rule prevents.
	descending := []struct {
		name  string
		style TypeStyle
	}{
		{"workspace title", typography.WorkspaceTitle},
		{"task title", typography.TaskTitle},
		{"section title", typography.SectionTitle},
		{"panel heading", typography.PanelHeading},
		{"body", typography.Body},
	}
	for index := 1; index < len(descending); index++ {
		above, below := descending[index-1], descending[index]
		if above.style.Size > below.style.Size {
			continue
		}
		if above.style.Size == below.style.Size && above.style.Weight > below.style.Weight {
			continue
		}
		return fmt.Errorf(
			"typography hierarchy must descend by semantic importance: %s does not outrank %s",
			above.name, below.name)
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

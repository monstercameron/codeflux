package design

import (
	"github.com/monstercameron/GoWebComponents/v5/css"
)

// HeadingRole names what a heading is doing, so its treatment follows from its
// job rather than from whichever component happened to render it.
//
// The roles split along the same line the type faces do. A heading that
// introduces material a person reads and judges takes the serif; a heading that
// labels a piece of interface chrome takes the sans. Getting this from a role
// rather than from a local helper is what stops the same heading looking like
// two different things in two panels.
type HeadingRole string

const (
	// HeadingPage titles a whole route. One per page.
	HeadingPage HeadingRole = "page"
	// HeadingSection titles a body of material: a task graph, a plan, a
	// report. Serif, because the material under it is read.
	HeadingSection HeadingRole = "section"
	// HeadingPanel names a panel of interface chrome. Sans, because it labels
	// controls rather than introducing prose.
	HeadingPanel HeadingRole = "panel"
	// HeadingRailLabel names a list in a rail. Small, tracked, and quiet: it
	// tells you what the list is without competing with the list.
	HeadingRailLabel HeadingRole = "rail-label"
)

// AllHeadingRoles returns every declared role.
func AllHeadingRoles() []HeadingRole {
	return []HeadingRole{HeadingPage, HeadingSection, HeadingPanel, HeadingRailLabel}
}

// TextTreatment is one resolved typographic decision.
//
// It exists as data rather than only as a generated class because the class a
// css helper returns is a content hash: nothing can be asserted about it. The
// repository lint accepts any heading styled through this package without
// reading the helper bodies, and that trust is only sound if the decisions
// themselves can be checked. They can be, here.
type TextTreatment struct {
	// Font is the stack this text is set in.
	Font string
	// Style is the size, line height, and weight.
	Style TypeStyle
	// Color is always set. A heading that inherits its colour is legible by
	// accident; this product shipped one that rendered near-white on
	// near-white in the light theme.
	Color Color
	// TrackingEms adjusts letter spacing.
	TrackingEms float64
	// Uppercase marks a label that is set in capitals.
	Uppercase bool
	// MaxWidthCharacters bounds a measure, and is zero where none applies.
	MaxWidthCharacters float64
}

// HeadingTreatment resolves one heading role.
//
// An unrecognised role falls through to the quietest treatment rather than to
// nothing: a typo in a role name should produce a small readable label, not an
// unstyled browser heading.
func HeadingTreatment(tokens Tokens, role HeadingRole) TextTreatment {
	switch role {
	case HeadingPage:
		return TextTreatment{
			Font:  tokens.Fonts.Display,
			Style: tokens.Typography.TaskTitle,
			Color: tokens.Colors.TextPrimary,
			// A hair of positive tracking. Serifs at display size close up
			// when tracked the way a sans is.
			TrackingEms: 0.004,
		}
	case HeadingSection:
		return TextTreatment{
			Font:  tokens.Fonts.Display,
			Style: tokens.Typography.SectionTitle,
			Color: tokens.Colors.TextPrimary,
		}
	case HeadingPanel:
		return TextTreatment{
			Font:  tokens.Fonts.UI,
			Style: tokens.Typography.PanelHeading,
			Color: tokens.Colors.TextPrimary,
		}
	default:
		return TextTreatment{
			Font:        tokens.Fonts.UI,
			Style:       tokens.Typography.Metadata,
			Color:       tokens.Colors.TextMuted,
			TrackingEms: 0.09,
			Uppercase:   true,
		}
	}
}

// ProseTreatment resolves a block of text a person reads and judges.
//
// Serif, at a measured width. A line of serif much past eighty characters
// loses the reader between the end of one line and the start of the next, and
// this product's prose is read while deciding whether to stop a machine.
func ProseTreatment(tokens Tokens) TextTreatment {
	return TextTreatment{
		Font:               tokens.Fonts.Reading,
		Style:              tokens.Typography.Body,
		Color:              tokens.Colors.TextSecondary,
		MaxWidthCharacters: 76,
	}
}

// ReadoutTreatment resolves a value the machine measured.
//
// Monospace, so a figure that changes while you watch it does not change its
// own width as it changes, and so a column of them lines up.
func ReadoutTreatment(tokens Tokens) TextTreatment {
	return TextTreatment{
		Font:  tokens.Fonts.Code,
		Style: tokens.Typography.MetricValue,
		Color: tokens.Colors.TextPrimary,
	}
}

// HeadingClass returns the class list for one heading role.
func HeadingClass(tokens Tokens, role HeadingRole) string {
	return treatmentClass(HeadingTreatment(tokens, role))
}

// ProseClass returns the class list for read-and-judge prose.
func ProseClass(tokens Tokens) string { return treatmentClass(ProseTreatment(tokens)) }

// ReadoutClass returns the class list for a measured value.
func ReadoutClass(tokens Tokens) string { return treatmentClass(ReadoutTreatment(tokens)) }

// treatmentClass compiles one resolved treatment.
func treatmentClass(treatment TextTreatment) string {
	rules := []css.Rule{
		css.Margin(css.Zero),
		css.TextColor(css.Hex(string(treatment.Color))),
		css.Font(css.FontStack(treatment.Font)),
		css.FontSize(css.Px(treatment.Style.Size)),
		css.LineHeightLen(css.Px(treatment.Style.LineHeight)),
		weightRule(treatment.Style.Weight),
	}
	if treatment.TrackingEms != 0 {
		rules = append(rules, css.Tracking(css.Ems(treatment.TrackingEms)))
	}
	if treatment.Uppercase {
		rules = append(rules, css.TextTransform.Uppercase)
	}
	if treatment.MaxWidthCharacters > 0 {
		rules = append(rules, css.MaxWidth(css.Ch(treatment.MaxWidthCharacters)))
	}
	return css.New(rules...).String()
}

// weightRule maps a numeric weight onto the css package's named weights.
func weightRule(weight int) css.Rule {
	switch {
	case weight >= 700:
		return css.FontWeight.Bold
	case weight >= 600:
		return css.FontWeight.Semibold
	case weight >= 500:
		return css.FontWeight.Medium
	default:
		return css.FontWeight.Normal
	}
}

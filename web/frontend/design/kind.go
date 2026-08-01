package design

import "fmt"

// Kind identifies the typed content category a piece of timeline or graph
// presentation represents. Kind is independent from Status: a card's Status
// describes where its workflow stands (passed, running, blocked); its Kind
// describes what sort of artifact or step it is (code, a test, a plan). The
// mockup reference set color-codes both conversation cards and graph nodes by
// Kind, using it as the primary visual hierarchy device.
type Kind string

const (
	KindCode       Kind = "code"
	KindTest       Kind = "test"
	KindPlan       Kind = "plan"
	KindEvidence   Kind = "evidence"
	KindMemory     Kind = "memory"
	KindForecast   Kind = "forecast"
	KindExecution  Kind = "execution"
	KindValidation Kind = "validation"
)

// KindPresentation provides redundant text, icon, and color encodings for one
// content category; color is never the only signal.
type KindPresentation struct {
	Kind       Kind
	Label      string
	Icon       string
	Foreground Color
	Background Color
}

// KindPresentationFor returns the accessible presentation for one content
// category. Plan and Evidence intentionally reuse the same semantic colors as
// StatusPlan and StatusEvidence: the mockup reference set uses one hue for
// each of those meanings whether it labels a graph-legend kind or a
// conversation-card kind.
func KindPresentationFor(kind Kind, tokens Tokens) (KindPresentation, error) {
	colors := tokens.Colors
	switch kind {
	case KindCode:
		return kindPresentation(kind, "Code", "</>", colors.OnCode, colors.Code), nil
	case KindTest:
		return kindPresentation(kind, "Test", "✓", colors.OnTest, colors.Test), nil
	case KindPlan:
		return kindPresentation(kind, "Plan", "◇", colors.OnPlan, colors.Plan), nil
	case KindEvidence:
		return kindPresentation(kind, "Evidence", "◆", colors.OnEvidence, colors.Evidence), nil
	case KindMemory:
		return kindPresentation(kind, "Memory", "●", colors.OnMemory, colors.Memory), nil
	case KindForecast:
		return kindPresentation(kind, "Forecast", "↗", colors.OnForecast, colors.Forecast), nil
	case KindExecution:
		return kindPresentation(kind, "Execution", "▶", colors.OnExecution, colors.Execution), nil
	case KindValidation:
		return kindPresentation(kind, "Validation", "⛨", colors.OnValidation, colors.Validation), nil
	default:
		return KindPresentation{}, fmt.Errorf("unsupported frontend kind %q", kind)
	}
}

// KnownKinds returns every typed content category the design system colors.
func KnownKinds() []Kind {
	return []Kind{
		KindCode, KindTest, KindPlan, KindEvidence, KindMemory,
		KindForecast, KindExecution, KindValidation,
	}
}

func kindPresentation(
	kind Kind,
	label string,
	icon string,
	foreground Color,
	background Color,
) KindPresentation {
	return KindPresentation{
		Kind: kind, Label: label, Icon: icon,
		Foreground: foreground, Background: background,
	}
}

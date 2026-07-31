package design

import "fmt"

// Status identifies a semantic state independent from a component's type.
type Status string

const (
	StatusNeutral     Status = "neutral"
	StatusAccent      Status = "accent"
	StatusSuccess     Status = "success"
	StatusWarning     Status = "warning"
	StatusFailure     Status = "failure"
	StatusActive      Status = "active"
	StatusBlocked     Status = "blocked"
	StatusInvalidated Status = "invalidated"
	StatusPending     Status = "pending"
	StatusPlan        Status = "plan"
	StatusEvidence    Status = "evidence"
)

// StatusPresentation provides redundant text, icon, and shape/pattern
// encodings; color is never the only status signal.
type StatusPresentation struct {
	Status     Status
	Label      string
	Icon       string
	Shape      string
	Foreground Color
	Background Color
}

// StatusPresentationFor returns the accessible presentation for one state.
func StatusPresentationFor(
	status Status,
	tokens Tokens,
) (StatusPresentation, error) {
	colors := tokens.Colors
	switch status {
	case StatusNeutral:
		return StatusPresentation{
			Status: status, Label: "Neutral", Icon: "•", Shape: "circle",
			Foreground: colors.TextPrimary, Background: colors.Surface3,
		}, nil
	case StatusAccent:
		return statusPresentation(status, "Action", "→", "arrow", colors.OnAccent, colors.Accent), nil
	case StatusSuccess:
		return statusPresentation(status, "Passed", "✓", "check", colors.OnSuccess, colors.Success), nil
	case StatusWarning:
		return statusPresentation(status, "Warning", "!", "triangle", colors.OnWarning, colors.Warning), nil
	case StatusFailure:
		return statusPresentation(status, "Failed", "×", "cross", colors.OnFailure, colors.Failure), nil
	case StatusActive:
		return statusPresentation(status, "Running", "▶", "ring", colors.OnActive, colors.Active), nil
	case StatusBlocked:
		return statusPresentation(status, "Blocked", "■", "lock", colors.OnBlocked, colors.Blocked), nil
	case StatusInvalidated:
		return statusPresentation(status, "Invalidated", "↯", "broken", colors.OnInvalidated, colors.Invalidated), nil
	case StatusPending:
		return statusPresentation(status, "Pending", "○", "hollow", colors.OnPending, colors.Pending), nil
	case StatusPlan:
		return statusPresentation(status, "Planned", "◇", "diamond", colors.OnPlan, colors.Plan), nil
	case StatusEvidence:
		return statusPresentation(status, "Evidence", "◆", "hexagon", colors.OnEvidence, colors.Evidence), nil
	default:
		return StatusPresentation{}, fmt.Errorf("unsupported frontend status %q", status)
	}
}

func statusPresentation(
	status Status,
	label string,
	icon string,
	shape string,
	foreground Color,
	background Color,
) StatusPresentation {
	return StatusPresentation{
		Status: status, Label: label, Icon: icon, Shape: shape,
		Foreground: foreground, Background: background,
	}
}

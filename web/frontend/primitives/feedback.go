package primitives

import (
	"fmt"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type InlineAlertProps struct {
	Title   string
	Message string
	Tone    design.Status
	Mode    Mode
}

func InlineAlert(props InlineAlertProps) ui.Node {
	if strings.TrimSpace(props.Message) == "" {
		return contractError("inline-alert", "message is required")
	}
	tokens := props.Mode.Tokens()
	presentation, err := design.StatusPresentationFor(props.Tone, tokens)
	if err != nil {
		presentation, _ = design.StatusPresentationFor(design.StatusNeutral, tokens)
	}
	role := "status"
	live := "polite"
	if props.Tone == design.StatusFailure || props.Tone == design.StatusInvalidated {
		role = "alert"
		live = "assertive"
	}
	children := []ui.Node{
		html.Span(html.Props{
			Aria:  map[string]string{"hidden": "true"},
			Class: css.New(css.TextColor(css.Hex(string(presentation.Background))), css.FontWeight.Bold).String(),
		}, html.Text(presentation.Icon)),
	}
	if props.Title != "" {
		children = append(children, html.Strong(html.Props{}, html.Text(props.Title)))
	}
	children = append(children, html.Span(html.Props{}, html.Text(props.Message)))
	return html.Div(
		html.Props{
			Role: role,
			Aria: map[string]string{"live": live},
			Data: map[string]string{
				"component": "inline-alert",
				"tone":      string(props.Tone),
				"shape":     presentation.Shape,
			},
			Class: css.New(
				u.Flex,
				u.ItemsCenter,
				css.Gap(css.Px(tokens.Spacing.SM)),
				css.Padding(css.Px(tokens.Spacing.MD)),
				css.Bg(css.Hex(string(tokens.Colors.Surface2))),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
				css.BorderLeft(css.Px(tokens.Geometry.BorderStrongWidth), css.Hex(string(presentation.Background))),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			).String(),
		},
		children...,
	)
}

type SkeletonProps struct {
	AccessibleLabel string
	Lines           int
	Mode            Mode
}

func Skeleton(props SkeletonProps) ui.Node {
	if strings.TrimSpace(props.AccessibleLabel) == "" {
		return contractError("skeleton", "accessible label is required")
	}
	lines := props.Lines
	if lines < 1 {
		lines = 1
	}
	tokens := props.Mode.Tokens()
	children := make([]ui.Node, 0, lines)
	for index := 0; index < lines; index++ {
		children = append(children, html.Div(html.Props{
			Key:  fmt.Sprintf("line-%d", index),
			Aria: map[string]string{"hidden": "true"},
			Class: css.New(
				css.H(css.Px(tokens.Typography.Body.LineHeight)),
				css.Bg(css.Hex(string(tokens.Colors.Surface3))),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			).String(),
		}))
	}
	return html.Div(
		html.Props{
			Role: "status",
			Aria: map[string]string{
				"label": props.AccessibleLabel,
				"busy":  "true",
				"live":  "polite",
			},
			Data: map[string]string{
				"component":      "skeleton",
				"state":          "loading",
				"reduced-motion": boolARIA(props.Mode.ReducedMotion),
				"animation":      map[bool]string{true: "none", false: "bounded"}[props.Mode.ReducedMotion],
			},
			Class: stackClass(tokens),
		},
		children...,
	)
}

type ProgressIndicatorProps struct {
	Label string
	Value float64
	Max   float64
	Mode  Mode
}

func ProgressIndicator(props ProgressIndicatorProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("progress-indicator", "accessible label is required")
	}
	maximum := props.Max
	if maximum <= 0 {
		maximum = 100
	}
	value := clamp(props.Value, 0, maximum)
	return html.Progress(
		html.Props{
			Value: fmt.Sprintf("%g", value),
			Max:   fmt.Sprintf("%g", maximum),
			Aria:  map[string]string{"label": props.Label},
			Data:  map[string]string{"component": "progress-indicator", "state": "loading"},
		},
		html.Text(fmt.Sprintf("%.0f%%", value/maximum*100)),
	)
}

type BadgeProps struct {
	Label  string
	Status design.Status
	Mode   Mode
}

func Badge(props BadgeProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("badge", "label is required")
	}
	presentation, err := design.StatusPresentationFor(props.Status, props.Mode.Tokens())
	if err != nil {
		presentation, _ = design.StatusPresentationFor(design.StatusNeutral, props.Mode.Tokens())
	}
	return html.Span(
		html.Props{
			Title: props.Label,
			Data: map[string]string{
				"component": "badge",
				"status":    string(props.Status),
				"shape":     presentation.Shape,
			},
			Class: css.New(
				u.InlineFlex,
				u.ItemsCenter,
				css.Gap(css.Px(4)),
				css.PaddingX(css.Px(8)),
				css.Bg(css.Hex(string(props.Mode.Tokens().Colors.Surface3))),
				css.TextColor(css.Hex(string(props.Mode.Tokens().Colors.TextPrimary))),
				css.Border(css.Px(1), css.Hex(string(presentation.Background))),
				css.Rounded(css.Px(999)),
				css.FontSize(css.Px(props.Mode.Tokens().Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(props.Mode.Tokens().Typography.Metadata.LineHeight)),
				css.FontWeight.Medium,
			).String(),
		},
		html.Span(html.Props{
			Aria:  map[string]string{"hidden": "true"},
			Class: css.New(css.TextColor(css.Hex(string(presentation.Background)))).String(),
		}, html.Text(presentation.Icon)),
		html.Text(props.Label),
	)
}

type EmptyStateProps struct {
	Title       string
	Body        string
	ActionLabel string
	Mode        Mode
	OnAction    func()
}

func EmptyState(props EmptyStateProps) ui.Node {
	return statePanel("empty-state", "status", props.Title, props.Body, props.ActionLabel, props.Mode, props.OnAction)
}

type ErrorStateProps struct {
	Title       string
	Body        string
	ActionLabel string
	Mode        Mode
	OnAction    func()
}

func ErrorState(props ErrorStateProps) ui.Node {
	return statePanel("error-state", "alert", props.Title, props.Body, props.ActionLabel, props.Mode, props.OnAction)
}

func statePanel(component, role, title, body, actionLabel string, mode Mode, onAction func()) ui.Node {
	if strings.TrimSpace(title) == "" {
		return contractError(component, "title is required")
	}
	tokens := mode.Tokens()
	children := []ui.Node{html.H2(html.Props{
		Class: css.New(
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Margin(css.Zero),
			css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
			css.FontWeight.Semibold,
		).String(),
	}, html.Text(title))}
	if body != "" {
		children = append(children, html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero), css.MaxWidth(css.Ch(72)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				css.FontSize(css.Px(tokens.Typography.Body.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
			).String(),
		}, html.Text(body)))
	}
	if actionLabel != "" {
		children = append(children, Button(ButtonProps{
			Label:   actionLabel,
			Primary: component == "error-state",
			Mode:    mode,
			OnClick: onAction,
		}))
	}
	return html.Section(
		html.Props{
			Role:  role,
			Data:  map[string]string{"component": component, "state": map[string]string{"empty-state": "empty", "error-state": "error"}[component]},
			Class: surfaceClass(tokens),
		},
		children...,
	)
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

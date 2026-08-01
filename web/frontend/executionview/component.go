package executionview

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// MetricStripProps is the row of figures above a run.
type MetricStripProps struct {
	Measurements []Measurement
	Mode         primitives.Mode
}

// MetricStrip renders the figures that decide whether to intervene.
//
// Every value is set in the monospace face with a quiet sans label above it.
// A figure that changes while you watch it must not change its own width as it
// changes, and a column of them has to line up to be comparable.
func MetricStrip(props MetricStripProps) ui.Node {
	tokens := props.Mode.Tokens()
	if len(props.Measurements) == 0 {
		return html.Span(html.Props{Hidden: true})
	}
	cells := make([]ui.Node, 0, len(props.Measurements))
	for _, measurement := range props.Measurements {
		if err := measurement.Validate(); err != nil {
			// A measurement that cannot be presented honestly is shown as
			// unreadable rather than dropped: a strip that silently loses a
			// figure reads as a run that has fewer things worth watching.
			cells = append(cells, metricCell(tokens, Measurement{
				Label: "unreadable", Unknown: true,
			}))
			continue
		}
		cells = append(cells, metricCell(tokens, measurement))
	}
	return html.Section(html.Props{
		Data: map[string]string{"component": "execution-metric-strip"},
		Aria: map[string]string{"label": "Run measurements"},
		Class: css.New(
			u.Grid,
			css.GridCols(css.Repeat(len(cells), css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))),
			css.Gap(css.Px(tokens.Spacing.LG)),
			css.Padding(css.Px(tokens.Rhythm.PanelInset)),
			css.Rounded(css.Px(tokens.Geometry.DialogRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(
				css.Px(tokens.Geometry.BorderWidth),
				css.Hex(string(tokens.Colors.BorderSubtle)),
			),
		).String(),
	}, cells...)
}

func metricCell(tokens design.Tokens, measurement Measurement) ui.Node {
	value := measurement.Display()
	colour := tokens.Colors.TextPrimary
	switch measurement.Tone {
	case ToneGood:
		colour = tokens.Colors.Success
	case ToneCaution:
		colour = tokens.Colors.Warning
	case ToneBad:
		colour = tokens.Colors.Failure
	}
	if measurement.Unknown {
		// An unmeasured figure is muted rather than toned. Colouring a value
		// nobody has measured would make an absence look like a finding.
		colour = tokens.Colors.TextMuted
	}
	children := []ui.Node{
		html.Span(html.Props{
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.Tracking(css.Ems(0.06)),
			).String(),
			Text: measurement.Label,
		}),
		html.Span(html.Props{
			Data: map[string]string{
				"field":   "value",
				"unknown": strconv.FormatBool(measurement.Unknown),
			},
			Class: css.New(
				css.TextColor(css.Hex(string(colour))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.MetricValue.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.MetricValue.LineHeight)),
				css.FontWeight.Medium,
			).String(),
			Text: value,
		}),
	}
	if len(measurement.Trend) > 1 {
		children = append(children, sparkline(tokens, measurement.Trend, colour))
	}
	return html.Div(html.Props{
		Data: map[string]string{"component": "execution-metric", "label": measurement.Label},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero),
		).String(),
	}, children...)
}

// sparkline draws a short series beneath a figure.
//
// It carries no axis and no labels on purpose: it answers "is this going up or
// down" and nothing else, and the exact value is already stated above it.
func sparkline(tokens design.Tokens, series []float64, colour design.Color) ui.Node {
	const width, height = 96.0, 22.0
	lowest, highest := series[0], series[0]
	for _, value := range series {
		if value < lowest {
			lowest = value
		}
		if value > highest {
			highest = value
		}
	}
	span := highest - lowest
	if span == 0 {
		// A flat series draws through the middle rather than at an edge,
		// where it would read as a floor or a ceiling it never touched.
		span = 1
		lowest -= 0.5
	}
	points := make([]string, 0, len(series))
	for index, value := range series {
		x := width * float64(index) / float64(len(series)-1)
		y := height - (height * (value - lowest) / span)
		points = append(points, formatNumber(x)+","+formatNumber(y))
	}
	return html.Svg(html.Props{
		Aria: map[string]string{"hidden": "true"},
		Raw: map[string]any{
			"viewBox": "0 0 " + formatNumber(width) + " " + formatNumber(height),
			"width":   int(width), "height": int(height),
			"fill": "none", "preserveAspectRatio": "none",
		},
	}, html.Polyline(html.Props{
		Raw: map[string]any{
			"points": strings.Join(points, " "),
			"stroke": string(colour), "stroke-width": 1.5,
			"stroke-linecap": "round", "stroke-linejoin": "round",
			"vector-effect": "non-scaling-stroke",
		},
	}))
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

// TimelineProps is the ordered list of steps a run has taken.
type TimelineProps struct {
	Steps []Step
	// Now is supplied rather than read, so this component stays pure and a
	// test can place a run at an exact moment.
	Mode primitives.Mode
}

// ExecutionTimeline renders the ordered steps.
func ExecutionTimeline(props TimelineProps) ui.Node {
	tokens := props.Mode.Tokens()
	progress := ProgressOf(props.Steps)
	rows := make([]ui.Node, 0, len(props.Steps))
	for _, step := range props.Steps {
		if err := step.Validate(); err != nil {
			rows = append(rows, html.Li(html.Props{
				Role: "alert",
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.Failure))),
					css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
				).String(),
				Text: "This step cannot be shown: " + err.Error(),
			}))
			continue
		}
		rows = append(rows, timelineRow(tokens, step))
	}
	return html.Section(html.Props{
		Data: map[string]string{
			"component": "execution-timeline",
			"completed": strconv.Itoa(progress.Completed),
			"total":     strconv.Itoa(progress.Total),
		},
		Aria:  map[string]string{"label": "Execution timeline"},
		Class: executionPanelClass(tokens),
	},
		executionPanelHeader(tokens, "Execution timeline", progress.Label()),
		progressBar(tokens, progress.Fraction(), progress.Label()),
		html.Ol(html.Props{
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
				css.Margin(css.Zero), css.Padding(css.Zero),
			).String(),
		}, rows...),
	)
}

func timelineRow(tokens design.Tokens, step Step) ui.Node {
	tone, icon := stepPresentation(step.State)
	colour := stepColour(tokens, step.State)
	children := []ui.Node{
		html.Span(html.Props{
			Aria: map[string]string{"hidden": "true"},
			Class: css.New(
				u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
				css.W(css.Px(18)), css.H(css.Px(18)), css.FlexShrink(css.Num(0)),
				css.TextColor(css.Hex(string(colour))),
			).String(),
		}, primitives.Icon(primitives.IconProps{Name: icon, Size: 15})),
		html.Span(html.Props{
			Class: css.New(
				css.FlexGrow(css.Num(1)), css.MinWidth(css.Zero),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
			).String(),
			Text: step.Label,
		}),
		// The state is named as well as coloured. A reader who cannot separate
		// the colours must still be able to tell a finished step from a failed
		// one.
		html.Span(html.Props{
			Data: map[string]string{"field": "state"},
			Class: css.New(
				css.TextColor(css.Hex(string(colour))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.WhiteSpace.NoWrap,
			).String(),
			Text: tone,
		}),
	}
	if !step.At.IsZero() {
		children = append(children, html.Span(html.Props{
			Data: map[string]string{"field": "at"},
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.WhiteSpace.NoWrap,
			).String(),
			Text: step.At.UTC().Format("15:04:05"),
		}))
	}
	return html.Li(html.Props{
		Data: map[string]string{
			"component": "execution-step", "step": step.ID, "state": string(step.State),
		},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinHeight(css.Px(26)),
		).String(),
	}, children...)
}

func stepPresentation(state StepState) (string, primitives.IconName) {
	switch state {
	case StepRunning:
		return "running", primitives.IconTheme
	case StepDone:
		return "done", primitives.IconCheck
	case StepFailed:
		return "failed", primitives.IconClose
	case StepSkipped:
		return "skipped", primitives.IconChevronRight
	default:
		return "pending", primitives.IconChevronDown
	}
}

func stepColour(tokens design.Tokens, state StepState) design.Color {
	switch state {
	case StepRunning:
		return tokens.Colors.Active
	case StepDone:
		return tokens.Colors.Success
	case StepFailed:
		return tokens.Colors.Failure
	case StepSkipped:
		return tokens.Colors.TextMuted
	default:
		return tokens.Colors.TextMuted
	}
}

// LogProps is the streaming log and its filter.
type LogProps struct {
	Lines  []LogLine
	Filter Filter
	// Streaming marks a log that is still receiving lines, so the panel can
	// say so rather than looking like a finished log that stopped early.
	Streaming  bool
	Mode       primitives.Mode
	OnToggle   func(Severity)
	OnClearAll func()
}

// StreamingLog renders emitted lines with severity filters.
func StreamingLog(props LogProps) ui.Node {
	tokens := props.Mode.Tokens()
	admitted, hidden := props.Filter.Apply(props.Lines)
	rows := make([]ui.Node, 0, len(admitted))
	for _, line := range admitted {
		if err := line.Validate(); err != nil {
			continue
		}
		rows = append(rows, logRow(tokens, line))
	}

	children := []ui.Node{
		executionPanelHeader(tokens, "Execution log", logStatus(props.Streaming, len(admitted))),
		severityFilters(props),
	}
	if len(rows) == 0 {
		children = append(children, html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
			).String(),
			Text: emptyLogMessage(len(props.Lines), hidden, props.Streaming),
		}))
	} else {
		children = append(children, html.Ol(html.Props{
			Data: map[string]string{"component": "execution-log-lines"},
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
				css.Margin(css.Zero), css.Padding(css.Zero),
				css.MaxHeight(css.Px(280)), css.OverflowY.Auto,
			).String(),
		}, rows...))
	}
	if hidden > 0 && len(rows) > 0 {
		// A filter that hides lines says so. A shorter log with no explanation
		// reads as a quieter run.
		children = append(children, html.P(html.Props{
			Role: "status",
			Class: css.New(
				css.Margin(css.Zero),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			).String(),
			Text: strconv.Itoa(hidden) + " line(s) hidden by the current filter",
		}))
	}
	return html.Section(html.Props{
		Data: map[string]string{
			"component": "execution-log",
			"streaming": strconv.FormatBool(props.Streaming),
			"hidden":    strconv.Itoa(hidden),
		},
		Aria:  map[string]string{"label": "Execution log"},
		Class: executionPanelClass(tokens),
	}, children...)
}

func logStatus(streaming bool, shown int) string {
	if streaming {
		return "streaming · " + strconv.Itoa(shown) + " lines"
	}
	return strconv.Itoa(shown) + " lines"
}

func emptyLogMessage(total, hidden int, streaming bool) string {
	switch {
	case total == 0 && streaming:
		return "Waiting for the first line."
	case total == 0:
		return "This run emitted no log lines."
	case hidden == total:
		return "Every line is hidden by the current filter."
	default:
		return "No lines match the current filter."
	}
}

func severityFilters(props LogProps) ui.Node {
	tokens := props.Mode.Tokens()
	chips := make([]ui.Node, 0, len(AllSeverities())+1)
	chips = append(chips, filterChip(
		tokens, "All", !props.Filter.Active(), props.OnClearAll,
	))
	for _, severity := range AllSeverities() {
		selected := props.Filter.Selected[severity]
		toggle := props.OnToggle
		current := severity
		var handler func()
		if toggle != nil {
			handler = func() { toggle(current) }
		}
		chips = append(chips, filterChip(tokens, severity.Tag(), selected, handler))
	}
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Filter log by severity"},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
		).String(),
	}, chips...)
}

func filterChip(
	tokens design.Tokens,
	label string,
	selected bool,
	handler func(),
) ui.Node {
	background := tokens.Colors.Surface2
	foreground := tokens.Colors.TextSecondary
	if selected {
		background = tokens.Colors.Selection
		foreground = tokens.Colors.OnSelection
	}
	buttonProps := html.PropsOf(html.OnClick(handler))
	buttonProps.Type = "button"
	buttonProps.Disabled = handler == nil
	buttonProps.Aria = map[string]string{
		"label": "Show " + label, "pressed": strconv.FormatBool(selected),
	}
	buttonProps.Data = map[string]string{
		"component": "log-filter", "severity": strings.ToLower(label),
		"selected": strconv.FormatBool(selected),
	}
	buttonProps.Text = label
	buttonProps.Class = css.New(
		u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
		css.H(css.Px(26)), css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Rounded(css.Px(tokens.Geometry.PillRadius)),
		css.Bg(css.Hex(string(background))),
		css.TextColor(css.Hex(string(foreground))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(11)),
		css.Tracking(css.Ems(0.06)),
		css.Cursor.Pointer,
	).String()
	return html.Button(buttonProps)
}

func logRow(tokens design.Tokens, line LogLine) ui.Node {
	children := []ui.Node{
		html.Span(html.Props{
			Data: map[string]string{"field": "at"},
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FlexShrink(css.Num(0)),
			).String(),
			Text: line.At.UTC().Format("15:04:05"),
		}),
		html.Span(html.Props{
			Data: map[string]string{"field": "severity"},
			Class: css.New(
				u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
				css.W(css.Px(40)), css.FlexShrink(css.Num(0)),
				css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
				css.Bg(css.Hex(string(severityBackground(tokens, line.Severity)))),
				css.TextColor(css.Hex(string(severityForeground(tokens, line.Severity)))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(10)),
				css.FontWeight.Semibold,
			).String(),
			Text: line.Severity.Tag(),
		}),
		html.Span(html.Props{
			Class: css.New(
				css.FlexGrow(css.Num(1)), css.MinWidth(css.Zero),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Code.Size)),
				css.OverflowWrap.Anywhere,
			).String(),
			Text: line.Text,
		}),
	}
	if line.Duration > 0 {
		children = append(children, html.Span(html.Props{
			Data: map[string]string{"field": "duration"},
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FlexShrink(css.Num(0)),
			).String(),
			Text: FormatDuration(line.Duration),
		}))
	}
	return html.Li(html.Props{
		Data: map[string]string{
			"component": "execution-log-line", "line": line.ID,
			"severity": string(line.Severity),
		},
		Class: css.New(
			u.Flex, u.ItemsStart, css.Gap(css.Px(tokens.Spacing.SM)),
		).String(),
	}, children...)
}

func severityBackground(tokens design.Tokens, severity Severity) design.Color {
	switch severity {
	case SeverityPass:
		return tokens.Colors.Success
	case SeverityWarn:
		return tokens.Colors.Warning
	case SeverityFailure:
		return tokens.Colors.Failure
	case SeverityRun:
		return tokens.Colors.Active
	default:
		return tokens.Colors.Surface3
	}
}

func severityForeground(tokens design.Tokens, severity Severity) design.Color {
	switch severity {
	case SeverityPass:
		return tokens.Colors.OnSuccess
	case SeverityWarn:
		return tokens.Colors.OnWarning
	case SeverityFailure:
		return tokens.Colors.OnFailure
	case SeverityRun:
		return tokens.Colors.OnActive
	default:
		return tokens.Colors.TextSecondary
	}
}

// CurrentWorkProps is what the run is executing right now.
type CurrentWorkProps struct {
	Work Work
	Mode primitives.Mode
}

// Work aliases CurrentWork so the props read naturally at the call site.
type Work = CurrentWork

// CurrentlyExecuting renders the unit of work in flight.
func CurrentlyExecuting(props CurrentWorkProps) ui.Node {
	tokens := props.Mode.Tokens()
	if !props.Work.Present {
		return html.Section(html.Props{
			Data:  map[string]string{"component": "currently-executing", "present": "false"},
			Aria:  map[string]string{"label": "Currently executing"},
			Class: executionPanelClass(tokens),
		},
			executionPanelHeader(tokens, "Currently executing", "idle"),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
				).String(),
				Text: "Nothing is executing.",
			}),
		)
	}
	remaining := "unknown"
	if props.Work.RemainingKnown {
		remaining = FormatDuration(props.Work.Remaining)
	}
	return html.Section(html.Props{
		Data:  map[string]string{"component": "currently-executing", "present": "true"},
		Aria:  map[string]string{"label": "Currently executing"},
		Class: executionPanelClass(tokens),
	},
		executionPanelHeader(tokens, "Currently executing", ""),
		html.P(html.Props{
			Class: design.HeadingClass(tokens, design.HeadingPanel),
			Text:  props.Work.Title,
		}),
		html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.OverflowWrap.Anywhere,
			).String(),
			Text: props.Work.Detail,
		}),
		progressBar(tokens, props.Work.Fraction, "Current work"),
		html.Div(html.Props{
			Class: css.New(
				u.Flex, css.Gap(css.Px(tokens.Spacing.LG)),
			).String(),
		},
			elapsedPair(tokens, "elapsed", FormatDuration(props.Work.Elapsed)),
			elapsedPair(tokens, "remaining", remaining),
		),
	)
}

func elapsedPair(tokens design.Tokens, label, value string) ui.Node {
	return html.Div(html.Props{
		Data:  map[string]string{"field": label},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String(),
	},
		html.Span(html.Props{
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			).String(),
			Text: label,
		}),
		html.Span(html.Props{
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
			).String(),
			Text: value,
		}),
	)
}

// progressBar draws completion, and names it for anyone who cannot see it.
func progressBar(tokens design.Tokens, fraction float64, label string) ui.Node {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	percent := int(fraction*100 + 0.5)
	return html.Div(html.Props{
		Role: "progressbar",
		Aria: map[string]string{
			"label": label, "valuemin": "0", "valuemax": "100",
			"valuenow": strconv.Itoa(percent),
		},
		Data: map[string]string{"component": "execution-progress"},
		Class: css.New(
			css.W(css.Full), css.H(css.Px(4)),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface3))),
			css.Overflow.Hidden,
		).String(),
	}, html.Div(html.Props{
		Class: css.New(
			css.H(css.Full), css.W(css.Percent(float64(percent))),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Accent))),
		).String(),
	}))
}

func executionPanelClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
		css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.MinWidth(css.Zero),
		css.Rounded(css.Px(tokens.Geometry.DialogRadius)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.Border(
			css.Px(tokens.Geometry.BorderWidth),
			css.Hex(string(tokens.Colors.BorderSubtle)),
		),
	).String()
}

func executionPanelHeader(tokens design.Tokens, title, status string) ui.Node {
	children := []ui.Node{
		html.H2(html.Props{
			Class: design.HeadingClass(tokens, design.HeadingPanel),
			Text:  title,
		}),
	}
	if status != "" {
		children = append(children, html.Span(html.Props{
			Data: map[string]string{"field": "panel-status"},
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			).String(),
			Text: status,
		}))
	}
	return html.Div(html.Props{
		Class: css.New(
			u.Flex, u.ItemsCenter, u.JustifyBetween,
			css.Gap(css.Px(tokens.Spacing.SM)),
		).String(),
	}, children...)
}

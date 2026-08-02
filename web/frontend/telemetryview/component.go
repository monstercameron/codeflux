// Package telemetryview renders the content-free local telemetry controls in
// Settings. It receives already-redacted closed-schema rows only.
package telemetryview

import (
	"fmt"
	"time"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type Row struct {
	LocalID   uint64
	Kind      string
	Outcome   string
	Component string
	Occurred  time.Time
	Duration  time.Duration
}

type Props struct {
	Mode               primitives.Mode
	Loading            bool
	Busy               bool
	Error              bool
	Rows               []Row
	HasMore            bool
	OnReload           func()
	OnLoadMore         func()
	DeleteConfirmation bool
	OnDeleteRequest    func()
	OnDeleteConfirm    func()
	OnDeleteCancel     func()
}

func Component(props Props) ui.Node {
	children := []ui.Node{
		html.P(html.Props{Text: "Local product-use measurements contain event categories, timing, and status only. They never include keystrokes, prompts, source, tool output, or hidden reasoning."}),
	}
	switch {
	case props.Loading:
		children = append(children, html.P(html.Props{Text: "Loading local telemetry…", Aria: map[string]string{"live": "polite"}}))
	case props.Error:
		children = append(children,
			html.P(html.Props{Text: "Local telemetry could not be loaded. Existing data was not changed.", Aria: map[string]string{"live": "polite"}}),
			primitives.Button(primitives.ButtonProps{Label: "Retry telemetry", Mode: props.Mode, Disabled: props.OnReload == nil, OnClick: props.OnReload}),
		)
	case len(props.Rows) == 0:
		children = append(children, html.P(html.Props{Text: "No local telemetry has been recorded."}))
	default:
		children = append(children, eventLog(props))
		if props.HasMore {
			children = append(children, primitives.Button(primitives.ButtonProps{
				Label: "Load older telemetry", Mode: props.Mode, Busy: props.Busy,
				Disabled: props.OnLoadMore == nil, OnClick: props.OnLoadMore,
			}))
		}
	}
	if props.DeleteConfirmation {
		children = append(children,
			html.P(html.Props{Text: "Delete every local telemetry event? This cannot be undone.", Aria: map[string]string{"live": "polite"}}),
			actionRow(props,
				primitives.Button(primitives.ButtonProps{
					Label: "Confirm telemetry deletion", Mode: props.Mode, Busy: props.Busy,
					Disabled: props.OnDeleteConfirm == nil, OnClick: props.OnDeleteConfirm,
				}),
				primitives.Button(primitives.ButtonProps{
					Label: "Cancel deletion", Mode: props.Mode, Disabled: props.Busy || props.OnDeleteCancel == nil,
					OnClick: props.OnDeleteCancel,
				}),
			),
		)
	} else {
		// The two ways this control can be unusable are different, and a
		// reader deciding what to do next needs to know which one applies.
		reason := ""
		switch {
		case len(props.Rows) == 0:
			reason = "There is no local telemetry to delete."
		case props.OnDeleteRequest == nil:
			reason = "Telemetry deletion is unavailable while the session is disconnected."
		}
		children = append(children, actionRow(props, primitives.Button(primitives.ButtonProps{
			Label: "Delete all local telemetry", AccessibleLabel: "Delete all local product telemetry",
			Mode: props.Mode, Busy: props.Busy,
			Disabled:       props.OnDeleteRequest == nil || len(props.Rows) == 0,
			DisabledReason: reason,
			OnClick:        props.OnDeleteRequest,
		})))
	}
	return html.Div(html.Props{
		Data:  map[string]string{"component": "telemetry-settings"},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(props.Mode.Tokens().Spacing.SM))).String(),
	}, children...)
}

// actionRow sets the controls at the width of their own labels, at the edge the
// page puts its values on.
//
// In a stacked column a button takes the full width by default, which made the
// one destructive control on the settings page its largest target and the
// widest element in its section.
func actionRow(props Props, controls ...ui.Node) ui.Node {
	return html.Div(html.Props{
		Class: css.New(
			u.Flex, u.JustifyEnd, u.ItemsCenter, css.FlexWrap.Wrap,
			css.Gap(css.Px(props.Mode.Tokens().Spacing.XS)),
		).String(),
	}, controls...)
}

// eventLog draws the recorded events as a bounded readout rather than a
// bullet list.
//
// Fifty events rendered as list items ran the settings page to four thousand
// pixels and buried every other section under a wall of timestamps. This is a
// log: it is set in the code face, it counts itself, and it scrolls inside a
// fixed height so the page around it stays readable.
func eventLog(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	rows := make([]ui.Node, 0, len(props.Rows))
	for _, row := range props.Rows {
		rows = append(rows, html.Div(html.Props{
			Data: map[string]string{"telemetry-id": fmt.Sprintf("%d", row.LocalID)},
			Class: css.New(
				u.Flex, css.Gap(css.Px(tokens.Spacing.SM)),
				css.PaddingY(css.Px(3)),
				css.WhiteSpace.NoWrap,
			).String(),
		},
			html.Span(html.Props{
				Class: css.New(
					css.FlexShrink(css.Num(0)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: row.Occurred.UTC().Format("01-02 15:04:05"),
			}),
			html.Span(html.Props{
				Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.TextPrimary)))).String(),
				Text:  row.Kind,
			}),
			html.Span(html.Props{
				Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.TextSecondary)))).String(),
				Text:  row.Component,
			}),
			html.Span(html.Props{
				Class: css.New(
					css.Margin(css.RawLength("0 0 0 auto")), css.FlexShrink(css.Num(0)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: outcomeAndDuration(row),
			}),
		))
	}
	return html.Div(html.Props{Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String()},
		html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: recordedCountLabel(len(props.Rows), props.HasMore),
		}),
		html.Div(html.Props{
			Role: "log",
			Aria: map[string]string{"label": "Local telemetry events"},
			Class: css.New(
				css.MaxHeight(css.Px(260)), css.OverflowY.Auto, css.OverflowX.Auto,
				css.Padding(css.RawLength("6px 8px")),
				css.Bg(css.Hex(string(tokens.Colors.Surface1))),
				css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
			).String(),
		}, rows...),
	)
}

// outcomeAndDuration keeps the two facts that decide whether an event matters.
func outcomeAndDuration(row Row) string {
	if row.Duration > 0 {
		return row.Outcome + " · " + row.Duration.String()
	}
	return row.Outcome
}

// recordedCountLabel says how much is shown and whether it is everything, so
// the reader is not left to guess whether the log was truncated.
func recordedCountLabel(count int, hasMore bool) string {
	label := fmt.Sprintf("%d events", count)
	if count == 1 {
		label = "1 event"
	}
	if hasMore {
		return label + " · older events not loaded"
	}
	return label + " · everything recorded"
}

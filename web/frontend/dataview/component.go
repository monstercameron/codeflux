// Package dataview renders the local-data controls in Settings: what this
// browser has stored about the interface, and how to forget it. It owns no
// state and reaches no storage itself.
package dataview

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Props configures the local-data controls.
//
// Stored reports whether this browser is actually holding anything, so the
// section can say "nothing is stored" rather than offering to forget something
// that does not exist.
type Props struct {
	Mode         primitives.Mode
	Stored       bool
	Unavailable  bool
	Busy         bool
	Confirming   bool
	Notice       string
	NoticeTone   design.Status
	OnForget     func()
	OnConfirm    func()
	OnCancel     func()
	DatabasePath string
}

// Component renders what this browser stores and what it does not.
//
// The section used to read "Backup, retention, and local data controls." and
// offer none of the three. What the console can honestly speak for is the
// interface state this browser holds; the repository, the memory, the
// evidence, and the backups live in the coordinator's database, which no
// browser control reaches.
func Component(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	children := []ui.Node{
		html.P(html.Props{
			Class: proseClass(tokens),
			Text: "This browser stores the last route you were on, the rail and " +
				"split layout, and the theme. Nothing else about your work is kept " +
				"here: threads, tasks, evidence, and project memory live in the " +
				"coordinator's local database.",
		}),
	}
	if strings.TrimSpace(props.DatabasePath) != "" {
		children = append(children, definition(tokens, "Coordinator database", props.DatabasePath))
	}
	switch {
	case props.Unavailable:
		children = append(children, html.P(html.Props{
			Class: proseClass(tokens),
			Text:  "Browser storage is unavailable, so nothing about the interface is being kept.",
		}))
	case !props.Stored:
		children = append(children, html.P(html.Props{
			Class: proseClass(tokens),
			Text:  "No interface state is stored in this browser yet.",
		}))
	case props.Confirming:
		children = append(children,
			html.P(html.Props{
				Class: proseClass(tokens),
				Aria:  map[string]string{"live": "polite"},
				Text: "Forget the stored route, layout, and theme? The console will " +
					"open with its defaults next time. Nothing in the coordinator changes.",
			}),
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS))).String(),
			},
				primitives.Button(primitives.ButtonProps{
					Label: "Forget interface state", Mode: props.Mode, Busy: props.Busy,
					Disabled: props.OnConfirm == nil, OnClick: props.OnConfirm,
				}),
				primitives.Button(primitives.ButtonProps{
					Label: "Keep it", Quiet: true, Mode: props.Mode,
					Disabled: props.Busy || props.OnCancel == nil, OnClick: props.OnCancel,
				}),
			),
		)
	default:
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Forget interface state", Mode: props.Mode, Busy: props.Busy,
			Disabled: props.OnForget == nil, OnClick: props.OnForget,
		}))
	}
	if notice := strings.TrimSpace(props.Notice); notice != "" {
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Local data", Message: notice, Tone: props.NoticeTone, Mode: props.Mode,
		}))
	}
	return html.Div(html.Props{
		Data:  map[string]string{"component": "data-settings"},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM))).String(),
	}, children...)
}

// definition names one fact about where data lives, in the code face, because
// a path is read character by character rather than as a sentence.
func definition(tokens design.Tokens, label, value string) ui.Node {
	return html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String(),
	},
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: label,
		}),
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				css.OverflowWrap.Anywhere,
			).String(),
			Text: value,
		}),
	)
}

func proseClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	).String()
}

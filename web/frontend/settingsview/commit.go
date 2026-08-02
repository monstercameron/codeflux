package settingsview

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CommitBar is what an unsaved configuration looks like.
//
// It exists only while something is waiting to be saved. The old page put the
// save control at the bottom of a five-thousand-pixel column, so the control
// and the setting it applied to could not be on screen together; this stays
// with the reader and names every change by setting, from and to, so what is
// about to be committed is legible without scrolling back for it.
func CommitBar(props Props) ui.Node {
	if len(props.FlowPending) == 0 {
		return html.Fragment()
	}
	tokens := props.Mode.Tokens()
	return html.Div(
		html.Props{
			Role: "region",
			Aria: map[string]string{"label": "Unsaved run settings"},
			Data: map[string]string{
				"component": "settings-commit-bar",
				"pending":   countOf(len(props.FlowPending), "change", "changes"),
			},
			Class: css.New(
				css.Position.Sticky, css.Bottom(css.Zero),
				u.Flex, css.FlexWrap.Wrap, u.ItemsCenter,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.PaddingX(css.Px(tokens.Spacing.LG)),
				css.PaddingY(css.Px(tokens.Spacing.MD)),
				css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
				css.BorderTop(
					css.Px(tokens.Geometry.BorderStrongWidth),
					css.Hex(string(tokens.Colors.Warning)),
				),
				css.MinWidth(css.Zero),
			).String(),
		},
		html.Span(html.Props{
			Text: countOf(len(props.FlowPending), "change", "changes") + " not saved",
			Aria: map[string]string{"live": "polite"},
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Body.Size)),
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
		}),
		html.Span(html.Props{
			Text: pendingSummary(props),
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				css.MinWidth(css.Zero), css.OverflowWrap.Anywhere,
			).String(),
		}),
		html.Div(
			html.Props{
				Class: css.New(
					u.Flex, css.Gap(css.Px(tokens.Spacing.XS)),
					css.Custom("margin-left", "auto"),
				).String(),
			},
			primitives.Button(primitives.ButtonProps{
				Label: "Discard", Mode: props.Mode,
				AccessibleLabel: "Discard the unsaved run settings",
				Disabled:        props.OnFlowDiscard == nil || props.FlowBusy,
				OnClick: func() {
					if props.OnFlowDiscard != nil {
						props.OnFlowDiscard()
					}
				},
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Save changes", Mode: props.Mode, Primary: true,
				Busy:     props.FlowBusy,
				Disabled: props.OnFlowSave == nil || props.FlowBusy,
				DisabledReason: disabledReason(
					props.OnFlowSave == nil && !props.FlowBusy,
					"A value outside its bound cannot be saved.",
				),
				OnClick: func() {
					if props.OnFlowSave != nil {
						props.OnFlowSave()
					}
				},
			}),
		),
	)
}

// pendingSummary names every change as from and to, in the sheet's order.
//
// A count alone says how much is unsaved; this says what, which is what
// somebody about to commit a configuration needs to read.
func pendingSummary(props Props) string {
	parts := make([]string, 0, len(props.FlowPending))
	for _, setting := range props.Flow {
		pending, changed := props.FlowPending[setting.Key]
		if !changed {
			continue
		}
		parts = append(parts, setting.Label+" "+flowValueText(setting)+"→"+
			flowValueText(mergedPending(setting, pending)))
	}
	return strings.Join(parts, "   ")
}

// mergedPending is the stored setting carrying the pending value.
func mergedPending(setting, pending FlowSetting) FlowSetting {
	setting.Text, setting.Number, setting.Enabled = pending.Text, pending.Number, pending.Enabled
	return setting
}

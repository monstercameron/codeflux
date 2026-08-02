package settingsview

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

// settingRow lays one run setting on the sheet.
//
// The value sits on the shared axis whatever kind the setting is, so a number,
// a posture, and a switch all read down one column. Below it sits the one
// thing worth knowing about that value: the bound the engine enforces, whether
// it is still the shipped default, and — when it has been changed and not yet
// saved — what it was.
func settingRow(props Props, setting FlowSetting) ui.Node {
	stored, _ := flowStored(props, setting.Key)
	_, pending := props.FlowPending[setting.Key]
	value, trailing := settingCells(props, setting, stored, pending)
	return row(props, rowProps{
		Name:      setting.Label,
		Rationale: setting.Help,
		Value:     value,
		Detail:    settingDetail(props, setting, pending),
		Trailing:  trailing,
		Marked:    props.Jumped == setting.Key,
		Data: map[string]string{
			"component": "settings-flow-setting",
			"setting":   setting.Key,
			"kind":      setting.Kind,
			"changed":   boolLabel(pending),
			"default":   boolLabel(setting.AtDefault && !pending),
			"jumped":    boolLabel(props.Jumped == setting.Key),
		},
	})
}

// settingValue renders the value on the axis, and what it was when it has been
// changed and not yet saved.
//
// A diff shown before saving is the difference between changing a setting and
// hoping you changed the one you meant.
func settingValue(props Props, setting FlowSetting, stored FlowSetting, pending bool) ui.Node {
	tokens := props.Mode.Tokens()
	current := flowValueText(setting)
	if !pending {
		return html.Span(html.Props{
			Text: current, Class: valueTextClass(props, true),
		})
	}
	return html.Span(
		html.Props{
			Class: css.New(u.InlineFlex, css.Items.Baseline, css.Gap(css.Px(tokens.Spacing.XS))).String(),
		},
		html.Span(html.Props{
			Text: flowValueText(stored),
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextDisabled))),
				css.TextDecoration.LineThrough,
			).String(),
		}),
		html.Span(html.Props{
			Text: current,
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Body.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.Warning))),
			).String(),
		}),
	)
}

// settingDetail is the small line under a value: its bound, and whether it is
// still what the engine ships with.
func settingDetail(props Props, setting FlowSetting, pending bool) ui.Node {
	tokens := props.Mode.Tokens()
	switch {
	case pending:
		return html.Span(html.Props{
			Text: "unsaved", Class: detailClass(props, tokens.Colors.Warning),
		})
	case setting.Kind == FlowNumber:
		bound := strconv.FormatInt(int64(setting.Minimum), 10) + "–" +
			strconv.FormatInt(int64(setting.Maximum), 10)
		if !setting.AtDefault {
			return html.Span(html.Props{
				Text: bound + " · changed", Class: detailClass(props, tokens.Colors.Accent),
			})
		}
		return html.Span(html.Props{
			Text: bound, Class: detailClass(props, tokens.Colors.TextDisabled),
		})
	case setting.Kind == FlowSet || setting.Kind == FlowSequence ||
		setting.Kind == FlowMapping:
		word := "default"
		if !setting.AtDefault {
			word = "changed"
		}
		// Said plainly: this surface reports the value and cannot change it.
		return html.Span(html.Props{
			Text: word + " · read only here", Class: detailClass(props, tokens.Colors.TextDisabled),
		})
	case !setting.AtDefault:
		return html.Span(html.Props{
			Text: "changed", Class: detailClass(props, tokens.Colors.Accent),
		})
	default:
		return html.Span(html.Props{
			Text: "default", Class: detailClass(props, tokens.Colors.TextDisabled),
		})
	}
}

// settingCells decides what stands on the axis and what stands beside it.
//
// A control that fits the axis takes the value's place, so the value is never
// printed twice. One that does not fit leaves the value on the axis and stands
// to its right, where it can be as wide as it needs to be. A kind this sheet
// has no control for keeps its value and offers nothing: an inert control
// would invite somebody to set something that would not be sent.
func settingCells(
	props Props,
	setting FlowSetting,
	stored FlowSetting,
	pending bool,
) (ui.Node, ui.Node) {
	value := settingValue(props, setting, stored, pending)
	switch setting.Kind {
	case FlowNumber:
		if pending {
			// While a change is unsaved the axis shows what it replaces, so the
			// stepper moves beside it rather than over it.
			return value, stepperControl(props, setting)
		}
		return stepperControl(props, setting), nil
	case FlowChoice, FlowSwitch:
		options := discreteOptions(setting)
		if !segmentedFitsTheAxis(options) {
			// Too many options, or options too long, to sit side by side. The
			// list takes the axis and the value it shows is the value.
			if pending {
				return value, listControl(props, setting)
			}
			return listControl(props, setting), nil
		}
		control := discreteControl(props, setting, options)
		if pending {
			return value, control
		}
		return control, nil
	}
	return value, nil
}

// numberOutOfBound reports a value outside what the engine declared.
func numberOutOfBound(setting FlowSetting) bool {
	return setting.Number < setting.Minimum || setting.Number > setting.Maximum
}

// providerRow lays one provider and the credential it needs.
func providerRow(props Props, provider ProviderGroup) ui.Node {
	tokens := props.Mode.Tokens()
	check, checked := props.Checks[provider.ID]
	detail := ui.Node(html.Span(html.Props{
		Text:  countOf(len(provider.Models), "model", "models"),
		Class: detailClass(props, tokens.Colors.TextDisabled),
	}))
	below := []ui.Node{}
	reference := props.Reference[provider.ID]
	if len(provider.Models) == 0 {
		below = append(below, noteLine(props,
			"No model is catalogued for this provider, so a credential cannot be "+
				"configured against one yet."))
	} else {
		below = append(below, credentialField(props, provider, reference))
	}
	if checked {
		switch {
		case check.Running:
			below = append(below, noteLine(props, "Checking the credential…"))
		case check.Summary != "":
			tone := design.StatusWarning
			if check.Resolved {
				tone = design.StatusSuccess
			}
			below = append(below, primitives.InlineAlert(primitives.InlineAlertProps{
				Title: "Credential check", Message: check.Summary,
				Tone: tone, Mode: props.Mode,
			}))
		}
	}
	return row(props, rowProps{
		Name:   provider.Name,
		Value:  html.Span(html.Props{Text: availabilityWord(provider.Available), Class: valueTextClass(props, provider.Available)}),
		Detail: detail,
		Trailing: primitives.Button(primitives.ButtonProps{
			// In a row of values the action is sized to the row.
			Compact: true,
			Label:   "Check", Mode: props.Mode,
			AccessibleLabel: "Check the stored credential for " + provider.Name,
			Busy:            check.Running,
			Disabled:        props.OnCheckCredential == nil,
			OnClick: func() {
				if props.OnCheckCredential != nil {
					props.OnCheckCredential(provider.ID)
				}
			},
		}),
		Below: html.Div(
			html.Props{
				Class: css.New(
					u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
					css.Custom("padding-top", strconv.Itoa(tokens.Spacing.XS)+"px"),
				).String(),
			},
			below...,
		),
		Data: map[string]string{
			"component":   "settings-provider",
			"provider-id": provider.ID,
			"configured":  boolLabel(provider.Available),
		},
	})
}

// credentialField is where an operating-system reference is typed.
func credentialField(props Props, provider ProviderGroup, reference string) ui.Node {
	tokens := props.Mode.Tokens()
	fieldProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if props.OnReferenceInput != nil {
			props.OnReferenceInput(provider.ID, event.GetValue())
		}
	}))
	fieldProps.ID = "credential-" + provider.ID
	fieldProps.Type = "text"
	fieldProps.Value = reference
	fieldProps.Placeholder = "os://service/account"
	fieldProps.Disabled = props.OnReferenceInput == nil || props.Busy == provider.ID
	fieldProps.Aria = map[string]string{
		"label": "Operating-system credential reference for " + provider.Name,
	}
	fieldProps.Class = css.New(
		css.W(css.Full), css.MaxWidth(css.Px(rationaleMeasure)),
		css.MinHeight(css.Px(fieldHeight)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Code.Size)),
		css.Border(
			css.Px(tokens.Geometry.BorderWidth),
			css.Hex(string(tokens.Colors.BorderSubtle)),
		),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
	controls := []ui.Node{html.Input(fieldProps)}
	for _, model := range provider.Models {
		modelID := model.ID
		controls = append(controls, primitives.Button(primitives.ButtonProps{
			Compact: true,
			Label:   "Bind for " + model.Name, Mode: props.Mode,
			AccessibleLabel: "Configure " + provider.Name + " for " + model.Name +
				" with the entered credential reference",
			Busy:     props.Busy == provider.ID,
			Disabled: reference == "" || props.OnConfigure == nil || props.Busy == provider.ID,
			DisabledReason: disabledReason(
				reference == "", "Enter the os://service/account reference first.",
			),
			OnClick: func() {
				if props.OnConfigure != nil {
					props.OnConfigure(provider.ID, modelID)
				}
			},
		}))
	}
	return html.Div(
		html.Props{
			Class: css.New(
				u.Flex, css.FlexWrap.Wrap, u.ItemsCenter,
				css.Gap(css.Px(tokens.Spacing.XS)),
			).String(),
		},
		controls...,
	)
}

// flowStored is the value the coordinator last reported for one setting.
func flowStored(props Props, key string) (FlowSetting, bool) {
	for _, setting := range props.Flow {
		if setting.Key == key {
			return setting, true
		}
	}
	return FlowSetting{}, false
}

// flowValueText renders one setting's value in the machine's own words.
func flowValueText(setting FlowSetting) string {
	switch setting.Kind {
	case FlowChoice:
		if setting.Text == "" {
			return "not set"
		}
		return setting.Text
	case FlowNumber:
		return strconv.FormatInt(int64(setting.Number), 10)
	case FlowSwitch:
		if setting.Enabled {
			return "on"
		}
		return "off"
	case FlowSet:
		if len(setting.Items) == 0 {
			return "none"
		}
		return strings.Join(setting.Items, ", ")
	case FlowSequence:
		if len(setting.Items) == 0 {
			return "none"
		}
		// The arrow is the setting: a ladder is climbed in this order, and a
		// comma would read as a set that happens to be written down.
		return strings.Join(setting.Items, " → ")
	case FlowMapping:
		if len(setting.Pairs) == 0 {
			return "none"
		}
		parts := make([]string, 0, len(setting.Pairs))
		for _, pair := range setting.Pairs {
			parts = append(parts, pair.Key+"="+pair.Value)
		}
		return strings.Join(parts, "  ")
	}
	return "not shown"
}

package composer

import (
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type ViewModel struct {
	ThreadID       domain.ThreadID
	Draft          Draft
	SendStatus     SendStatus
	IdempotencyKey IdempotencyKey
	Retryable      bool
	SafeMessage    string
	CanSubmit      bool
	Task           TaskActionSet
}

func View(model Model, threadID domain.ThreadID, taskState domain.TaskState) ViewModel {
	view := ViewModel{
		ThreadID:   threadID,
		Draft:      model.Draft(threadID),
		SendStatus: SendIdle,
		CanSubmit:  model.CanSubmit(threadID),
		Task:       AvailableTaskActions(taskState),
	}
	if attempt, exists := model.Attempt(threadID); exists {
		view.SendStatus = attempt.Status()
		view.IdempotencyKey = attempt.Key()
		view.Retryable = attempt.Retryable()
		view.SafeMessage = attempt.SafeMessage()
	}
	return view
}

type ModelOption struct {
	Value ModelOverride
	Label string
}

type Props struct {
	View                      ViewModel
	Mode                      primitives.Mode
	Disabled                  bool
	DisabledReason            string
	MutationDisabled          bool
	MutationDisabledReason    string
	TransportMode             string
	BudgetCurrency            domain.CurrencyCode
	ModelOptions              []ModelOption
	OnTextChange              func(string)
	OnSubmitRequested         func()
	OnRetryRequested          func(IdempotencyKey)
	OnPolicyChange            func(domain.PolicyPreset, bool)
	OnBudgetMinorUnitsChange  func(string)
	OnModelChange             func(ModelOverride, bool)
	OnEffortChange            func(domain.ReasoningEffort, bool)
	OnOpenAttachmentPicker    func()
	AttachmentPickerOpen      bool
	AttachmentOptions         []RepositoryAttachment
	OnAttachmentSelected      func(RepositoryAttachment)
	OnAttachmentPickerDismiss func()
	OnRemoveAttachment        func(string)
	OnTaskAction              func(TaskAction)
	stableSubmit              ui.Handler
	stableTaskAction          ui.Handler
}

// Composer renders a controlled multiline GWC composer. All durable effects
// remain callbacks; this component owns no transport or browser persistence.
func Composer(props Props) ui.Node {
	busy := props.View.SendStatus == SendPending || props.View.SendStatus == SendAwaitingConfirmation
	editingDisabled := busy || props.Disabled || props.OnTextChange == nil
	mutationDisabled := props.Disabled || props.MutationDisabled
	composing := ui.UseRef(false)
	focus := ui.UseFocusManager()
	requestedSubmit := props.OnSubmitRequested
	stableSubmit := ui.UseEvent(func() {
		if requestedSubmit != nil {
			requestedSubmit()
		}
	})
	// This callback changes when the shell moves from its local preview to an
	// authoritative thread. Keep one GWC listener identity while always
	// invoking the latest transport-bound callback.
	props.stableSubmit = stableSubmit
	requestedTaskAction := props.OnTaskAction
	taskActions := props.View.Task
	props.stableTaskAction = ui.UseEvent(func(event ui.MouseEvent) {
		action := TaskAction(event.GetValue())
		if requestedTaskAction != nil && taskActions.Has(action) && taskActions.DisabledReason(action) == "" {
			requestedTaskAction(action)
		}
	})
	restoreAttachmentFocus := func() {
		ui.SafeGo("restore composer attachment focus", func() {
			// Let the overlay inert/background cleanup and controlled draft
			// rerender finish before moving focus to the stable attach trigger.
			time.Sleep(20 * time.Millisecond)
			focus.FocusByID("composer-attach")
		})
	}
	if onSelected := props.OnAttachmentSelected; onSelected != nil {
		props.OnAttachmentSelected = func(attachment RepositoryAttachment) {
			onSelected(attachment)
			restoreAttachmentFocus()
		}
	}
	if onRemove := props.OnRemoveAttachment; onRemove != nil {
		props.OnRemoveAttachment = func(key string) {
			onRemove(key)
			restoreAttachmentFocus()
		}
	}
	ui.UseDocumentEvent("compositionstart", func(ui.Event) { composing.Set(true) })
	ui.UseDocumentEvent("compositionend", func(ui.Event) { composing.Set(false) })

	textareaProps := html.PropsOf(
		html.OnInput(func(event ui.InputEvent) {
			if !busy && props.OnTextChange != nil {
				props.OnTextChange(event.GetValue())
			}
		}),
		html.OnKeyDown(func(event ui.KeyboardEvent) {
			decision := ResolveKeyboard(
				keyInputFromEvent(event, composing.Get()),
				props.View.CanSubmit && !mutationDisabled && props.OnSubmitRequested != nil,
			)
			if decision == KeyboardNone || decision == KeyboardNewline {
				return
			}
			event.PreventDefault()
			if decision == KeyboardSubmit && props.OnSubmitRequested != nil {
				props.OnSubmitRequested()
			}
		}),
	)
	textareaProps.ID = "thread-composer"
	textareaProps.Rows = 2
	textareaProps.Value = props.View.Draft.Text()
	textareaProps.Placeholder = "Describe the next change or ask a question"
	textareaProps.Disabled = editingDisabled
	describedBy := "composer-keyboard-help"
	if props.MutationDisabled && strings.TrimSpace(props.MutationDisabledReason) != "" {
		describedBy += " composer-mutation-disabled-reason"
	}
	textareaProps.Aria = map[string]string{
		"label": "Message", "multiline": "true", "describedby": describedBy,
	}
	textareaProps.Data = map[string]string{
		"component": "multiline-composer-input", "keyboard": "enter-submit-shift-enter-newline",
	}
	textareaProps.Class = composerInputClass(props.Mode.Tokens(), true)

	children := []ui.Node{
		html.Label(html.Props{
			For: "thread-composer", Text: "Message", Class: composerAssistiveClass(),
		}),
		html.Div(html.Props{Class: composerBarClass(props.Mode.Tokens())},
			html.Textarea(textareaProps, html.Text(props.View.Draft.Text())),
			html.Div(html.Props{
				Class: composerFooterClass(props.Mode.Tokens()),
			},
				html.Div(html.Props{
					Role: "group", Aria: map[string]string{"label": "Message options"},
					Class: composerCommandTrayClass(props.Mode.Tokens()),
				},
					composerAttachments(props, busy || mutationDisabled),
					composerOverrideControls(props, busy || mutationDisabled),
					composerTaskControls(props, busy, mutationDisabled),
				),
				composerSendControls(props, busy),
			),
		),
		composerAttachmentChips(props, busy || mutationDisabled),
		composerAttachmentPicker(props, busy || mutationDisabled),
	}
	if props.MutationDisabled && strings.TrimSpace(props.MutationDisabledReason) != "" {
		children = append(children, html.P(html.Props{
			ID: "composer-mutation-disabled-reason", Role: "status",
			Text: props.MutationDisabledReason, Class: composerMutationStatusClass(props.Mode.Tokens()),
		}))
	}
	children = append(children, html.P(html.Props{
		ID: "composer-keyboard-help", Text: "Enter sends. Shift+Enter inserts a newline.",
		Class: composerAssistiveClass(),
	}))
	return html.Section(html.Props{
		Aria: map[string]string{
			"label": "Message composer", "disabled": strconv.FormatBool(props.Disabled),
		},
		Data: map[string]string{
			"component": "composer", "send-state": string(props.View.SendStatus),
			"stop-reachable":           strconv.FormatBool(props.View.Task.StopImmediatelyReachable()),
			"disabled":                 strconv.FormatBool(props.Disabled),
			"disabled-reason":          props.DisabledReason,
			"mutation-disabled":        strconv.FormatBool(props.MutationDisabled),
			"mutation-disabled-reason": props.MutationDisabledReason,
			"transport-mode":           props.TransportMode,
		},
		Class: composerSurfaceClass(props.Mode.Tokens()),
	}, children...)
}

func composerAttachmentPicker(props Props, disabled bool) ui.Node {
	tokens := props.Mode.Tokens()
	selected := make(map[string]struct{})
	for _, attachment := range props.View.Draft.Attachments() {
		selected[attachment.Key()] = struct{}{}
	}
	options := make([]ui.Node, 0, len(props.AttachmentOptions))
	for _, attachment := range props.AttachmentOptions {
		attachment := attachment
		if attachment.Validate() != nil {
			continue
		}
		_, alreadySelected := selected[attachment.Key()]
		label := "Attach " + attachment.DisplayLabel()
		if alreadySelected {
			label = "Already attached " + attachment.DisplayLabel()
		}
		options = append(options, primitives.Button(primitives.ButtonProps{
			Label:           attachmentGlyph(attachment.Kind()) + "  " + attachment.DisplayLabel(),
			AccessibleLabel: label,
			Mode:            props.Mode,
			Disabled:        disabled || alreadySelected || props.OnAttachmentSelected == nil,
			OnClick: func() {
				if props.OnAttachmentSelected != nil {
					props.OnAttachmentSelected(attachment)
				}
			},
		}))
	}
	if len(options) == 0 {
		options = append(options, html.P(html.Props{
			Role: "status", Text: "No repository files or symbols are available.",
			Class: css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextSecondary)))).String(),
		}))
	}
	return primitives.Dialog(primitives.OverlayProps{
		ID: "composer-attachment-picker", Open: props.AttachmentPickerOpen,
		LabelledBy:           "composer-attachment-picker-title",
		DescribedBy:          "composer-attachment-picker-description",
		InitialFocusSelector: "#composer-attachment-picker-close",
		AppRootSelector:      `[data-component="app-shell"]`,
		Mode:                 props.Mode, OnDismiss: props.OnAttachmentPickerDismiss,
		Content: html.Section(html.Props{
			Data: map[string]string{
				"component":    "repository-attachment-picker",
				"authority":    "server-identities-only",
				"focus-return": "composer-attach",
			},
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD))).String(),
		},
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.MD))).String(),
			},
				html.Div(html.Props{},
					html.H2(html.Props{
						ID:    "composer-attachment-picker-title",
						Class: composerDialogTitleClass(tokens),
						Text:  "Attach repository context",
					}),
					html.P(html.Props{
						ID:    "composer-attachment-picker-description",
						Class: composerDialogDescriptionClass(tokens),
						Text:  "Choose a server-resolved file or symbol. Browser file paths are never accepted.",
					}),
				),
				primitives.Button(primitives.ButtonProps{
					ID: "composer-attachment-picker-close", Label: "×",
					AccessibleLabel: "Close attachment picker", Mode: props.Mode,
					Disabled: props.OnAttachmentPickerDismiss == nil,
					OnClick:  props.OnAttachmentPickerDismiss,
				}),
			),
			html.Div(html.Props{
				Role: "group", Aria: map[string]string{"label": "Repository attachment choices"},
				Class: css.New(u.Grid, css.GridCols(css.Repeat(1, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.SM))).String(),
			}, options...),
		),
	})
}

func attachmentGlyph(kind AttachmentKind) string {
	if kind == AttachmentSymbol {
		return "ƒ"
	}
	return "▤"
}

func composerOverrideControls(props Props, disabled bool) ui.Node {
	policy, hasPolicy := props.View.Draft.PolicyOverride()
	policyValue := ""
	if hasPolicy {
		policyValue = string(policy)
	}
	policyProps := html.PropsOf(html.OnChange(func(event ui.ChangeEvent) {
		value := domain.PolicyPreset(event.GetValue())
		if props.OnPolicyChange != nil {
			props.OnPolicyChange(value, value == "")
		}
	}))
	policyProps.ID = "composer-policy"
	policyProps.Value = policyValue
	policyProps.Disabled = disabled || props.OnPolicyChange == nil
	policyProps.Aria = map[string]string{"label": "Cost speed correctness policy"}
	policyProps.Class = composerInputClass(props.Mode.Tokens(), false)

	budgetValue := ""
	budget, hasBudget := props.View.Draft.BudgetOverride()
	budgetCurrency := props.BudgetCurrency
	if hasBudget {
		budgetValue = strconv.FormatInt(budget.MinorUnits, 10)
		budgetCurrency = budget.Currency
	}
	budgetLabel := "Hard budget (exact minor units)"
	budgetAriaLabel := "Hard budget in exact currency minor units"
	if currency, err := domain.ParseCurrencyCode(string(budgetCurrency)); err == nil {
		budgetLabel = "Hard budget (" + string(currency) + " minor units)"
		budgetAriaLabel = "Hard budget in exact " + string(currency) + " minor units"
	}
	budgetProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if props.OnBudgetMinorUnitsChange != nil {
			props.OnBudgetMinorUnitsChange(event.GetValue())
		}
	}))
	budgetProps.ID = "composer-hard-budget"
	budgetProps.Type = "number"
	budgetProps.Value = budgetValue
	budgetProps.Min = "1"
	budgetProps.Step = "1"
	budgetProps.Disabled = disabled || props.OnBudgetMinorUnitsChange == nil
	budgetProps.Aria = map[string]string{"label": budgetAriaLabel}
	budgetProps.Class = composerInputClass(props.Mode.Tokens(), false)

	model, hasModel := props.View.Draft.ModelOverride()
	modelValue := ""
	if hasModel {
		modelValue = model.Key()
	}
	modelProps := html.PropsOf(html.OnChange(func(event ui.ChangeEvent) {
		selected := event.GetValue()
		if props.OnModelChange == nil {
			return
		}
		if selected == "" {
			props.OnModelChange(ModelOverride{}, true)
			return
		}
		for _, option := range props.ModelOptions {
			if option.Value.Key() == selected {
				props.OnModelChange(option.Value, false)
				return
			}
		}
	}))
	modelProps.ID = "composer-model"
	modelProps.Value = modelValue
	modelProps.Disabled = disabled || props.OnModelChange == nil
	modelProps.Aria = map[string]string{"label": "Optional model override"}
	modelProps.Class = composerInputClass(props.Mode.Tokens(), false)

	effort, hasEffort := props.View.Draft.EffortOverride()
	effortValue := ""
	if hasEffort {
		effortValue = string(effort)
	}
	effortProps := html.PropsOf(html.OnChange(func(event ui.ChangeEvent) {
		value := domain.ReasoningEffort(event.GetValue())
		if props.OnEffortChange != nil {
			props.OnEffortChange(value, value == "")
		}
	}))
	effortProps.ID = "composer-effort"
	effortProps.Value = effortValue
	effortProps.Disabled = disabled || props.OnEffortChange == nil
	effortProps.Aria = map[string]string{"label": "Optional reasoning effort override"}
	effortProps.Class = composerInputClass(props.Mode.Tokens(), false)

	return html.Details(html.Props{
		Data:  map[string]string{"component": "composer-advanced-options"},
		Class: composerDetailsClass(props.Mode.Tokens()),
	},
		html.Summary(html.Props{
			Aria:  map[string]string{"label": "Show policy, budget, model, and effort options"},
			Class: composerSummaryClass(props.Mode.Tokens()), Text: "Options",
		}),
		html.Div(html.Props{
			Role: "group", Aria: map[string]string{"label": "Composer policy and budget overrides"},
			Class: composerOverrideGridClass(props.Mode.Tokens()),
		},
			composerLabeledControl(props.Mode.Tokens(), "composer-policy", "Policy", html.Select(policyProps,
				html.Option(html.Props{Value: "", Text: "Use default policy"}),
				html.Option(html.Props{Value: string(domain.PolicyPresetCorrectness), Text: "Correctness"}),
				html.Option(html.Props{Value: string(domain.PolicyPresetBalanced), Text: "Balanced"}),
				html.Option(html.Props{Value: string(domain.PolicyPresetFast), Text: "Fast"}),
				html.Option(html.Props{Value: string(domain.PolicyPresetEconomical), Text: "Economical"}),
			)),
			composerLabeledControl(props.Mode.Tokens(), "composer-hard-budget", budgetLabel, html.Input(budgetProps)),
			composerLabeledControl(props.Mode.Tokens(), "composer-model", "Model override",
				html.Select(modelProps, composerModelOptions(props.ModelOptions)...)),
			composerLabeledControl(props.Mode.Tokens(), "composer-effort", "Reasoning effort override", html.Select(effortProps,
				html.Option(html.Props{Value: "", Text: "Use default effort"}),
				html.Option(html.Props{Value: string(domain.ReasoningEffortMinimal), Text: "Minimal"}),
				html.Option(html.Props{Value: string(domain.ReasoningEffortStandard), Text: "Standard"}),
				html.Option(html.Props{Value: string(domain.ReasoningEffortExtended), Text: "Extended"}),
				html.Option(html.Props{Value: string(domain.ReasoningEffortMaximum), Text: "Maximum"}),
			)),
		),
	)
}

func composerTaskControls(props Props, busy, disabled bool) ui.Node {
	return html.Details(html.Props{
		Data:  map[string]string{"component": "composer-task-controls"},
		Class: composerDetailsClass(props.Mode.Tokens()),
	},
		html.Summary(html.Props{
			Aria:  map[string]string{"label": "Show task controls"},
			Class: composerSummaryClass(props.Mode.Tokens()),
			Text:  "Task controls",
		}),
		html.Div(html.Props{
			Role:  "group",
			Aria:  map[string]string{"label": "Task controls"},
			Class: composerTaskMenuClass(props.Mode.Tokens()),
		}, composerTaskActions(props, busy, disabled)),
	)
}

func composerLabeledControl(tokens design.Tokens, id, label string, control ui.Node) ui.Node {
	return html.Div(html.Props{Class: composerStackClass(tokens)},
		html.Label(html.Props{For: id, Text: label}), control,
	)
}

func composerModelOptions(options []ModelOption) []ui.Node {
	nodes := []ui.Node{html.Option(html.Props{Value: "", Text: "Use default model"})}
	for _, option := range options {
		nodes = append(nodes, html.Option(html.Props{
			Value: option.Value.Key(), Text: option.Label,
		}))
	}
	return nodes
}

func composerAttachments(props Props, disabled bool) ui.Node {
	return primitives.Button(primitives.ButtonProps{
		ID: "composer-attach", Label: "Attach", AccessibleLabel: "Attach file or symbol",
		Disabled: disabled || props.OnOpenAttachmentPicker == nil, Mode: props.Mode,
		OnClick: props.OnOpenAttachmentPicker,
	})
}

func composerAttachmentChips(props Props, disabled bool) ui.Node {
	attachments := props.View.Draft.Attachments()
	if len(attachments) == 0 {
		return nil
	}
	chips := make([]ui.Node, 0, len(attachments))
	for _, attachment := range attachments {
		key := attachment.Key()
		chips = append(chips, html.Li(html.Props{
			Data: map[string]string{
				"component": "attachment-chip", "kind": string(attachment.Kind()),
				"server-identity": attachment.Identity(),
			},
			Class: composerChipClass(props.Mode.Tokens()),
		},
			html.Span(html.Props{Text: attachment.DisplayLabel()}),
			primitives.Button(primitives.ButtonProps{
				Label: "Remove", AccessibleLabel: "Remove attachment " + attachment.DisplayLabel(),
				Disabled: disabled || props.OnRemoveAttachment == nil, Mode: props.Mode,
				OnClick: func() {
					if props.OnRemoveAttachment != nil {
						props.OnRemoveAttachment(key)
					}
				},
			}),
		))
	}
	return html.Ul(html.Props{
		Aria:  map[string]string{"label": "Selected attachments"},
		Data:  map[string]string{"focus-return": "composer-attach"},
		Class: composerActionsClass(props.Mode.Tokens()),
	}, chips...)
}

func composerSendControls(props Props, busy bool) ui.Node {
	controls := []ui.Node{primitives.Button(primitives.ButtonProps{
		ID: "composer-submit", Label: "Send", Primary: true,
		Disabled: !props.View.CanSubmit || props.Disabled || props.MutationDisabled || props.OnSubmitRequested == nil,
		Busy:     busy, Mode: props.Mode,
		DescribedBy: func() string {
			if props.MutationDisabled && strings.TrimSpace(props.MutationDisabledReason) != "" {
				return "composer-mutation-disabled-reason"
			}
			return ""
		}(),
		OnClickHandler: props.stableSubmit, StableOnClick: true,
	})}
	if props.View.SendStatus == SendFailed {
		controls = append(controls,
			html.P(html.Props{ID: "composer-send-error", Role: "alert", Text: props.View.SafeMessage}),
			primitives.Button(primitives.ButtonProps{
				ID: "composer-retry", Label: "Retry send",
				Disabled: !props.View.Retryable || props.Disabled || props.MutationDisabled || props.OnRetryRequested == nil,
				Mode:     props.Mode,
				OnClick: func() {
					if props.OnRetryRequested != nil {
						props.OnRetryRequested(props.View.IdempotencyKey)
					}
				},
			}),
		)
	}
	if props.View.SendStatus == SendAwaitingConfirmation {
		controls = append(controls, html.P(html.Props{
			ID: "composer-send-confirmation", Role: "status",
			Text: "Message accepted. Your draft is preserved until it appears in the authoritative timeline.",
		}))
	}
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Message send actions"},
		Class: composerActionsClass(props.Mode.Tokens()),
	}, controls...)
}

func composerTaskActions(props Props, busy, disabled bool) ui.Node {
	buttons := make([]ui.Node, 0, len(props.View.Task.Actions))
	for _, action := range props.View.Task.Actions {
		if action == ActionSend || action == ActionChangePolicy {
			continue
		}
		action := action
		reason := strings.TrimSpace(props.View.Task.DisabledReason(action))
		if props.OnTaskAction == nil && reason == "" {
			reason = "This action is not currently available."
		}
		reasonID := "composer-task-action-" + string(action) + "-reason"
		children := []ui.Node{primitives.Button(primitives.ButtonProps{
			ID: "composer-task-action-" + string(action), Label: taskActionLabel(action),
			Value:       string(action),
			Disabled:    disabled || reason != "" || props.OnTaskAction == nil || (busy && action != ActionStop),
			DescribedBy: map[bool]string{true: reasonID}[reason != ""],
			Mode:        props.Mode, OnClickHandler: props.stableTaskAction, StableOnClick: true,
		})}
		if reason != "" {
			children = append(children, html.P(html.Props{
				ID: reasonID, Text: reason,
			}))
		}
		buttons = append(buttons, html.Span(html.Props{
			Data: map[string]string{
				"task-action":           string(action),
				"immediately-reachable": strconv.FormatBool(action == ActionStop),
				"disabled-reason":       reason,
			},
		}, children...))
	}
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Task actions"},
		DataAttr: html.DataAttribute{Name: "task-message", Value: props.View.Task.PrimaryMessage},
		Class:    composerActionsClass(props.Mode.Tokens()),
	}, buttons...)
}

func composerSurfaceClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
		css.MinWidth(css.Zero), css.MaxWidth(css.Full),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.UI)),
	).String()
}

func composerInputClass(tokens design.Tokens, multiline bool) string {
	rules := []css.Rule{
		css.MinWidth(css.Zero),
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.LG)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
	}
	if multiline {
		rules = append(rules,
			css.FlexGrow(css.Num(1)), css.MinWidth(css.Px(240)),
			css.MinHeight(css.Px(72)), css.PaddingY(css.Px(tokens.Spacing.MD)),
		)
	} else {
		rules = append(rules, css.W(css.Full))
	}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func composerBarClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Zero),
	).String()
}

func composerCommandTrayClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.ItemsCenter, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinWidth(css.Zero),
	).String()
}

func composerFooterClass(tokens design.Tokens) string {
	rules := []css.Rule{
		u.Flex, u.ItemsCenter, u.JustifyBetween,
		css.Gap(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Zero),
	}
	rules = append(rules, css.Media(
		css.MaxW(560),
		u.FlexCol,
		u.ItemsStretch,
	)...)
	return css.New(rules...).String()
}

func composerTaskMenuClass(tokens design.Tokens) string {
	return css.New(
		u.Absolute,
		css.Left(css.Zero),
		css.Bottom(css.RawLength("calc(100% + 6px)")),
		css.ZIndex(20),
		css.MinWidth(css.Px(230)),
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.Padding(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(14), css.Px(34), css.Px(-14), css.RGBA(0, 0, 0, 0.72),
		)),
	).String()
}

func composerDetailsClass(tokens design.Tokens) string {
	return css.New(css.MinWidth(css.Zero), css.Position.Relative).String()
}

func composerSummaryClass(tokens design.Tokens) string {
	return css.New(
		u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)), css.Cursor.Pointer,
	).String()
}

func composerAssistiveClass() string {
	return css.New(
		css.Position.Absolute, css.W(css.Px(1)), css.H(css.Px(1)),
		css.Margin(css.Px(-1)), css.Padding(css.Zero), css.Overflow.Hidden,
	).String()
}

func composerMutationStatusClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero), css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.TextColor(css.Hex(string(tokens.Colors.Warning))),
	).String()
}

func composerOverrideGridClass(tokens design.Tokens) string {
	return css.New(
		u.Grid,
		css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))),
		css.Gap(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Px(360)),
		css.MarginY(css.Px(tokens.Spacing.SM)), css.Padding(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
	).String()
}

func composerStackClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero),
	).String()
}

func composerActionsClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.ItemsCenter, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.SM)),
		css.Margin(css.Zero), css.Padding(css.Zero),
	).String()
}

func composerChipClass(tokens design.Tokens) string {
	return css.New(
		u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
}

func taskActionLabel(action TaskAction) string {
	labels := map[TaskAction]string{
		ActionStop: "Stop", ActionApprovePlan: "Approve plan", ActionRequestChange: "Request change",
		ActionStart: "Start", ActionPause: "Pause", ActionInspectGraph: "Inspect graph",
		ActionAllowOnce: "Allow once", ActionAllowForTask: "Allow for this task", ActionDeny: "Deny",
		ActionResume: "Resume", ActionReview: "Review", ActionInspectChecks: "Inspect checks",
		ActionAccept: "Accept", ActionRepair: "Repair", ActionReject: "Reject", ActionRollback: "Roll back",
		ActionSafeResume: "Safe resume", ActionReconcile: "Reconcile", ActionPreservePatch: "Preserve patch",
		ActionAbandon: "Abandon", ActionInspectEvidence: "Inspect evidence",
		ActionStartRelatedTask: "Start related task", ActionInspect: "Inspect",
		ActionNewAttempt: "New attempt", ActionResumeNewPlan: "Resume from new plan", ActionFinish: "Finish",
	}
	if label := labels[action]; label != "" {
		return label
	}
	return string(action)
}

// composerDialogTitleClass names the attachment picker.
//
// The dialog title is set in the serif with the rest of the material a person
// reads, at the section-title size rather than the browser's own heading size.
func composerDialogTitleClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Display)),
		css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
		css.FontWeight.Normal,
	).String()
}

// composerDialogDescriptionClass sets the sentence under that title.
func composerDialogDescriptionClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.MaxWidth(css.Ch(74)),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
	).String()
}

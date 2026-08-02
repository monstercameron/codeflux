// Package settingsview renders the server-backed settings surfaces: the
// policy that governs every run, the providers the coordinator has recorded,
// and the models catalogued against them.
//
// It receives already-projected rows and holds no state of its own, so a
// settings route surface stays free of hooks and nothing here can invent a
// value the coordinator did not answer with.
package settingsview

import (
	"strconv"
	"time"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ModelRow is one catalogued model.
type ModelRow struct {
	ID   string
	Name string
	// Available reports that the coordinator could use this model now: its
	// provider is enabled and a credential is bound to it. It is not a claim
	// that the provider answered.
	Available bool
}

// ProviderGroup is one provider and the models catalogued for it.
type ProviderGroup struct {
	ID        string
	Name      string
	Available bool
	Models    []ModelRow
}

// CredentialCheck is the outcome of one credential check.
//
// Summary is the coordinator's own sentence and is rendered unchanged. It
// always states that no provider request was made, so this surface cannot
// present a local check as a live connection test.
type CredentialCheck struct {
	Running  bool
	Resolved bool
	Summary  string
}

// PolicyRow is the execution policy in force.
type PolicyRow struct {
	Preset          string
	ReasoningEffort string
	RiskFloor       string
	AssuranceFloor  string
	RequestTimeout  time.Duration
	Revision        uint64
	// Known distinguishes "the coordinator has not answered yet" from "the
	// coordinator answered with empty fields".
	Known bool
}

// Props is everything the settings surfaces draw from.
type Props struct {
	Mode primitives.Mode
	// Loading, Failed, and Unavailable are the three states this surface can be
	// in besides having an answer. Unavailable carries its own reason because a
	// surface that cannot be asked is not the same as one that failed.
	Loading           bool
	Failed            bool
	Unavailable       bool
	UnavailableReason string

	Policy    PolicyRow
	Providers []ProviderGroup

	// Reference holds what has been typed for each provider, keyed by provider
	// identity. It is browser-only interaction state; the value it produces is
	// an opaque operating-system reference, never a credential.
	Reference map[string]string
	// Checks holds the last credential check for each provider.
	Checks map[string]CredentialCheck
	// Busy names the provider whose configuration is in flight, so its controls
	// report busy rather than inviting a second submission.
	Busy string
	// Notice reports the outcome of the last configuration attempt.
	Notice     string
	NoticeTone design.Status

	OnReload          func()
	OnReferenceInput  func(providerID, value string)
	OnCheckCredential func(providerID string)
	OnConfigure       func(providerID, modelID string)
}

// Policy renders what governs every run.
func Policy(props Props) ui.Node {
	if node, handled := stateNode(props, "policy"); handled {
		return node
	}
	if !props.Policy.Known {
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No policy answer",
			Body:  "The coordinator has not reported the policy governing runs.",
			Mode:  props.Mode,
		})
	}
	revision := "compiled defaults"
	if props.Policy.Revision > 0 {
		revision = "settings revision " + strconv.FormatUint(props.Policy.Revision, 10)
	}
	terms := []ui.Node{
		policyTerm("Preset", props.Policy.Preset),
		policyTerm("Reasoning effort", props.Policy.ReasoningEffort),
		policyTerm("Risk floor", props.Policy.RiskFloor),
		policyTerm("Required assurance floor", props.Policy.AssuranceFloor),
	}
	// The request timeout arrives with the model list, because that is the one
	// place the coordinator states it. A coordinator with no provider recorded
	// has not reported one, and a row reading "unknown" would say the timeout
	// is unknown to the product rather than not yet answered here.
	if props.Policy.RequestTimeout > 0 {
		terms = append(terms, policyTerm(
			"Request timeout", props.Policy.RequestTimeout.String(),
		))
	}
	terms = append(terms, policyTerm("Source", revision))
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": "Execution policy in force"},
			Data: map[string]string{"component": "settings-policy"},
		},
		html.P(html.Props{
			Text: "Routing uses one versioned policy for this prototype. These values " +
				"are reported so you can see what governs a run; they are not " +
				"controls, and nothing here changes them.",
		}),
		html.Tag("dl", html.Props{}, terms...),
	)
}

// Providers renders the provider configurations and their controls.
func Providers(props Props) ui.Node {
	if node, handled := stateNode(props, "providers"); handled {
		return node
	}
	if len(props.Providers) == 0 {
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No provider is recorded",
			Body: "The coordinator records a provider the first time it prepares work " +
				"for one. Until then there is nothing here to configure.",
			ActionLabel: reloadLabel(props.OnReload),
			Mode:        props.Mode,
			OnAction:    props.OnReload,
		})
	}
	children := []ui.Node{html.P(html.Props{
		Text: "A credential is stored in this machine's operating-system credential " +
			"store. CodeFlux keeps only an os://service/account reference to it and " +
			"never displays or transmits the credential itself.",
	})}
	if props.Notice != "" {
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Provider configuration", Message: props.Notice,
			Tone: props.NoticeTone, Mode: props.Mode,
		}))
	}
	for _, provider := range props.Providers {
		children = append(children, providerCard(props, provider))
	}
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": "Provider configurations"},
			Data: map[string]string{"component": "settings-providers"},
		},
		children...,
	)
}

// Models renders the catalogued models and whether each can be used now.
func Models(props Props) ui.Node {
	if node, handled := stateNode(props, "models"); handled {
		return node
	}
	rows := make([]ui.Node, 0)
	for _, provider := range props.Providers {
		for _, model := range provider.Models {
			rows = append(rows, html.Li(
				html.Props{
					Data: map[string]string{
						"component": "settings-model",
						"model-id":  model.ID,
						"available": boolLabel(model.Available),
					},
				},
				html.Span(html.Props{Text: provider.Name + " · " + model.Name}),
				html.Text(" "),
				availabilityBadge(props.Mode, model.Available),
			))
		}
	}
	if len(rows) == 0 {
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No model is catalogued",
			Body: "A model is catalogued with the exact revision and capabilities the " +
				"coordinator recorded for it. Nothing has been recorded yet.",
			ActionLabel: reloadLabel(props.OnReload),
			Mode:        props.Mode,
			OnAction:    props.OnReload,
		})
	}
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": "Catalogued models"},
			Data: map[string]string{"component": "settings-models"},
		},
		html.P(html.Props{
			Text: "Availability means a credential is bound and the provider is enabled. " +
				"It is not evidence that the provider answered.",
		}),
		html.Ul(html.Props{Aria: map[string]string{"label": "Catalogued models"}}, rows...),
	)
}

// providerCard renders one provider, its credential state, and its controls.
func providerCard(props Props, provider ProviderGroup) ui.Node {
	children := []ui.Node{
		html.H3(html.Props{Text: provider.Name}),
		availabilityBadge(props.Mode, provider.Available),
	}
	if check, present := props.Checks[provider.ID]; present {
		switch {
		case check.Running:
			children = append(children, html.P(html.Props{
				Text: "Checking the credential…", Aria: map[string]string{"live": "polite"},
			}))
		case check.Summary != "":
			tone := design.StatusWarning
			if check.Resolved {
				tone = design.StatusSuccess
			}
			children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
				Title: "Credential check", Message: check.Summary,
				Tone: tone, Mode: props.Mode,
			}))
		}
	}
	busy := props.Busy == provider.ID
	children = append(children, primitives.Button(primitives.ButtonProps{
		Label: "Check credential", Mode: props.Mode,
		AccessibleLabel: "Check the stored credential for " + provider.Name,
		Busy:            props.Checks[provider.ID].Running,
		Disabled:        props.OnCheckCredential == nil,
		DisabledReason:  disabledReason(props.OnCheckCredential == nil, "This surface is not connected to the coordinator."),
		OnClick: func() {
			if props.OnCheckCredential != nil {
				props.OnCheckCredential(provider.ID)
			}
		},
	}))
	reference := props.Reference[provider.ID]
	children = append(children, primitives.TextField(primitives.TextFieldProps{
		ID: "provider-reference-" + provider.ID, Label: "Credential reference",
		AccessibleLabel: "Operating-system credential reference for " + provider.Name,
		Value:           reference, Placeholder: "os://service/account",
		Disabled: busy || props.OnReferenceInput == nil, Mode: props.Mode,
		OnInput: func(value string) {
			if props.OnReferenceInput != nil {
				props.OnReferenceInput(provider.ID, value)
			}
		},
	}))
	if len(provider.Models) == 0 {
		// The request contract names the model a credential is configured for,
		// and the coordinator refuses one it has never catalogued. Saying so is
		// more useful than a control that fails on click.
		children = append(children, html.P(html.Props{
			Text: "No model is catalogued for this provider yet, so a credential " +
				"cannot be configured against one.",
		}))
		return providerSection(props, provider, children)
	}
	for _, model := range provider.Models {
		modelID := model.ID
		blocked := reference == "" || props.OnConfigure == nil
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Use for " + model.Name, Mode: props.Mode,
			AccessibleLabel: "Configure " + provider.Name + " for " + model.Name +
				" with the entered credential reference",
			Busy: busy, Disabled: blocked || busy,
			DisabledReason: disabledReason(
				reference == "",
				"Enter the os://service/account reference first.",
			),
			OnClick: func() {
				if props.OnConfigure != nil {
					props.OnConfigure(provider.ID, modelID)
				}
			},
		}))
	}
	return providerSection(props, provider, children)
}

func providerSection(props Props, provider ProviderGroup, children []ui.Node) ui.Node {
	_ = props
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": provider.Name},
			Data: map[string]string{
				"component":   "settings-provider",
				"provider-id": provider.ID,
				"configured":  boolLabel(provider.Available),
			},
		},
		children...,
	)
}

// stateNode renders the states in which a surface has no answer to draw.
func stateNode(props Props, subject string) (ui.Node, bool) {
	switch {
	case props.Unavailable:
		reason := props.UnavailableReason
		if reason == "" {
			reason = "This surface cannot be read yet."
		}
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "Not available", Body: reason, Mode: props.Mode,
		}), true
	case props.Loading:
		return html.P(html.Props{
			Text: "Loading " + subject + "…", Aria: map[string]string{"live": "polite"},
		}), true
	case props.Failed:
		return primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Settings could not be read",
			Body: "The coordinator did not answer. Nothing was changed, and the " +
				"stored configuration is unaffected.",
			ActionLabel: reloadLabel(props.OnReload),
			Mode:        props.Mode,
			OnAction:    props.OnReload,
		}), true
	default:
		return nil, false
	}
}

func policyTerm(term, value string) ui.Node {
	if value == "" {
		value = "unknown"
	}
	return html.Fragment(
		html.Tag("dt", html.Props{Text: term}),
		html.Tag("dd", html.Props{Text: value}),
	)
}

func availabilityBadge(mode primitives.Mode, available bool) ui.Node {
	if available {
		return primitives.Badge(primitives.BadgeProps{
			Label: "Credential bound", Status: design.StatusSuccess, Mode: mode,
		})
	}
	return primitives.Badge(primitives.BadgeProps{
		Label: "Not configured", Status: design.StatusWarning, Mode: mode,
	})
}

func reloadLabel(onReload func()) string {
	if onReload == nil {
		return ""
	}
	return "Reload settings"
}

func disabledReason(disabled bool, reason string) string {
	if !disabled {
		return ""
	}
	return reason
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// Package settingsview renders the server-backed settings surfaces: the
// policy that governs every run, the providers the coordinator has recorded,
// and the models catalogued against them.
//
// It receives already-projected rows and holds no state of its own, so a
// settings route surface stays free of hooks and nothing here can invent a
// value the coordinator did not answer with.
package settingsview

import (
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

// FlowSetting is one choice the run flow leaves open, with the description the
// coordinator sent and the value in force.
//
// The description is rendered rather than restated here: the engine declares
// what each setting costs and what bounds it accepts, and a page that carried
// its own copy would eventually offer a value the engine refuses.
type FlowSetting struct {
	Key     string
	Label   string
	Help    string
	Kind    string
	Choices []string
	Minimum int32
	Maximum int32
	Group   string
	Text    string
	Number  int32
	Enabled bool
	// AtDefault reports that the value in force is the one the engine ships
	// with, so the sheet can mark where this machine has departed from them.
	AtDefault bool
	// Items carries a setting whose value is a list of choices: a set, whose
	// order says nothing, or a sequence, whose order is the setting.
	Items []string
	// Pairs carries a setting that gives named keys their own choice.
	Pairs []FlowSettingPair
}

// FlowSettingPair is one named key and the choice given to it.
type FlowSettingPair struct {
	Key   string
	Value string
}

// Flow setting kinds, as the coordinator names them.
const (
	FlowChoice = "choice"
	FlowNumber = "number"
	FlowSwitch = "switch"
	// FlowSet, FlowSequence, and FlowMapping are values this sheet reports but
	// does not yet offer a control for. Showing the value without a control is
	// the honest half: a person can see what governs their runs even where
	// this surface cannot change it.
	FlowSet      = "set"
	FlowSequence = "sequence"
	FlowMapping  = "mapping"
)

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

	// Spend is what the recorded work cost, sliced by the flow's own phases
	// and by model. It sits on this surface because a budget a person can see
	// and a spend they cannot is half an answer.
	Spend SpendPanel

	// Appearance, LocalData, and Telemetry are the panels that belong to this
	// browser rather than to a run, supplied by the application that owns those
	// choices. They are nodes because the sheet places them; it does not know
	// what is in them.
	Appearance ui.Node
	LocalData  ui.Node
	Telemetry  ui.Node

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

	// Flow is every choice the run flow leaves open. FlowPending holds what has
	// been changed on screen and not yet saved, so a person can see what they
	// are about to commit before they commit it.
	Flow []FlowSetting
	// FlowUnrenderable names settings the coordinator declares that this
	// surface cannot draw, so the sheet can say a row is missing rather than
	// quietly understating what governs a run.
	FlowUnrenderable  []string
	FlowPending       map[string]FlowSetting
	FlowBusy          bool
	FlowNotice        string
	FlowTone          design.Status
	FlowRevisionKnown bool

	// Search is what has been typed to find a setting, and Jumped is the one a
	// result sent somebody to, so the row can say "this is the one" when they
	// arrive at a page of rows that otherwise look alike.
	Search string
	Jumped string

	OnReload          func()
	OnSearch          func(query string)
	OnJump            func(key string)
	OnFlowChoice      func(key, value string)
	OnFlowNumber      func(key string, value int32)
	OnFlowSwitch      func(key string)
	OnFlowSave        func()
	OnFlowDiscard     func()
	OnReferenceInput  func(providerID, value string)
	OnCheckCredential func(providerID string)
	OnConfigure       func(providerID, modelID string)
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

package settingsview_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/settingsview"
)

// describedFlow is the shape the coordinator sends: description and value
// together, in the order and grouping it chose.
func describedFlow() []settingsview.FlowSetting {
	return []settingsview.FlowSetting{
		{
			Key: "ambiguity", Label: "When a request reads two ways",
			Help: "Ask stops and puts the question to you. Assume states the " +
				"narrowest defensible reading.",
			Kind: settingsview.FlowChoice, Group: "Intake",
			Choices: []string{"ask", "assume"}, Text: "ask", AtDefault: true,
		},
		{
			Key: "maximum_attempts", Label: "Attempts before stopping",
			Help: "Each attempt is a full model round, so this is the main " +
				"thing a run costs.",
			Kind: settingsview.FlowNumber, Group: "Refinement",
			Minimum: 1, Maximum: 12, Number: 6, AtDefault: true,
		},
		{
			Key: "mutation_threshold_percent", Label: "Defects the tests must catch",
			Help: "Deliberate faults are injected and the tests are run.",
			Kind: settingsview.FlowNumber, Group: "Verification",
			Minimum: 0, Maximum: 100, Number: 50, AtDefault: true,
		},
		{
			Key: "repetition_runs", Label: "Times to repeat the suite",
			Help: "Repeating tells a correct program from an intermittently correct one.",
			Kind: settingsview.FlowNumber, Group: "Verification",
			Minimum: 1, Maximum: 20, Number: 3, AtDefault: true,
		},
		{
			Key: "adversarial_review", Label: "Review work that already passes",
			Help: "Attacks work once every gate holds.",
			Kind: settingsview.FlowSwitch, Group: "Quality", Enabled: true,
			AtDefault: true,
		},
	}
}

func TestARunSettingRendersAsTheKindTheEngineDeclared(t *testing.T) {
	props := answeredSheet()
	props.OnFlowChoice = func(string, string) {}
	props.OnFlowNumber = func(string, int32) {}
	props.OnFlowSwitch = func(string) {}
	markup := renderSettings(t, settingsview.Sheet(props))
	for _, want := range []string{
		// The engine's own groups, not a taxonomy this page invented.
		"Intake", "Refinement", "Quality",
		// The rationale is what a person decides on, so it is rendered whole.
		"Each attempt is a full model round",
		// Every discrete value is one segmented control, and the option in
		// force is marked rather than restated beside it.
		`aria-pressed="true"`,
		`data-component="settings-segmented"`,
		// A switch offers both states, so which one is off is as visible as
		// which one is on.
		`data-option="off"`,
		// A number is a stepper whose bounds are enforced by the control.
		`data-component="settings-stepper"`,
		// A bound is stated beside the value rather than inside a field label.
		"1–12",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup lacks %q: %s", want, markup)
		}
	}
}

func TestAValueOutsideTheEnginesBoundIsMarkedBeforeItIsSent(t *testing.T) {
	props := answeredSheet()
	props.FlowPending = map[string]settingsview.FlowSetting{
		"maximum_attempts": {
			Key: "maximum_attempts", Kind: settingsview.FlowNumber, Number: 400,
		},
	}
	props.OnFlowNumber = func(string, int32) {}
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, `aria-invalid="true"`) {
		t.Fatalf("an out-of-range value must be marked where it was typed: %s", markup)
	}
}

func TestABusySaveDoesNotInviteASecond(t *testing.T) {
	props := answeredSheet()
	props.FlowBusy = true
	props.FlowPending = map[string]settingsview.FlowSetting{
		"adversarial_review": {Key: "adversarial_review", Kind: settingsview.FlowSwitch},
	}
	props.OnFlowSave = func() {}
	bar := renderSettings(t, settingsview.CommitBar(props))
	if !strings.Contains(bar, `aria-busy="true"`) {
		t.Fatalf("a save in flight must report itself busy: %s", bar)
	}
}

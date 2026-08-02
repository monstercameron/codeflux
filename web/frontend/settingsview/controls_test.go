package settingsview_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/settingsview"
)

// longChoice is the shape that broke the row: a posture whose options are
// model identities rather than two short words.
func longChoice() settingsview.FlowSetting {
	return settingsview.FlowSetting{
		Key: "planning_rung", Label: "Model that decides how work is split",
		Help: "Does not climb: it is at the top from the first request.",
		Kind: settingsview.FlowChoice, Group: "Escalation",
		Choices: []string{
			"gpt-5.6-luna:low", "gpt-5.6-luna:medium", "gpt-5.6-luna:high",
			"gpt-5.6-luna:max", "gpt-5.6-sol:low", "gpt-5.6-sol:medium",
			"gpt-5.6-sol:high", "gpt-5.6-sol:max",
		},
		Text: "gpt-5.6-sol:high", AtDefault: true,
	}
}

func TestALongOptionSetBecomesAListRatherThanOverrunningTheRow(t *testing.T) {
	props := answeredSheet()
	props.Flow = append(props.Flow, longChoice())
	props.OnFlowChoice = func(string, string) {}
	markup := renderSettings(t, settingsview.Sheet(props))
	// Eight model identities laid side by side are wider than the sheet; laid
	// into the control column they paint over the setting's own name.
	if !strings.Contains(markup, `data-component="settings-list"`) {
		t.Fatalf("a long option set must become a list: %s", markup)
	}
	segments := strings.Count(markup, `aria-label="Set Model that decides how work is split to`)
	if segments != 0 {
		t.Fatalf("a long option set must not render %d segments", segments)
	}
	// The option in force is the one the list shows.
	if !strings.Contains(markup, `<option selected value="gpt-5.6-sol:high"`) &&
		!strings.Contains(markup, `selected`) {
		t.Fatalf("the list must mark the option in force: %s", markup)
	}
}

func TestAShortOptionSetStaysASegmentedControl(t *testing.T) {
	props := answeredSheet()
	props.OnFlowChoice = func(string, string) {}
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, `data-component="settings-segmented"`) {
		t.Fatalf("two short postures must stay side by side: %s", markup)
	}
}

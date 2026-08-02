package settingsview_test

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/web/frontend/settingsview"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderSettings(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// answeredSheet is a coordinator answer with something in every section.
func answeredSheet() settingsview.Props {
	return settingsview.Props{
		Policy: settingsview.PolicyRow{
			Preset: "balanced", ReasoningEffort: "maximum",
			RiskFloor: "routine", AssuranceFloor: "runtime-only",
			RequestTimeout: 5 * time.Minute, Known: true,
		},
		Providers: []settingsview.ProviderGroup{{
			ID: "prv_1", Name: "OpenAI", Available: true,
			Models: []settingsview.ModelRow{{ID: "mdl_1", Name: "gpt-5.6-sol", Available: true}},
		}},
		Flow:              describedFlow(),
		FlowRevisionKnown: true,
	}
}

func TestTheSheetOpensWithWhatARunWouldDoNow(t *testing.T) {
	markup := renderSettings(t, settingsview.Sheet(answeredSheet()))
	for _, want := range []string{
		"Operating contract",
		// The four facts that decide what happens if work starts now.
		"balanced · maximum effort", "1 of 1 bound", "6 of 12 max", "50% caught · 3× repeat",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("the contract lacks %q: %s", want, markup)
		}
	}
}

func TestTheContractSaysNotAnsweredRatherThanGuessing(t *testing.T) {
	markup := renderSettings(t, settingsview.Sheet(settingsview.Props{}))
	if !strings.Contains(markup, "not answered") {
		t.Fatalf("an unanswered contract must say so: %s", markup)
	}
	// A page that filled a gap with a plausible value would be the one line
	// here nobody could trust.
	if strings.Contains(markup, "0 of 0 max") {
		t.Fatalf("the contract invented a value: %s", markup)
	}
}

func TestEveryValueSitsOnTheSharedAxis(t *testing.T) {
	markup := renderSettings(t, settingsview.Sheet(answeredSheet()))
	// Rows are the sheet's only structure. Panels are what this replaced.
	rows := strings.Count(markup, `data-component="settings-row"`) +
		strings.Count(markup, `data-component="settings-flow-setting"`) +
		strings.Count(markup, `data-component="settings-provider"`)
	if rows < 15 {
		t.Fatalf("want a row per fact, got %d: %s", rows, markup)
	}
	// Nothing is boxed: the sheet has no panel surfaces to break the axis.
	if strings.Contains(markup, `data-component="panel"`) {
		t.Fatalf("the sheet grew a panel: %s", markup)
	}
	for _, want := range []string{
		`data-component="settings-section"`,
		`data-component="settings-flow-setting"`,
		`data-component="settings-provider"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup lacks %q", want)
		}
	}
}

func TestASettingMarksWhetherItIsStillTheShippedDefault(t *testing.T) {
	props := answeredSheet()
	props.Flow[1].AtDefault = false
	props.Flow[1].Number = 9
	props.Flow[0].AtDefault = true
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, ">1–12 · changed<") {
		t.Fatalf("a departed value must be marked beside its bound: %s", markup)
	}
	if !strings.Contains(markup, ">default<") {
		t.Fatalf("a value still at its default must say so: %s", markup)
	}
	// The section says how far this machine has drifted, without counting rows.
	if !strings.Contains(markup, "off default") {
		t.Fatalf("the section lacks its departure count: %s", markup)
	}
}

func TestAnUnsavedChangeShowsWhatItWasBesideWhatItWillBe(t *testing.T) {
	props := answeredSheet()
	props.FlowPending = map[string]settingsview.FlowSetting{
		"maximum_attempts": {
			Key: "maximum_attempts", Kind: settingsview.FlowNumber, Number: 9,
		},
	}
	props.OnFlowSave = func() {}
	props.OnFlowDiscard = func() {}
	markup := renderSettings(t, settingsview.Sheet(props))
	changed := markup[strings.Index(markup, `data-setting="maximum_attempts"`):]
	changed = changed[:strings.Index(changed, "</div></div></div>")]
	// The value it replaces is shown beside the value it will become, so a
	// change can be read before it is committed.
	if !strings.Contains(changed, ">6<") || !strings.Contains(changed, ">9<") {
		t.Fatalf("an unsaved change must show both values: %s", changed)
	}
	if !strings.Contains(markup, `data-changed="true"`) ||
		!strings.Contains(markup, ">unsaved<") {
		t.Fatalf("an unsaved value must be marked: %s", markup)
	}

	bar := renderSettings(t, settingsview.CommitBar(props))
	for _, want := range []string{
		"1 change not saved", "Attempts before stopping 6→9", "Save changes", "Discard",
	} {
		if !strings.Contains(bar, want) {
			t.Errorf("the commit bar lacks %q: %s", want, bar)
		}
	}
}

func TestTheCommitBarExistsOnlyWhileSomethingIsUnsaved(t *testing.T) {
	bar := renderSettings(t, settingsview.CommitBar(answeredSheet()))
	if strings.Contains(bar, "Save changes") {
		t.Fatalf("a settled sheet must not offer a save: %s", bar)
	}
}

func TestAProviderWithNoCataloguedModelExplainsItselfInsteadOfOfferingADeadControl(t *testing.T) {
	props := answeredSheet()
	props.Providers = []settingsview.ProviderGroup{{ID: "prv_1", Name: "Anthropic"}}
	props.OnConfigure = func(string, string) {}
	props.OnReferenceInput = func(string, string) {}
	props.OnCheckCredential = func(string) {}
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, "No model is catalogued for this provider") {
		t.Fatalf("markup lacks the reason a credential cannot be bound: %s", markup)
	}
	if strings.Contains(markup, "Bind for ") {
		t.Fatalf("a provider with no model must not offer a bind control: %s", markup)
	}
	if !strings.Contains(markup, "no credential") {
		t.Fatalf("an unusable provider must say so on the axis: %s", markup)
	}
}

func TestACredentialCheckIsShownAsTheCoordinatorWordedIt(t *testing.T) {
	summary := "The bound credential resolved from the operating-system credential store. " +
		"No provider request was made, so the provider's live response is unverified."
	props := answeredSheet()
	props.Checks = map[string]settingsview.CredentialCheck{
		"prv_1": {Resolved: true, Summary: summary},
	}
	props.OnCheckCredential = func(string) {}
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, "No provider request was made") {
		t.Fatalf("the check must not be presented as a live connection test: %s", markup)
	}
}

func TestRoutingIsReportedAndSaysItIsFixed(t *testing.T) {
	markup := renderSettings(t, settingsview.Sheet(answeredSheet()))
	for _, want := range []string{
		"Routing", "runtime-only", "5m0s", "nothing here changes them",
		"compiled defaults",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("routing markup lacks %q: %s", want, markup)
		}
	}
}

func TestTheSheetSaysWhichStateItIsIn(t *testing.T) {
	loading := renderSettings(t, settingsview.Sheet(settingsview.Props{Loading: true}))
	if !strings.Contains(loading, "Loading settings") {
		t.Errorf("loading markup lacks its state: %s", loading)
	}
	failed := renderSettings(t, settingsview.Sheet(settingsview.Props{
		Failed: true, OnReload: func() {},
	}))
	if !strings.Contains(failed, "Settings could not be read") {
		t.Errorf("failure markup lacks its state: %s", failed)
	}
	unavailable := renderSettings(t, settingsview.Sheet(settingsview.Props{
		Unavailable: true, UnavailableReason: "Choose a repository first.",
	}))
	if !strings.Contains(unavailable, "Choose a repository first.") {
		t.Errorf("unavailable markup lacks its reason: %s", unavailable)
	}
	empty := renderSettings(t, settingsview.Sheet(settingsview.Props{OnReload: func() {}}))
	for _, want := range []string{"No provider is recorded", "No model is catalogued"} {
		if !strings.Contains(empty, want) {
			t.Errorf("an empty sheet lacks %q: %s", want, empty)
		}
	}
}

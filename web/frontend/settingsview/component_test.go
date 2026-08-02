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

func TestThePolicyRegionReportsWhatGovernsARunAndOffersNoControl(t *testing.T) {
	markup := renderSettings(t, settingsview.Policy(settingsview.Props{
		Policy: settingsview.PolicyRow{
			Preset: "balanced", ReasoningEffort: "maximum",
			RiskFloor: "routine", AssuranceFloor: "runtime-only",
			RequestTimeout: 5 * time.Minute, Known: true,
		},
	}))
	for _, want := range []string{
		"settings-policy", "balanced", "maximum", "routine", "runtime-only",
		"5m0s", "compiled defaults",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("policy markup lacks %q: %s", want, markup)
		}
	}
	// Routing is one frozen versioned policy for this prototype. A control here
	// would tell somebody their runs had changed when nothing reads a second
	// value.
	if strings.Contains(markup, "<button") {
		t.Fatalf("the policy region must not offer controls: %s", markup)
	}
}

func TestAPolicyNobodyAnsweredIsNotDrawnAsEmptyValues(t *testing.T) {
	markup := renderSettings(t, settingsview.Policy(settingsview.Props{}))
	if !strings.Contains(markup, "No policy answer") {
		t.Fatalf("an unanswered policy must say so: %s", markup)
	}
}

func TestASettingsRegionSaysWhichStateItIsIn(t *testing.T) {
	loading := renderSettings(t, settingsview.Providers(settingsview.Props{Loading: true}))
	if !strings.Contains(loading, "Loading providers") {
		t.Errorf("loading markup lacks its state: %s", loading)
	}
	failed := renderSettings(t, settingsview.Providers(settingsview.Props{
		Failed: true, OnReload: func() {},
	}))
	for _, want := range []string{"Settings could not be read", "Nothing was changed", "Reload settings"} {
		if !strings.Contains(failed, want) {
			t.Errorf("failure markup lacks %q: %s", want, failed)
		}
	}
	unavailable := renderSettings(t, settingsview.Providers(settingsview.Props{
		Unavailable: true, UnavailableReason: "Choose a repository first.",
	}))
	if !strings.Contains(unavailable, "Choose a repository first.") {
		t.Errorf("unavailable markup lacks its reason: %s", unavailable)
	}
}

func TestAProviderWithNoCatalogedModelExplainsItselfInsteadOfOfferingADeadControl(t *testing.T) {
	markup := renderSettings(t, settingsview.Providers(settingsview.Props{
		Providers: []settingsview.ProviderGroup{{ID: "prv_1", Name: "OpenAI"}},
		OnConfigure: func(string, string) {
		},
		OnReferenceInput:  func(string, string) {},
		OnCheckCredential: func(string) {},
	}))
	if !strings.Contains(markup, "No model is catalogued for this provider yet") {
		t.Fatalf("markup lacks the reason a credential cannot be configured: %s", markup)
	}
	if strings.Contains(markup, "Use for ") {
		t.Fatalf("a provider with no model must not offer a configure control: %s", markup)
	}
	if !strings.Contains(markup, "Not configured") {
		t.Fatalf("an unconfigured provider must say so: %s", markup)
	}
}

func TestConfiguringIsBlockedUntilAReferenceIsEnteredAndSaysWhy(t *testing.T) {
	props := settingsview.Props{
		Providers: []settingsview.ProviderGroup{{
			ID: "prv_1", Name: "OpenAI",
			Models: []settingsview.ModelRow{{ID: "mdl_1", Name: "gpt-5.6-sol · 2026-05"}},
		}},
		OnConfigure:       func(string, string) {},
		OnReferenceInput:  func(string, string) {},
		OnCheckCredential: func(string) {},
	}
	blocked := renderSettings(t, settingsview.Providers(props))
	for _, want := range []string{
		"Use for gpt-5.6-sol · 2026-05",
		"Enter the os://service/account reference first.",
		"os://service/account",
	} {
		if !strings.Contains(blocked, want) {
			t.Errorf("blocked markup lacks %q: %s", want, blocked)
		}
	}

	props.Reference = map[string]string{"prv_1": "os://codeflux/openai"}
	ready := renderSettings(t, settingsview.Providers(props))
	if strings.Contains(ready, "Enter the os://service/account reference first.") {
		t.Fatalf("an entered reference must unblock the control: %s", ready)
	}
	// The page holds a reference to a credential, never a credential.
	if !strings.Contains(ready, "never displays or transmits the credential itself") {
		t.Fatalf("markup lacks the credential boundary statement: %s", ready)
	}
}

func TestACredentialCheckIsShownAsTheCoordinatorWordedIt(t *testing.T) {
	summary := "The bound credential resolved from the operating-system credential store. " +
		"No provider request was made, so the provider's live response is unverified."
	markup := renderSettings(t, settingsview.Providers(settingsview.Props{
		Providers: []settingsview.ProviderGroup{{ID: "prv_1", Name: "OpenAI", Available: true}},
		Checks: map[string]settingsview.CredentialCheck{
			"prv_1": {Resolved: true, Summary: summary},
		},
		OnCheckCredential: func(string) {},
	}))
	if !strings.Contains(markup, "No provider request was made") {
		t.Fatalf("the check must not be presented as a live connection test: %s", markup)
	}
	if !strings.Contains(markup, "Credential bound") {
		t.Fatalf("a configured provider must say so: %s", markup)
	}
}

func TestTheModelRegionNamesEveryCataloguedModelAndItsAvailability(t *testing.T) {
	markup := renderSettings(t, settingsview.Models(settingsview.Props{
		Providers: []settingsview.ProviderGroup{{
			ID: "prv_1", Name: "OpenAI", Available: true,
			Models: []settingsview.ModelRow{
				{ID: "mdl_1", Name: "gpt-5.6-sol · 2026-05", Available: true},
				{ID: "mdl_2", Name: "gpt-5.6-mini · 2026-05"},
			},
		}},
	}))
	for _, want := range []string{
		"settings-models", "OpenAI · gpt-5.6-sol · 2026-05",
		"OpenAI · gpt-5.6-mini · 2026-05", "Credential bound", "Not configured",
		"not evidence that the provider answered",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("model markup lacks %q: %s", want, markup)
		}
	}
}

func TestAnEmptyCatalogueIsAStateToActOnRatherThanAFailure(t *testing.T) {
	markup := renderSettings(t, settingsview.Models(settingsview.Props{OnReload: func() {}}))
	if !strings.Contains(markup, "No model is catalogued") {
		t.Fatalf("an empty catalogue must say so: %s", markup)
	}
	providers := renderSettings(t, settingsview.Providers(settingsview.Props{OnReload: func() {}}))
	if !strings.Contains(providers, "No provider is recorded") {
		t.Fatalf("an empty provider list must say so: %s", providers)
	}
}

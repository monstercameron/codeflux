package main

import (
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/settingsview"
	"codeflux.dev/codeflux/web/frontend/state"
	"google.golang.org/protobuf/types/known/durationpb"
)

func providerIdentityFixture(value string) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER,
		Value: value,
	}
}

func TestTheSettingsPolicyProjectionDistinguishesNoAnswerFromEmptyFields(t *testing.T) {
	unanswered := projectSettingsPolicy(nil)
	if unanswered.Known {
		t.Fatal("a missing response must not be reported as a known policy")
	}
	answered := projectSettingsPolicy(&codefluxv1.GetPolicyResponse{
		Policy: &codefluxv1.PolicyView{
			Preset: "balanced", ReasoningEffort: "maximum",
			Risk: "routine", RequiredAssurance: "runtime-only", Revision: 6,
		},
	})
	if !answered.Known || answered.Preset != "balanced" ||
		answered.ReasoningEffort != "maximum" || answered.RiskFloor != "routine" ||
		answered.AssuranceFloor != "runtime-only" || answered.Revision != 6 {
		t.Fatalf("policy projection lost a field: %+v", answered)
	}
}

func TestTheProviderProjectionGroupsModelsUnderTheProviderThatNamesThem(t *testing.T) {
	groups := projectSettingsProviders(&codefluxv1.GetModelsResponse{
		Models: []*codefluxv1.ModelView{
			{
				ProviderId:  providerIdentityFixture("prv_1"),
				DisplayName: &codefluxv1.RedactedText{Value: "OpenAI"},
				Available:   true,
			},
			{
				ProviderId:  providerIdentityFixture("prv_1"),
				ModelId:     "mdl_1",
				DisplayName: &codefluxv1.RedactedText{Value: "gpt-5.6-sol · 2026-05"},
				Available:   true,
			},
			{
				ProviderId:  providerIdentityFixture("prv_2"),
				DisplayName: &codefluxv1.RedactedText{Value: "Anthropic"},
			},
			// A row naming no provider cannot be configured or checked, so it
			// must not become a group offering controls that do nothing.
			{ModelId: "mdl_9", DisplayName: &codefluxv1.RedactedText{Value: "ghost"}},
		},
	})
	if len(groups) != 2 {
		t.Fatalf("want two providers, got %d: %+v", len(groups), groups)
	}
	if groups[0].ID != "prv_1" || groups[0].Name != "OpenAI" || !groups[0].Available {
		t.Fatalf("first provider lost a field: %+v", groups[0])
	}
	if len(groups[0].Models) != 1 || groups[0].Models[0].ID != "mdl_1" ||
		groups[0].Models[0].Name != "gpt-5.6-sol · 2026-05" || !groups[0].Models[0].Available {
		t.Fatalf("first provider's models lost a field: %+v", groups[0].Models)
	}
	// A provider with no catalogued model is still listed: it is the one that
	// cannot be used yet, and omitting it would leave nothing to configure.
	if groups[1].ID != "prv_2" || groups[1].Name != "Anthropic" || len(groups[1].Models) != 0 {
		t.Fatalf("second provider lost a field: %+v", groups[1])
	}
}

func TestAProviderWhoseNameNeverArrivedStaysIdentified(t *testing.T) {
	groups := projectSettingsProviders(&codefluxv1.GetModelsResponse{
		Models: []*codefluxv1.ModelView{{
			ProviderId:  providerIdentityFixture("prv_3"),
			ModelId:     "mdl_3",
			DisplayName: &codefluxv1.RedactedText{Value: "some-model"},
		}},
	})
	if len(groups) != 1 || groups[0].Name != "prv_3" {
		t.Fatalf("an unnamed provider must fall back to its identity: %+v", groups)
	}
}

func TestTheSettingsRouteStateFollowsTheAnswerItsRegionsDraw(t *testing.T) {
	// The route state was seeded loading and nothing moved it, so the regions
	// that read it drew a skeleton for the life of the page beside sections
	// that had already answered.
	if got := settingsViewForMountedSettings(settingsview.Props{Loading: true}); got.State != state.DataLoading {
		t.Fatalf("loading state = %q", got.State)
	}
	if got := settingsViewForMountedSettings(settingsview.Props{Failed: true}); got.State != state.DataRecoverableError {
		t.Fatalf("failed state = %q", got.State)
	}
	// A surface that cannot be asked has not failed and is not loading.
	if got := settingsViewForMountedSettings(settingsview.Props{Unavailable: true}); got.State != state.DataDenied {
		t.Fatalf("unavailable state = %q", got.State)
	}
	ready := settingsViewForMountedSettings(settingsview.Props{
		Policy: settingsview.PolicyRow{Known: true, Revision: 9},
		Providers: []settingsview.ProviderGroup{{
			ID: "prv_1", Models: []settingsview.ModelRow{{ID: "mdl_1"}, {ID: "mdl_2"}},
		}},
	})
	if ready.State != state.DataReady || ready.ProviderCount != 1 ||
		ready.ModelCount != 2 || ready.PolicyRevision != "9" {
		t.Fatalf("ready view lost a field: %+v", ready)
	}
	// No user layer has been written, so no revision is claimed.
	compiled := settingsViewForMountedSettings(settingsview.Props{
		Policy: settingsview.PolicyRow{Known: true},
	})
	if compiled.PolicyRevision != "" {
		t.Fatalf("policy revision = %q, want none for compiled defaults", compiled.PolicyRevision)
	}
}

func TestTheRequestTimeoutIsReadRatherThanInvented(t *testing.T) {
	if _, present := settingsRequestTimeout(&codefluxv1.GetModelsResponse{}); present {
		t.Fatal("an empty answer must not produce a timeout")
	}
	timeout, present := settingsRequestTimeout(&codefluxv1.GetModelsResponse{
		Models: []*codefluxv1.ModelView{
			{ProviderId: providerIdentityFixture("prv_1")},
			{
				ProviderId:     providerIdentityFixture("prv_1"),
				ModelId:        "mdl_1",
				DefaultTimeout: durationpb.New(90 * time.Second),
			},
		},
	})
	if !present || time.Duration(timeout) != 90*time.Second {
		t.Fatalf("timeout = %v, present = %v", time.Duration(timeout), present)
	}
}

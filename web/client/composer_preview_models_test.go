package main

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestPreviewModelOptionsPreserveDefaultAndAddGPT56Roles(t *testing.T) {
	t.Parallel()

	providerID, err := domain.ParseProviderID("prv_01890f3c-4a00-7abc-8def-0123456789ab")
	if err != nil {
		t.Fatalf("parse provider ID: %v", err)
	}
	options, err := previewModelOptions(providerID)
	if err != nil {
		t.Fatalf("build preview model options: %v", err)
	}

	want := []struct {
		model string
		label string
	}{
		{model: "gpt-5", label: "GPT-5 · preview"},
		{model: "gpt-5.6-sol", label: "GPT-5.6 Sol · frontier capability"},
		{model: "gpt-5.6-terra", label: "GPT-5.6 Terra · balanced intelligence and cost"},
		{model: "gpt-5.6-luna", label: "GPT-5.6 Luna · efficient high-volume"},
	}
	if len(options) != len(want) {
		t.Fatalf("option count = %d, want %d", len(options), len(want))
	}
	for index, expected := range want {
		option := options[index]
		if got := option.Value.Model(); got != expected.model {
			t.Errorf("option %d model = %q, want %q", index, got, expected.model)
		}
		if option.Label != expected.label {
			t.Errorf("option %d label = %q, want %q", index, option.Label, expected.label)
		}
		if got := option.Value.Revision(); got != previewModelRevision {
			t.Errorf("option %d revision = %q, want %q", index, got, previewModelRevision)
		}
		if got := option.Value.ProviderID(); got != providerID {
			t.Errorf("option %d provider ID = %q, want %q", index, got, providerID)
		}
	}
}

func TestPreviewModelOptionsRejectZeroProvider(t *testing.T) {
	t.Parallel()

	options, err := previewModelOptions(domain.ProviderID{})
	if err == nil {
		t.Fatal("previewModelOptions() error = nil, want invalid provider error")
	}
	if options != nil {
		t.Fatalf("previewModelOptions() options = %#v, want nil", options)
	}
}

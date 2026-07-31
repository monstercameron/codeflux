package main

import (
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
)

const previewModelRevision = "preview"

type previewModelSpec struct {
	id    string
	label string
}

var previewModelSpecs = [...]previewModelSpec{
	{id: "gpt-5", label: "GPT-5 · preview"},
	{id: "gpt-5.6-sol", label: "GPT-5.6 Sol · frontier capability"},
	{id: "gpt-5.6-terra", label: "GPT-5.6 Terra · balanced intelligence and cost"},
	{id: "gpt-5.6-luna", label: "GPT-5.6 Luna · efficient high-volume"},
}

// previewModelOptions keeps the local preview's existing default option while
// exposing each GPT-5.6 family member according to its intended workload role.
// Selection only changes the typed draft override; transport remains behind
// the composer's existing authoritative bridge.
func previewModelOptions(providerID domain.ProviderID) ([]composer.ModelOption, error) {
	options := make([]composer.ModelOption, 0, len(previewModelSpecs))
	for _, spec := range previewModelSpecs {
		value, err := composer.NewModelOverride(providerID, spec.id, previewModelRevision)
		if err != nil {
			return nil, err
		}
		options = append(options, composer.ModelOption{Value: value, Label: spec.label})
	}
	return options, nil
}

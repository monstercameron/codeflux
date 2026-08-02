package main

import (
	"strconv"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/settingsview"
	"codeflux.dev/codeflux/web/frontend/state"
)

// settingsViewForMountedSettings reports the settings route's data state from
// the answer its own regions are drawing.
//
// The route was seeded loading and nothing moved it, so the regions that read
// it — appearance, data, and local telemetry — drew a skeleton for the life of
// the page beside sections that had already answered. The state now follows
// the same coordinator exchange the rest of the surface follows.
func settingsViewForMountedSettings(props settingsview.Props) state.SettingsView {
	view := state.SettingsView{State: state.DataReady}
	switch {
	case props.Unavailable:
		// A surface that cannot be asked has not failed and is not loading. It
		// is reported as denied so the regions that read this state say the
		// route is not answerable rather than implying an error.
		view.State = state.DataDenied
	case props.Loading:
		view.State = state.DataLoading
	case props.Failed:
		view.State = state.DataRecoverableError
	}
	for _, provider := range props.Providers {
		view.ProviderCount++
		view.ModelCount += len(provider.Models)
	}
	if props.Policy.Known && props.Policy.Revision > 0 {
		view.PolicyRevision = strconv.FormatUint(props.Policy.Revision, 10)
	}
	return view
}

// settingsAnswer is one coordinator answer about the settings surfaces.
type settingsAnswer struct {
	Policy    settingsview.PolicyRow
	Providers []settingsview.ProviderGroup
}

// projectSettingsPolicy turns the policy answer into the row the page draws.
//
// A response the coordinator did not send leaves the row unknown rather than
// producing one full of empty strings, because a page showing blank fields
// under confident headings reads as "these are the values" instead of "no
// answer arrived".
func projectSettingsPolicy(response *codefluxv1.GetPolicyResponse) settingsview.PolicyRow {
	view := response.GetPolicy()
	if view == nil {
		return settingsview.PolicyRow{}
	}
	row := settingsview.PolicyRow{
		Preset:          view.GetPreset(),
		ReasoningEffort: view.GetReasoningEffort(),
		RiskFloor:       view.GetRisk(),
		AssuranceFloor:  view.GetRequiredAssurance(),
		Revision:        view.GetRevision(),
		Known:           true,
	}
	return row
}

// projectSettingsProviders groups the model list into providers.
//
// The coordinator names a provider with a view carrying no model identifier
// and each of its models with a view carrying one. Grouping here rather than
// in the page keeps the surface free of parsing, and keeps the one place that
// knows this shape next to the one test that pins it.
func projectSettingsProviders(
	response *codefluxv1.GetModelsResponse,
) []settingsview.ProviderGroup {
	views := response.GetModels()
	groups := make([]settingsview.ProviderGroup, 0, len(views))
	position := map[string]int{}
	for _, view := range views {
		providerID := view.GetProviderId().GetValue()
		if providerID == "" {
			// A row naming no provider cannot be configured or checked, and
			// drawing it would offer controls that do nothing.
			continue
		}
		index, present := position[providerID]
		if !present {
			groups = append(groups, settingsview.ProviderGroup{ID: providerID})
			index = len(groups) - 1
			position[providerID] = index
		}
		if view.GetModelId() == "" {
			groups[index].Name = view.GetDisplayName().GetValue()
			groups[index].Available = view.GetAvailable()
			continue
		}
		groups[index].Models = append(groups[index].Models, settingsview.ModelRow{
			ID:        view.GetModelId(),
			Name:      view.GetDisplayName().GetValue(),
			Available: view.GetAvailable(),
		})
	}
	for index := range groups {
		if groups[index].Name == "" {
			// A provider whose name never arrived is still identified, so it
			// remains configurable rather than being drawn as an empty heading.
			groups[index].Name = groups[index].ID
		}
	}
	return groups
}

// settingsRequestTimeout reports the effective per-request bound the
// coordinator carried on its model views.
//
// It is read from the first view that carries one. A page that invented a
// timeout would be stating a bound nothing enforces.
func settingsRequestTimeout(response *codefluxv1.GetModelsResponse) (int64, bool) {
	for _, view := range response.GetModels() {
		if view.GetDefaultTimeout() == nil {
			continue
		}
		if err := view.GetDefaultTimeout().CheckValid(); err != nil {
			continue
		}
		return int64(view.GetDefaultTimeout().AsDuration()), true
	}
	return 0, false
}

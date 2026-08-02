package main

import (
	"sort"
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
	Policy           settingsview.PolicyRow
	Providers        []settingsview.ProviderGroup
	Flow             []settingsview.FlowSetting
	FlowUnrenderable []string
	FlowRevision     uint64
	// Spend is what the recorded work cost. It is absent rather than zeroed
	// when the read failed, so the page says nothing instead of saying free.
	Spend settingsview.SpendPanel
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

// projectFlowSettings turns the coordinator's described settings into rows.
//
// The description is carried through unchanged. A page that restated a label,
// a bound, or a help sentence would eventually disagree with the engine that
// enforces it.
func projectFlowSettings(
	response *codefluxv1.GetFlowSettingsResponse,
) []settingsview.FlowSetting {
	views := response.GetSettings()
	settings := make([]settingsview.FlowSetting, 0, len(views))
	for _, view := range views {
		if view.GetKey() == "" || view.GetKind() == "" {
			// A setting with no key cannot be changed, and one with no kind
			// cannot be rendered. Drawing either would offer a control that
			// does nothing.
			continue
		}
		settings = append(settings, settingsview.FlowSetting{
			Key:       view.GetKey(),
			Label:     view.GetLabel(),
			Help:      view.GetHelp().GetValue(),
			Kind:      view.GetKind(),
			Choices:   view.GetChoices(),
			Minimum:   view.GetMinimum(),
			Maximum:   view.GetMaximum(),
			Group:     view.GetGroup(),
			Text:      view.GetTextValue(),
			Number:    view.GetNumberValue(),
			Enabled:   view.GetSwitchValue(),
			AtDefault: view.GetAtDefault(),
			Items:     view.GetItemValues(),
			Pairs:     projectFlowPairs(view.GetPairValues()),
		})
	}
	return settings
}

// flowSettingChanges renders the pending edits as the command the coordinator
// accepts.
//
// Only the field matching the setting kind is filled, because the coordinator
// reads only that one and sending the others would imply a change nobody made.
func flowSettingChanges(
	pending map[string]settingsview.FlowSetting,
) []*codefluxv1.FlowSettingChange {
	changes := make([]*codefluxv1.FlowSettingChange, 0, len(pending))
	for key, setting := range pending {
		change := &codefluxv1.FlowSettingChange{Key: key}
		switch setting.Kind {
		case settingsview.FlowChoice:
			change.TextValue = setting.Text
		case settingsview.FlowNumber:
			change.NumberValue = setting.Number
		case settingsview.FlowSwitch:
			change.SwitchValue = setting.Enabled
		}
		changes = append(changes, change)
	}
	sort.Slice(changes, func(first, second int) bool {
		// One order, so a retry of the same edits is the same request.
		return changes[first].GetKey() < changes[second].GetKey()
	})
	return changes
}

// projectFlowPairs turns a mapping setting's named choices into rows.
func projectFlowPairs(
	views []*codefluxv1.FlowSettingPair,
) []settingsview.FlowSettingPair {
	pairs := make([]settingsview.FlowSettingPair, 0, len(views))
	for _, view := range views {
		pairs = append(pairs, settingsview.FlowSettingPair{
			Key: view.GetKey(), Value: view.GetValue(),
		})
	}
	return pairs
}

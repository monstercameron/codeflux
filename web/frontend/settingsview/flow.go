package settingsview

// The sheet renders these settings; these are the two facts about the
// collection it needs that are not about one row.

// flowGroups lists the groups in the order the coordinator sent them.
func flowGroups(settings []FlowSetting) []string {
	seen := map[string]bool{}
	groups := make([]string, 0, len(settings))
	for _, setting := range settings {
		if setting.Group == "" || seen[setting.Group] {
			continue
		}
		seen[setting.Group] = true
		groups = append(groups, setting.Group)
	}
	return groups
}

// effectiveFlowSetting is what a control should show: the pending value when
// one has been made, and the stored value otherwise.
func effectiveFlowSetting(props Props, setting FlowSetting) FlowSetting {
	pending, changed := props.FlowPending[setting.Key]
	if !changed {
		return setting
	}
	setting.Text, setting.Number, setting.Enabled = pending.Text, pending.Number, pending.Enabled
	return setting
}

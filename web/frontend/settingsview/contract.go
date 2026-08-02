package settingsview

import (
	"strconv"
	"strings"
)

// The operating contract is the four facts that decide what happens if
// somebody starts a run right now.
//
// Each is assembled from what the coordinator already answered rather than
// asked for separately, and each says "not answered" instead of guessing when
// the answer has not arrived. A summary that filled a gap with a plausible
// value would be the one line on this page nobody could trust.

// contractModel names the model and effort a run will think with.
func (row PolicyRow) contractModel() string {
	if !row.Known {
		return ""
	}
	parts := []string{}
	if row.Preset != "" {
		parts = append(parts, row.Preset)
	}
	if row.ReasoningEffort != "" {
		parts = append(parts, row.ReasoningEffort+" effort")
	}
	return strings.Join(parts, " · ")
}

// sourceSentence says where the policy in force came from.
func (row PolicyRow) sourceSentence() string {
	if row.Revision > 0 {
		return "In force from settings revision " +
			strconv.FormatUint(row.Revision, 10) + "."
	}
	return "In force from the compiled defaults; no settings layer has been written."
}

// contractCredential reports whether anything can be called at all.
//
// This is the one line on the page that says whether work can start, so it
// counts what is usable rather than what is recorded.
func (props Props) contractCredential() string {
	if len(props.Providers) == 0 {
		return "none recorded"
	}
	usable := 0
	for _, provider := range props.Providers {
		if provider.Available {
			usable++
		}
	}
	if usable == 0 {
		return "none bound"
	}
	return strconv.Itoa(usable) + " of " + strconv.Itoa(len(props.Providers)) + " bound"
}

// contractAttempts reports how far a run may go before it stops and says so.
func (props Props) contractAttempts() string {
	setting, found := props.settingInForce("maximum_attempts")
	if !found {
		return ""
	}
	return flowValueText(setting) + " of " +
		strconv.FormatInt(int64(setting.Maximum), 10) + " max"
}

// contractVerification reports the floor a produced program must clear.
func (props Props) contractVerification() string {
	mutation, hasMutation := props.settingInForce("mutation_threshold_percent")
	repetition, hasRepetition := props.settingInForce("repetition_runs")
	if !hasMutation && !hasRepetition {
		return ""
	}
	parts := []string{}
	if hasMutation {
		parts = append(parts, flowValueText(mutation)+"% caught")
	}
	if hasRepetition {
		parts = append(parts, flowValueText(repetition)+"× repeat")
	}
	return strings.Join(parts, " · ")
}

// settingInForce is one setting as it will be after any unsaved change.
//
// The contract reads pending values, because it answers what a run would do
// and somebody looking at it has already decided to make the change.
func (props Props) settingInForce(key string) (FlowSetting, bool) {
	for _, setting := range props.Flow {
		if setting.Key != key {
			continue
		}
		return effectiveFlowSetting(props, setting), true
	}
	return FlowSetting{}, false
}

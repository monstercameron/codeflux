package main

import (
	"strconv"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/settingsview"
)

// phaseLabels names each flow phase in the words a person reading a bill
// would use, rather than in the identifiers the coordinator sends.
//
// An unrecognised phase keeps its own name rather than being dropped, because
// a phase this build has not heard of still spent money and hiding it would
// make the parts stop summing to the total for no visible reason.
var phaseLabels = map[string]string{
	"specification": "Specification",
	"atoms":         "Atoms",
	"molecules":     "Molecules",
	"control-flow":  "Control flow",
	"program":       "Program",
	"verification":  "Verification",
	"delivery":      "Delivery",
}

// projectSpendSummary turns the coordinator's answer into the panel the
// settings page draws.
//
// A nil response produces an unknown panel rather than an empty one. A page
// showing a confident zero for a request that never arrived would report the
// run as free, which is the exact misreading this surface exists to prevent.
func projectSpendSummary(
	response *codefluxv1.GetSpendSummaryResponse,
	windowLabel string,
) settingsview.SpendPanel {
	if response == nil {
		return settingsview.SpendPanel{}
	}
	panel := settingsview.SpendPanel{
		Known:                       true,
		WindowLabel:                 windowLabel,
		Total:                       spendLine("Total", response.GetTotal()),
		StageAttributionApproximate: response.GetStageAttributionApproximate(),
		UnpricedCalls:               response.GetTotal().GetCostUnknownCalls(),
	}
	for _, phase := range response.GetByPhase() {
		label, named := phaseLabels[phase.GetPhase()]
		if !named {
			label = phase.GetPhase()
		}
		panel.Phases = append(panel.Phases, spendLine(label, phase.GetSpend()))
	}
	for _, model := range response.GetByModel() {
		panel.Models = append(
			panel.Models, spendLine(modelLabel(model), model.GetSpend()),
		)
	}
	unattributed := response.GetUnattributed()
	if unattributed.GetCalls() > 0 {
		panel.Unattributed = spendLine("No stage claimed", unattributed)
	}
	return panel
}

// modelLabel names a model, including its version only when there is one to
// distinguish it from.
func modelLabel(model *codefluxv1.ModelSpendView) string {
	name := model.GetModel()
	if name == "" {
		name = "unnamed model"
	}
	if version := model.GetModelVersion(); version != "" && version != name {
		return name + " " + version
	}
	return name
}

// spendLine renders one slice for display.
func spendLine(
	label string,
	slice *codefluxv1.SpendSliceView,
) settingsview.SpendLine {
	line := settingsview.SpendLine{
		Label:  label,
		Detail: spendDetail(slice),
	}
	cost := slice.GetKnownCost()
	amount := cost.GetAmount()
	if amount == nil {
		return line
	}
	line.CostKnown = true
	line.Approximate = !cost.GetExact()
	line.Cost = formatSpendMoney(amount)
	return line
}

// spendDetail says how much work the money bought.
func spendDetail(slice *codefluxv1.SpendSliceView) string {
	if slice == nil || slice.GetCalls() == 0 {
		return ""
	}
	parts := []string{
		countLabel(slice.GetCalls(), "call", "calls"),
		settingsview.FormatTokenCount(slice.GetInputTokens()) + " in",
		settingsview.FormatTokenCount(slice.GetOutputTokens()) + " out",
	}
	if cached := slice.GetCachedInputTokens(); cached > 0 {
		parts = append(parts, settingsview.FormatTokenCount(cached)+" cached")
	}
	if reasoning := slice.GetReasoningTokens(); reasoning > 0 {
		parts = append(parts,
			settingsview.FormatTokenCount(reasoning)+" reasoning")
	}
	return strings.Join(parts, " · ")
}

// formatSpendMoney renders an exact Money at its own declared scale.
//
// It is separate from formatMoney, which renders a domain.Money at a fixed two
// places for budget figures. This one must honour the message's own
// decimal_places or every sub-cent call renders as zero.
//
// decimal_places is read from the message rather than assumed, because this
// surface deliberately sends a finer scale than whole minor units: a call
// costing a fraction of a cent is the normal case, and rendering it at two
// places would show every one of them as zero. Trailing zeros are trimmed so a
// round amount does not read as false precision.
func formatSpendMoney(amount *codefluxv1.Money) string {
	places := int(amount.GetDecimalPlaces())
	units := amount.GetMinorUnits()
	sign := ""
	if units < 0 {
		sign = "-"
		units = -units
	}
	digits := strconv.FormatInt(units, 10)
	if places == 0 {
		return amount.GetCurrencyCode() + " " + sign + digits
	}
	for len(digits) <= places {
		digits = "0" + digits
	}
	whole := digits[:len(digits)-places]
	fraction := strings.TrimRight(digits[len(digits)-places:], "0")
	// Two places is what a currency's own minor unit reads as, so an amount
	// that lands on it keeps the shape a person expects to see.
	for len(fraction) < 2 {
		fraction += "0"
	}
	return amount.GetCurrencyCode() + " " + sign + whole + "." + fraction
}

func countLabel(count uint64, singular, plural string) string {
	if count == 1 {
		return "1 " + singular
	}
	return strconv.FormatUint(count, 10) + " " + plural
}

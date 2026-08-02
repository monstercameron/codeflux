package settingsview

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SpendLine is one row of the spend readout: a phase, a stage, or a model.
//
// Cost arrives already rendered, because formatting money needs the currency
// and the scale the coordinator sent, and a view that reformats a number it
// was handed is a view that can disagree with the ledger.
type SpendLine struct {
	Label string
	// Detail is the token counts, in words the row can show beside the money.
	Detail string
	// Cost is the rendered amount. CostKnown is false when nothing in this row
	// could be priced, and the row must then say so rather than show a zero.
	Cost      string
	CostKnown bool
	// Approximate marks a cost that was rounded to the scale it is shown at,
	// so a figure is never presented as exact when it is not.
	Approximate bool
}

// SpendPanel is what the recorded work cost and where the money went.
//
// Known is false before an answer arrives. The panel deliberately carries the
// unattributed remainder and the unpriced call count as their own fields: a
// breakdown whose parts do not sum to its total is only honest if it says why.
type SpendPanel struct {
	Known bool
	// WindowLabel names the period these figures cover, because a spend figure
	// with no period is read as "now" whatever it actually covers.
	WindowLabel string
	Total       SpendLine
	// Phases is the flow's own movements — the atoms, the molecules, the
	// program — in flow order.
	Phases []SpendLine
	// Models is exact rather than approximated, ordered most expensive first.
	Models []SpendLine
	// Unattributed is the spend no stage claimed. Shown whenever it is not
	// empty, so the gap between the phases and the total is explained.
	Unattributed SpendLine
	// StageAttributionApproximate is true while phase costs are inferred from
	// call and stage timings rather than recorded on the call.
	StageAttributionApproximate bool
	// UnpricedCalls is how many calls in the window carried no price at all.
	UnpricedCalls uint64
}

// spendSection renders what the recorded work cost, broken down by the flow's
// own movements.
//
// It sits after the operating contract because the contract says what a run
// will do and this says what doing it cost, which is the order the two
// questions are actually asked in.
func spendSection(props Props) ui.Node {
	panel := props.Spend
	if !panel.Known {
		return section(props, "Spend", "", []ui.Node{
			emptyLine(props,
				"No spend has been recorded yet. A figure appears here once the "+
					"coordinator has completed a model request."),
		})
	}
	rows := []ui.Node{spendRow(props, panel.Total)}
	if panel.WindowLabel != "" {
		rows = append(rows, noteLine(props, "Covering "+panel.WindowLabel+"."))
	}
	if len(panel.Phases) == 0 {
		rows = append(rows, emptyLine(props,
			"No stage claimed any of this window's calls."))
	}
	for _, phase := range panel.Phases {
		rows = append(rows, spendRow(props, phase))
	}
	if panel.Unattributed.CostKnown || panel.Unattributed.Detail != "" {
		rows = append(rows, spendRow(props, panel.Unattributed))
	}
	if panel.StageAttributionApproximate && len(panel.Phases) > 0 {
		rows = append(rows, noteLine(props,
			"Phase costs are an estimate. A provider call does not record the "+
				"stage that made it, so each call is attributed to the stage "+
				"that was running when it was made. The total and the per-model "+
				"figures are exact."))
	}
	if panel.UnpricedCalls > 0 {
		rows = append(rows, noteLine(props,
			countOf(int(panel.UnpricedCalls), "call", "calls")+
				" in this window carried no price and are excluded from every "+
				"amount above, so the totals are a floor rather than a total."))
	}
	return section(props, "Spend", spendNote(panel), rows)
}

// spendModelSection reports what each model cost.
//
// It is its own band rather than a block inside the spend section because the
// question it answers is a different one: not where the work went, but which
// model the money went to.
func spendModelSection(props Props) ui.Node {
	panel := props.Spend
	if !panel.Known {
		return section(props, "Spend by model", "", []ui.Node{
			emptyLine(props, "No model spend has been recorded yet."),
		})
	}
	if len(panel.Models) == 0 {
		return section(props, "Spend by model", "", []ui.Node{
			emptyLine(props, "No model is recorded against this window."),
		})
	}
	rows := make([]ui.Node, 0, len(panel.Models)+1)
	for _, model := range panel.Models {
		rows = append(rows, spendRow(props, model))
	}
	rows = append(rows, noteLine(props,
		"A model is recorded on the request itself, so these figures are exact "+
			"rather than attributed."))
	return section(props, "Spend by model",
		countOf(len(panel.Models), "model", "models"), rows)
}

// spendRow is one line of the readout.
//
// An unknown cost renders as "not priced" and never as a zero amount: a zero
// says the work was free, which is the one thing an unpriced call is not known
// to be.
func spendRow(props Props, line SpendLine) ui.Node {
	value := line.Cost
	switch {
	case !line.CostKnown || value == "":
		// "not priced" rather than contractRow's "not answered": the
		// coordinator did answer, and what it said is that these calls carried
		// no price. The two are different facts and a person acts on them
		// differently.
		value = "not priced"
	case line.Approximate:
		value = "≈ " + value
	}
	name := line.Label
	if line.Detail != "" {
		name = line.Label + "  " + line.Detail
	}
	return row(props, rowProps{
		Name: name,
		Value: html.Span(html.Props{
			Text:  value,
			Class: valueTextClass(props, line.CostKnown),
		}),
	})
}

// spendNote summarises the window in the section's eyebrow.
func spendNote(panel SpendPanel) string {
	if !panel.Total.CostKnown && panel.Total.Detail == "" {
		return ""
	}
	parts := make([]string, 0, 2)
	if panel.Total.Detail != "" {
		parts = append(parts, panel.Total.Detail)
	}
	if len(panel.Phases) > 0 {
		parts = append(parts, countOf(len(panel.Phases), "phase", "phases"))
	}
	return strings.Join(parts, ", ")
}

// FormatTokenCount renders a token count compactly enough to sit beside money.
//
// Exported because the projection that fills a SpendLine needs the same
// rendering the view would have used, and two implementations of "how many
// tokens is that" drift.
func FormatTokenCount(tokens uint64) string {
	switch {
	case tokens < 1000:
		return strconv.FormatUint(tokens, 10)
	case tokens < 1_000_000:
		return strconv.FormatFloat(float64(tokens)/1000, 'f', 1, 64) + "k"
	default:
		return strconv.FormatFloat(float64(tokens)/1_000_000, 'f', 2, 64) + "M"
	}
}

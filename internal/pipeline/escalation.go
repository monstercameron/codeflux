package pipeline

import (
	"fmt"
	"sort"
	"strings"
)

// Models a run can be given, cheapest first.
//
// They are named here rather than left as free text because a misspelled model
// is not a slower run, it is a run that fails at the first request with a
// provider error nobody reading the ledger can attribute. A closed list makes
// that a settings error caught before anything is spent.
const (
	// ModelLuna is the cheapest, and the default first rung. Most requests do
	// not need more, and finding out which ones do is what the ladder is for.
	ModelLuna = "gpt-5.6-luna"
	// ModelSol is the capable rung, reached when the cheap one has stopped
	// making progress rather than merely failed once.
	ModelSol = "gpt-5.6-sol"
)

// KnownModels is every model a ladder may name, cheapest first.
func KnownModels() []string {
	return []string{ModelLuna, ModelSol}
}

// ModelBearing reports whether a stage can choose the rung for an attempt.
//
// This list used to name sixteen stages on the reasoning that they were the
// ones "that make a model request". None of them do. There is exactly one
// model entry point in the run — the implementation loop — and the rest of the
// flow is static analysis, compilation, and running things. An override on any
// of those sixteen would have been a control that silently did nothing, which
// is the failure this predicate exists to prevent.
//
// What is true is narrower and still useful: four gates can send work back,
// and the rung the next attempt runs on is a real choice at each of them. An
// override here means "when this gate rejects the work, retry it on this
// rung", which is what somebody who knows their acceptance checks are the hard
// part actually wants.
func ModelBearing(stage string) bool {
	switch stage {
	case StageNameAssembly, StageNameAtomVerification,
		StageNameEndToEndTests, StageNameAdversarial:
		return true
	}
	return false
}

// Stage names that can send work back, as the gates that drive refinement.
const (
	StageNameAssembly         = "assembly"
	StageNameAtomVerification = "atom-verification"
	StageNameEndToEndTests    = "end-to-end-tests"
	StageNameAdversarial      = "adversarial"
)

// ModelBearingStages is every stage a per-stage override may name.
func ModelBearingStages() []string {
	var names []string
	for _, stage := range Flow {
		if ModelBearing(stage.Name) {
			names = append(names, stage.Name)
		}
	}
	return names
}

// validateEscalation checks the ladder, the thresholds and the overrides.
//
// It lives beside the vocabulary it validates against so a rung added to the
// list is offered and accepted in the same edit.
func validateEscalation(settings Settings) error {
	if len(settings.ModelLadder) == 0 {
		return fmt.Errorf("the ladder is empty, so a run would have no model " +
			"to start on")
	}
	seen := map[string]bool{}
	previous := -1
	for _, named := range settings.ModelLadder {
		rung, err := ParseRung(named)
		if err != nil {
			return fmt.Errorf("the ladder is wrong: %w", err)
		}
		if seen[named] {
			return fmt.Errorf("the ladder names %q twice, so escalating to it "+
				"would change nothing", named)
		}
		seen[named] = true
		// Ascending, because a ladder that goes down is not a ladder. A run
		// escalates because it stalled; moving it somewhere cheaper at that
		// moment guarantees it stalls again, and the configuration would read
		// as an escalation policy while being the opposite.
		position := rungCost(rung)
		if position <= previous {
			return fmt.Errorf(
				"the ladder puts %q after a more capable rung, so climbing it "+
					"would move a stuck run to something cheaper", named)
		}
		previous = position
	}
	for _, named := range settings.ApprovalRungs {
		if _, err := ParseRung(named); err != nil {
			return fmt.Errorf("a rung needing approval is wrong: %w", err)
		}
	}
	if settings.PlanningRung != "" {
		if _, err := ParseRung(settings.PlanningRung); err != nil {
			return fmt.Errorf("the planning rung is wrong: %w", err)
		}
	}
	for stage, named := range settings.StageModels {
		if !ModelBearing(stage) {
			return fmt.Errorf("%q is given the rung %q, but that stage makes "+
				"no model request, so the override would do nothing; the "+
				"stages that take one are %s",
				stage, named, strings.Join(ModelBearingStages(), ", "))
		}
		if _, err := ParseRung(named); err != nil {
			return fmt.Errorf("the override for %q is wrong: %w", stage, err)
		}
	}
	return nil
}

// rungCost orders rungs by what they cost, model first and effort second.
//
// Model dominates because changing it raises the rate on every token, while
// raising effort bills more tokens at the rate already in force. Two rungs on
// the same model are ordered by effort; any rung on a costlier model outranks
// every rung on a cheaper one.
func rungCost(rung Rung) int {
	model, effort := -1, -1
	for index, named := range KnownModels() {
		if named == rung.Model {
			model = index
		}
	}
	for index, named := range KnownEfforts() {
		if named == rung.Effort {
			effort = index
		}
	}
	return model*len(KnownEfforts()) + effort
}

// SortedStageModels is the per-stage overrides in a stable order.
//
// Ranging a map to build a description or a record would produce a different
// order every time, which turns an unchanged configuration into a changed one
// everywhere it is compared.
func (settings Settings) SortedStageModels() [][2]string {
	pairs := make([][2]string, 0, len(settings.StageModels))
	for stage, named := range settings.StageModels {
		pairs = append(pairs, [2]string{stage, named})
	}
	sort.Slice(pairs, func(first, second int) bool {
		return pairs[first][0] < pairs[second][0]
	})
	return pairs
}

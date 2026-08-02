package pipeline

import (
	"strings"
	"testing"
)

// TestTheDefaultLadderClimbsEffortBeforeModel is the cost argument, pinned.
//
// Raising effort bills more tokens at the rate already in force; changing
// model raises the rate on every token. A ladder that reached for the
// expensive model before exhausting the cheap one would pay the higher rate to
// learn something the cheaper rung could have taught, on every request that
// needed more than the first rung.
func TestTheDefaultLadderClimbsEffortBeforeModel(t *testing.T) {
	ladder := DefaultLadder()
	if len(ladder) < 2 {
		t.Fatalf("the default ladder has %d rung(s)", len(ladder))
	}
	first, err := ParseRung(ladder[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Model != ModelLuna || first.Effort != EffortLow {
		t.Errorf("runs start on %s, not the cheapest rung", ladder[0])
	}
	// Every rung on the cheap model comes before every rung on the dear one.
	sawExpensive := false
	for _, named := range ladder {
		rung, parseErr := ParseRung(named)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if rung.Model == ModelSol {
			sawExpensive = true
			continue
		}
		if sawExpensive {
			t.Errorf("%s comes after a rung on the expensive model, so the "+
				"ladder pays the higher rate before exhausting the cheap one",
				named)
		}
	}
}

// TestTheDefaultLadderIsStrictlyAscending is what makes it a ladder.
func TestTheDefaultLadderIsStrictlyAscending(t *testing.T) {
	if err := DefaultSettings().Validate(); err != nil {
		t.Fatalf("the shipped ladder does not satisfy its own rule: %v", err)
	}
}

// TestADescendingLadderIsRefused keeps a configuration from reading as an
// escalation policy while being its opposite.
//
// A run escalates because it stalled. Moving it somewhere cheaper at that
// moment guarantees it stalls again, and nothing downstream would report that
// the ladder was upside down — the run would simply never converge.
func TestADescendingLadderIsRefused(t *testing.T) {
	settings := DefaultSettings()
	settings.ModelLadder = []string{
		Rung{Model: ModelSol, Effort: EffortHigh}.String(),
		Rung{Model: ModelLuna, Effort: EffortLow}.String(),
	}
	err := settings.Validate()
	if err == nil {
		t.Fatal("a ladder that descends was accepted")
	}
	if !strings.Contains(err.Error(), "cheaper") {
		t.Errorf("the refusal does not say what is wrong with it: %v", err)
	}
}

// TestARungTheEngineCannotBuildIsRefused stops a typo becoming a run that
// fails at its first request with a provider error nobody can attribute.
func TestARungTheEngineCannotBuildIsRefused(t *testing.T) {
	for _, wrong := range []string{
		"gpt-5.6-luna",           // no effort
		"gpt-5.6-luna:excessive", // no such effort
		"gpt-5.7-nova:low",       // no such model
	} {
		settings := DefaultSettings()
		settings.ModelLadder = []string{wrong}
		if err := settings.Validate(); err == nil {
			t.Errorf("%q was accepted as a rung", wrong)
		}
	}
}

// TestAnOverrideOnAStageThatChoosesNothingIsRefused is the guard against a
// control that silently does nothing.
//
// The stage list here was once sixteen names, on the reasoning that they were
// the stages that "make a model request". None of them do — there is one model
// entry point in the run. An override on any of them would have rendered,
// accepted a value, stored it, and changed nothing about any run.
func TestAnOverrideOnAStageThatChoosesNothingIsRefused(t *testing.T) {
	settings := DefaultSettings()
	settings.StageModels = map[string]string{
		"atom-fuzz": Rung{Model: ModelSol, Effort: EffortHigh}.String(),
	}
	err := settings.Validate()
	if err == nil {
		t.Fatal("a stage that chooses no rung accepted an override")
	}
	if !strings.Contains(err.Error(), "would do nothing") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	// And it names the stages that do take one, so the message is actionable.
	for _, stage := range ModelBearingStages() {
		if !strings.Contains(err.Error(), stage) {
			t.Errorf("the refusal does not name %q as a stage that takes an "+
				"override: %v", stage, err)
		}
	}
}

// TestEveryStageThatTakesAnOverrideIsARealStage keeps the two vocabularies in
// step: a gate named here and absent from the flow governs nothing.
func TestEveryStageThatTakesAnOverrideIsARealStage(t *testing.T) {
	inFlow := map[string]bool{}
	for _, stage := range Flow {
		inFlow[stage.Name] = true
	}
	for _, stage := range ModelBearingStages() {
		if !inFlow[stage] {
			t.Errorf("%q takes a rung override and is not a stage of the flow",
				stage)
		}
	}
	if len(ModelBearingStages()) == 0 {
		t.Error("no stage takes an override, so the setting governs nothing")
	}
}

// TestAPinOnlyEverMovesAStageUp keeps an override from undoing an escalation.
//
// A run reaches a higher rung by stalling on it. A pin that dropped it back
// down at the very gate that stalled is the one change guaranteed to make it
// stall again.
func TestAPinOnlyEverMovesAStageUp(t *testing.T) {
	settings := DefaultSettings()
	cheap := Rung{Model: ModelLuna, Effort: EffortLow}.String()
	dear := Rung{Model: ModelSol, Effort: EffortHigh}.String()
	settings.StageModels = map[string]string{StageNameEndToEndTests: dear}

	if got := settings.RungForStage(StageNameEndToEndTests, cheap); got != dear {
		t.Errorf("a pin above the current rung was ignored: %s", got)
	}
	settings.StageModels = map[string]string{StageNameEndToEndTests: cheap}
	if got := settings.RungForStage(StageNameEndToEndTests, dear); got != dear {
		t.Errorf("a pin below the current rung dragged an escalated run back "+
			"down to %s", got)
	}
	// A stage with no pin is left where the ladder put it.
	if got := settings.RungForStage(StageNameAssembly, dear); got != dear {
		t.Errorf("an unpinned stage was moved to %s", got)
	}
}

// TestTheApprovalRungIsNotOnTheDefaultLadder is the rarity rule.
//
// A rung that needs approval and is also on the default ladder would stop
// every stuck run to ask a question, which is not a gate on something rare —
// it is a gate on the ordinary case, and it would be clicked through.
func TestTheApprovalRungIsNotOnTheDefaultLadder(t *testing.T) {
	settings := DefaultSettings()
	for _, needing := range settings.ApprovalRungs {
		if contains(settings.ModelLadder, needing) {
			t.Errorf("%s needs approval and is on the default ladder, so every "+
				"stuck run stops to ask", needing)
		}
		if !settings.NeedsApproval(needing) {
			t.Errorf("%s is listed as needing approval and does not read as "+
				"needing it", needing)
		}
	}
	for _, ordinary := range settings.ModelLadder {
		if settings.NeedsApproval(ordinary) {
			t.Errorf("%s is on the ladder and needs approval", ordinary)
		}
	}
}

// TestPlanningRunsAtTheTopAndDoesNotClimb pins the one place the flow spends
// the most up front on purpose.
func TestPlanningRunsAtTheTopAndDoesNotClimb(t *testing.T) {
	settings := DefaultSettings()
	planning, err := ParseRung(settings.PlanningRung)
	if err != nil {
		t.Fatalf("the shipped planning rung is unreadable: %v", err)
	}
	first, err := ParseRung(settings.FirstRung())
	if err != nil {
		t.Fatal(err)
	}
	if rungCost(planning) <= rungCost(first) {
		t.Errorf("planning runs on %s, which is no better than what a run "+
			"starts on — a decomposition that misses a behaviour is paid for "+
			"on every rung and cannot be recovered further down",
			settings.PlanningRung)
	}
}

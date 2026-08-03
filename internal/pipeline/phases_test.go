package pipeline

import "testing"

// TestEveryStageOfTheFlowBelongsToExactlyOnePhase is the load-bearing check for
// phase attribution.
//
// A cost summary grouped by phase is only as trustworthy as this mapping: a
// stage with no phase drops its spend out of the totals, and a stage in two
// phases counts its spend twice. Driving the assertion from Flow rather than
// from a fixed list means a stage added later fails here instead of quietly
// disappearing from the summary.
func TestEveryStageOfTheFlowBelongsToExactlyOnePhase(t *testing.T) {
	seen := make(map[Number]Phase, len(Flow))
	for _, stage := range Flow {
		phase, found := PhaseOf(stage.Number)
		if !found {
			t.Errorf("stage %d (%s) belongs to no phase", stage.Number, stage.Name)
			continue
		}
		if !phase.Valid() {
			t.Errorf("stage %d (%s) reports unknown phase %q",
				stage.Number, stage.Name, phase)
		}
		seen[stage.Number] = phase
	}
	if len(seen) != len(Flow) {
		t.Fatalf("phased %d stages of %d", len(seen), len(Flow))
	}

	// Every named phase must own at least one stage, or the constant describes
	// a movement of the flow that no longer exists.
	owned := make(map[Phase]int, len(Phases))
	for _, phase := range seen {
		owned[phase]++
	}
	for _, phase := range Phases {
		if owned[phase] == 0 {
			t.Errorf("phase %q owns no stage", phase)
		}
	}
}

// TestPhasesRunInFlowOrderWithoutInterleaving proves the phases partition the
// flow into contiguous runs.
//
// Phase boundaries are held as "last stage number" bounds, which is only
// correct while each phase is one unbroken run of stages. If a later change
// interleaved them, grouping spend by phase would summarise a sequence nobody
// executed.
func TestPhasesRunInFlowOrderWithoutInterleaving(t *testing.T) {
	position := make(map[Phase]int, len(Phases))
	for index, phase := range Phases {
		position[phase] = index
	}
	previous := -1
	for stage := StageInstructions; stage <= StageSkipAudit; stage++ {
		phase, found := PhaseOf(stage)
		if !found {
			t.Fatalf("stage %d has no phase", stage)
		}
		current := position[phase]
		if current < previous {
			t.Fatalf("stage %d is in phase %q, which precedes the phase of stage %d",
				stage, phase, stage-1)
		}
		previous = current
	}
}

// TestStagesOutsideTheFlowAreUnattributedRatherThanDefaulted proves an
// out-of-range stage is refused instead of being filed under a phase.
func TestStagesOutsideTheFlowAreUnattributedRatherThanDefaulted(t *testing.T) {
	for _, stage := range []Number{-1, 0, StageSkipAudit + 1, 1000} {
		if phase, found := PhaseOf(stage); found {
			t.Errorf("stage %d was attributed to phase %q", stage, phase)
		}
	}
	if Phase("atom").Valid() {
		t.Error("a phase name that is not in the flow validated")
	}
}

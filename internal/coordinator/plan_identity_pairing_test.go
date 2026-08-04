package coordinator

import (
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/storage"
)

// TestStepIdentitiesArePairedWithWhatTheStoreHolds is the assumption behind an
// error that names neither the step nor the cause.
//
// The run's step identities and the store's are different vocabularies, paired
// by position. The pairing used to read the plan that was built to *send* to
// the store rather than the one the store *returned*. Those are the same object
// on an ordinary write and need not be on an idempotent one, where the store
// answers with the revision it already had — and the identities the run then
// adopts are ones the store never issued.
//
// Every later read of such a step is refused with "step does not belong to run
// plan". That is a true sentence about a situation nothing else reports, and it
// cost two whole ladder passes on 2026-08-04 before it was even legible. See
// LAD-003.
func TestStepIdentitiesArePairedWithWhatTheStoreHolds(t *testing.T) {
	steps := []agentloop.PlanStep{
		{ID: "edit-1"}, {ID: "edit-2"}, {ID: "verify"},
	}
	stored := []storage.AgentPlanStep{
		{ID: "stp_a"}, {ID: "stp_b"}, {ID: "stp_c"},
	}

	paired := pairStepIdentities(steps, stored)
	for local, want := range map[string]string{
		"edit-1": "stp_a", "edit-2": "stp_b", "verify": "stp_c",
	} {
		if paired[local] != want {
			t.Errorf("%s paired to %q, want %q — the run would attribute its "+
				"work to a step the store never issued",
				local, paired[local], want)
		}
	}
}

// TestAShortRecordedPlanLeavesTheExtraStepsUnpaired is the case that has to
// stay visible rather than silent.
//
// A step with no durable identity cannot be attributed to and every read of it
// is refused. Pairing must not invent one, and the mismatch must not pass
// unremarked — the whole cost of this defect was that it did.
func TestAShortRecordedPlanLeavesTheExtraStepsUnpaired(t *testing.T) {
	steps := []agentloop.PlanStep{
		{ID: "edit-1"}, {ID: "edit-2"}, {ID: "verify"},
	}
	stored := []storage.AgentPlanStep{{ID: "stp_a"}}

	paired := pairStepIdentities(steps, stored)
	if paired["edit-1"] != "stp_a" {
		t.Errorf("the recorded step was not paired: %v", paired)
	}
	if _, present := paired["edit-2"]; present {
		t.Error("a step the store never wrote was given a durable identity")
	}
	if _, present := paired["verify"]; present {
		t.Error("a step the store never wrote was given a durable identity")
	}
	// adoptDurablePlanSteps drops what pairing leaves out, so the two together
	// mean the run carries only steps the store knows.
	adopted := adoptDurablePlanSteps(steps, durablePlan{Steps: paired})
	if len(adopted) != 1 || adopted[0].ID != "stp_a" {
		t.Errorf("the run kept steps the store never wrote: %+v", adopted)
	}
}

// TestPairingAnEmptyPlanPairsNothing keeps the degraded path honest.
//
// A store that wrote no steps leaves nothing to pair against. Pairing has to
// answer with an empty map rather than a partial one, so the caller's own
// fallback decides what happens rather than inheriting half a mapping.
func TestPairingAnEmptyPlanPairsNothing(t *testing.T) {
	paired := pairStepIdentities(
		[]agentloop.PlanStep{{ID: "edit-1"}}, nil)
	if len(paired) != 0 {
		t.Errorf("want no pairings, got %v", paired)
	}
}

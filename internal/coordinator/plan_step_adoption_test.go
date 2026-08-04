package coordinator

import (
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
)

// TestAStepTheRecordedPlanDoesNotKnowIsDropped is the silent mismatch that
// ended two ladder passes.
//
// Adoption maps each step's local identity onto the one the store recorded. A
// step with no mapping used to be kept with its local identity, and the store
// then refuses it on every read: "load plan step state: database constraint:
// step does not belong to run plan". That arrives mid-attempt, names no step,
// and reaches the coordinator as an unrecognised loop error — which costs an
// attempt each time and, three times over, the run.
//
// Ladder rung 18 on 2026-08-04 lost two whole passes to it, and the trace said
// only "the loop refused the attempt".
func TestAStepTheRecordedPlanDoesNotKnowIsDropped(t *testing.T) {
	steps := []agentloop.PlanStep{
		{ID: "edit-1"}, {ID: "edit-2"}, {ID: "verify"},
	}
	plan := durablePlan{Steps: map[string]string{
		"edit-1": "stp_one",
		"verify": "stp_verify",
	}}

	adopted := adoptDurablePlanSteps(steps, plan)
	if len(adopted) != 2 {
		t.Fatalf("want the two recorded steps, got %d: %+v",
			len(adopted), adopted)
	}
	for _, step := range adopted {
		if step.ID == "edit-2" {
			t.Error("a step the plan never recorded survived adoption, so " +
				"every read of its state will be refused by the store")
		}
	}
	if adopted[0].ID != "stp_one" || adopted[1].ID != "stp_verify" {
		t.Errorf("the recorded steps did not take their durable identities: %+v",
			adopted)
	}
}

// TestEveryMappedStepIsKept is the control.
//
// The ordinary case is that every step maps, and dropping must not become a way
// to lose work: a plan that records all its steps has to come back whole and in
// order.
func TestEveryMappedStepIsKept(t *testing.T) {
	steps := []agentloop.PlanStep{
		{ID: "edit-1"}, {ID: "edit-2"}, {ID: "verify"},
	}
	plan := durablePlan{Steps: map[string]string{
		"edit-1": "stp_one", "edit-2": "stp_two", "verify": "stp_verify",
	}}

	adopted := adoptDurablePlanSteps(steps, plan)
	if len(adopted) != 3 {
		t.Fatalf("a fully recorded plan lost a step: %+v", adopted)
	}
	for index, want := range []string{"stp_one", "stp_two", "stp_verify"} {
		if adopted[index].ID != want {
			t.Errorf("step %d is %q, want %q", index, adopted[index].ID, want)
		}
	}
}

// TestAnUnrecordedPlanLeavesTheStepsAlone keeps the no-plan path working.
//
// When nothing was recorded at all — the store refused the plan, or there is no
// store — the run proceeds on its own identities. Dropping every step there
// would turn a degraded run into no run.
func TestAnUnrecordedPlanLeavesTheStepsAlone(t *testing.T) {
	steps := []agentloop.PlanStep{{ID: "edit-1"}, {ID: "verify"}}
	if adopted := adoptDurablePlanSteps(steps, durablePlan{}); len(adopted) != 2 {
		t.Errorf("a run with no recorded plan lost its steps: %+v", adopted)
	}
}

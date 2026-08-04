package agent

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/executor"
)

// TestAMislabelledCallIsAttributedToOneStepEverywhere is the consistency the
// mislabelled-call correction was missing.
//
// The correction itself is right and is not in question: when the model files a
// write under the wrong step and exactly one other step names that very path,
// the loop binds the call to the step that owns the path rather than discarding
// the turn. It was added because refusing cost ladder rung 5 a third of its
// budget, and TestAMislabelledWriteIsBoundToTheStepThatOwnsItsPath covers it.
//
// What was wrong is that it moved stepIndex and left proposed.PlanStepID alone.
// The tool request, the tool result and the checkpoints were written from the
// model's label; the plan-step transition was written from the corrected step.
// Two answers to one question, and the durable consistency trigger on
// agent_plan_step_transitions matches a transition to its tool request on task,
// run, plan revision, step and model request — so it refused the transition,
// PersistPlanStepTransition returned the constraint error rather than tolerating
// it the way it tolerates a stale revision, and the loop returned it, which
// discards the attempt.
//
// Proven to discriminate: against the previous implementation this fixture
// recorded the tool start under "step-002" and the transitions under
// "step-001". Ladder rung 1 on 2026-08-03 lost its second attempt to exactly
// that, after four patches, reported as "the last attempt was refused before
// any of its work was recorded: record plan step transition: database
// constraint: constraint failed: plan step transition attribution is
// inconsistent (1811)".
func TestAMislabelledCallIsAttributedToOneStepEverywhere(t *testing.T) {
	harness := newLoopHarness(t)
	harness.input.Plan = PlanProjection{
		Revision: 5, RepositoryRevision: "git-revision",
		Steps: []PlanStep{
			{
				ID: "step-001", Kind: StepKindEdit,
				SummaryRedacted: "Write the program",
				State:           StepPending, MaterialEdit: true,
				ValidationRequired: true,
				ExpectedFiles:      []string{"main.go"},
				CompletionTools:    []executor.ToolName{executor.ToolApplyEdit},
			},
			{
				ID: "step-002", Kind: StepKindEdit,
				SummaryRedacted: "Write its tests",
				State:           StepPending, MaterialEdit: true,
				ValidationRequired: true,
				ExpectedFiles:      []string{"main_test.go"},
				CompletionTools:    []executor.ToolName{executor.ToolApplyEdit},
			},
		},
	}
	requestOne := newModelRequestID(t)
	requestTwo := newModelRequestID(t)
	harness.model.steps = []modelStep{
		func(_ context.Context, _ ModelInput) (ModelTurn, error) {
			// main.go belongs to step-001. The model says step-002.
			return successfulToolTurn(
				harness.model.identity,
				requestOne,
				"edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main\n"}`,
				"step-002",
				true,
			), nil
		},
		func(_ context.Context, _ ModelInput) (ModelTurn, error) {
			return completionTurn(
				harness.model.identity,
				requestTwo,
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.result = executor.ToolResult{
		RequestID: "edit-1", SchemaVersion: executor.ToolSchemaVersion,
		State: "succeeded", ExitCode: 0, Summary: "edit applied",
	}

	if _, err := harness.loop.Run(t.Context(), harness.input); err != nil {
		t.Fatalf("a mislabelled write of a file the plan named ended the run: %v",
			err)
	}

	if len(harness.journal.starts) != 1 {
		t.Fatalf("expected one journalled tool start, got %d",
			len(harness.journal.starts))
	}
	if len(harness.plan.transitions) == 0 {
		t.Fatal("no plan-step transition was recorded, so there is nothing " +
			"for the tool request to be consistent with")
	}

	const owning = "step-001"
	if got := harness.journal.starts[0].PlanStepID; got != owning {
		t.Errorf("the tool request was journalled under %q, the model's label, "+
			"not %q, the step the loop bound it to", got, owning)
	}
	for _, result := range harness.journal.results {
		if result.PlanStepID != owning {
			t.Errorf("a tool result was journalled under %q, want %q",
				result.PlanStepID, owning)
		}
	}
	for _, transition := range harness.plan.transitions {
		if transition.PlanStepID != owning {
			t.Errorf("a transition was recorded under %q, want %q",
				transition.PlanStepID, owning)
		}
	}
	for _, checkpoint := range harness.checkpoints.requests {
		if checkpoint.PlanStepID != owning {
			t.Errorf("a checkpoint was recorded under %q, want %q",
				checkpoint.PlanStepID, owning)
		}
	}

	// The trigger's actual predicate, stated as this test's own assertion:
	// every transition naming a tool request must agree with that request
	// about the step. Asserting the pairing rather than only the constant is
	// what makes this a guard against the constraint rather than against one
	// hard-coded string.
	byRequest := map[string]string{}
	for _, start := range harness.journal.starts {
		byRequest[start.RequestID] = start.PlanStepID
	}
	for _, transition := range harness.plan.transitions {
		if transition.ToolRequestID == "" {
			continue
		}
		journalled, present := byRequest[transition.ToolRequestID]
		if !present {
			t.Errorf("transition names tool request %q, which was never "+
				"journalled", transition.ToolRequestID)
			continue
		}
		if journalled != transition.PlanStepID {
			t.Errorf("tool request %q was journalled under step %q while its "+
				"transition claims step %q; the durable consistency trigger "+
				"refuses exactly this pair",
				transition.ToolRequestID, journalled, transition.PlanStepID)
		}
	}
}

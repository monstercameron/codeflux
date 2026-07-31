package composer_test

import (
	"slices"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
)

func TestComposerKeyboardConventionIsExhaustive(t *testing.T) {
	tests := []struct {
		name      string
		input     composer.KeyInput
		canSubmit bool
		want      composer.KeyboardAction
	}{
		{name: "enter submits", input: composer.KeyInput{Key: "Enter"}, canSubmit: true, want: composer.KeyboardSubmit},
		{name: "shift enter newline", input: composer.KeyInput{Key: "Enter", Shift: true}, canSubmit: true, want: composer.KeyboardNewline},
		{name: "empty enter blocked", input: composer.KeyInput{Key: "Enter"}, want: composer.KeyboardBlocked},
		{name: "composition inert", input: composer.KeyInput{Key: "Enter", Composing: true}, canSubmit: true, want: composer.KeyboardNone},
		{name: "repeat inert", input: composer.KeyInput{Key: "Enter", Repeat: true}, canSubmit: true, want: composer.KeyboardNone},
		{name: "control enter inert", input: composer.KeyInput{Key: "Enter", Control: true}, canSubmit: true, want: composer.KeyboardNone},
		{name: "alt enter inert", input: composer.KeyInput{Key: "Enter", Alt: true}, canSubmit: true, want: composer.KeyboardNone},
		{name: "meta enter inert", input: composer.KeyInput{Key: "Enter", Meta: true}, canSubmit: true, want: composer.KeyboardNone},
		{name: "modified shift enter inert", input: composer.KeyInput{Key: "Enter", Shift: true, Control: true}, canSubmit: true, want: composer.KeyboardNone},
		{name: "shift enter works when submit blocked", input: composer.KeyInput{Key: "Enter", Shift: true}, want: composer.KeyboardNewline},
		{name: "ordinary key inert", input: composer.KeyInput{Key: "a"}, canSubmit: true, want: composer.KeyboardNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := composer.ResolveKeyboard(test.input, test.canSubmit); got != test.want {
				t.Fatalf("ResolveKeyboard() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTaskActionMatrixMatchesEveryDurableState(t *testing.T) {
	tests := map[domain.TaskState][]composer.TaskAction{
		domain.TaskStateDraft:                {composer.ActionSend, composer.ActionChangePolicy, composer.ActionChangeBudget},
		domain.TaskStateForecasting:          {composer.ActionStop},
		domain.TaskStateAwaitingPlanApproval: {composer.ActionApprovePlan, composer.ActionRequestChange, composer.ActionStop},
		domain.TaskStateReady:                {composer.ActionStart, composer.ActionChangeBudget, composer.ActionStop},
		domain.TaskStateRunning:              {composer.ActionPause, composer.ActionStop, composer.ActionInspectGraph},
		domain.TaskStateAwaitingAuthority:    {composer.ActionAllowOnce, composer.ActionAllowForTask, composer.ActionDeny, composer.ActionStop},
		domain.TaskStatePaused:               {composer.ActionResume, composer.ActionChangeBudget, composer.ActionReview, composer.ActionStop},
		domain.TaskStateValidating:           {composer.ActionPause, composer.ActionStop, composer.ActionInspectChecks},
		domain.TaskStateAwaitingReview:       {composer.ActionReview, composer.ActionAccept, composer.ActionRepair, composer.ActionReject, composer.ActionRollback},
		domain.TaskStateRecoveryRequired:     {composer.ActionSafeResume, composer.ActionReconcile, composer.ActionPreservePatch, composer.ActionAbandon},
		domain.TaskStateCompleted:            {composer.ActionInspectEvidence, composer.ActionStartRelatedTask},
		domain.TaskStateFailed:               {composer.ActionInspect, composer.ActionRepair, composer.ActionPreservePatch},
		domain.TaskStateCancelled:            {composer.ActionInspect, composer.ActionPreservePatch, composer.ActionNewAttempt},
		domain.TaskStateRolledBack:           {composer.ActionResumeNewPlan, composer.ActionFinish},
	}
	declared := domain.AllTaskStates()
	if len(tests) != len(declared) {
		t.Fatalf("matrix cases = %d, durable states = %d", len(tests), len(declared))
	}
	for _, taskState := range declared {
		want, exists := tests[taskState]
		if !exists {
			t.Fatalf("durable task state %q is absent from composer matrix", taskState)
		}
		got := composer.AvailableTaskActions(taskState)
		if got.PrimaryMessage == "" || !slices.Equal(got.Actions, want) {
			t.Errorf("state %s action set = %#v, want %v", taskState, got, want)
		}
	}
}

func TestStopIsImmediatelyReachableThroughoutActiveWork(t *testing.T) {
	for _, taskState := range []domain.TaskState{
		domain.TaskStateForecasting,
		domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateReady,
		domain.TaskStateRunning,
		domain.TaskStateAwaitingAuthority,
		domain.TaskStatePaused,
		domain.TaskStateValidating,
	} {
		if !composer.AvailableTaskActions(taskState).StopImmediatelyReachable() {
			t.Errorf("Stop is not immediately reachable in %s", taskState)
		}
	}
	if composer.AvailableTaskActions(domain.TaskStateCompleted).Has(composer.ActionStop) {
		t.Fatal("completed task still exposes Stop")
	}
}

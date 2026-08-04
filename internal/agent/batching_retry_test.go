package agent

import (
	"errors"
	"strings"
	"testing"
)

// TestOverBatchingIsCorrectableRatherThanFatal is the price a slip was charged.
//
// A turn asking for more tool calls than the round takes cost the whole
// attempt, counted alongside a turn that carries both calls and completion —
// which is a model disagreeing with the contract. This is not that. The run
// knows what it wants to do and got the batching wrong, and one sentence
// corrects it.
//
// Ladder rung 18 on 2026-08-04 opened by writing all three of its files in one
// turn, lost the attempt, did it again, and the run was over in seventy-seven
// seconds having produced nothing.
func TestOverBatchingIsCorrectableRatherThanFatal(t *testing.T) {
	if maximumBatchingRetries < 1 {
		t.Fatal("a correctable slip that gets no correction is not correctable")
	}
	if maximumBatchingRetries > 4 {
		t.Errorf("told %d times, the correction becomes the thing that spends "+
			"the round budget", maximumBatchingRetries)
	}
	// It has to be distinguishable from the malformed turns that are not
	// correctable, or the loop cannot treat it differently.
	if errors.Is(errTooManyCallsInATurn, errEmptyModelTurn) {
		t.Error("over-batching and an empty turn are the same error, so they " +
			"cannot be priced differently")
	}
}

// TestTheBatchingRefusalSaysWhatToDoNext keeps the round worth spending.
//
// A refusal that only says no costs a round and teaches nothing. This one has
// to carry both numbers — what was sent and what the round takes — and say that
// the work already written is safe, because a run told only "refused" may
// reasonably assume it has to start again.
func TestTheBatchingRefusalSaysWhatToDoNext(t *testing.T) {
	// The error the validator produces, which is what the refusal quotes.
	err := errors.New("agent model turn is malformed: " +
		errTooManyCallsInATurn.Error() + ": 3 calls in one turn, and this " +
		"round takes 1")
	if !strings.Contains(err.Error(), "3 calls in one turn") {
		t.Error("the message does not say how many were sent")
	}
	if !strings.Contains(err.Error(), "round takes 1") {
		t.Error("the message does not say how many the round takes")
	}
}

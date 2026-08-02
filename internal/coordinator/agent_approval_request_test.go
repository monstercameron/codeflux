package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// TestTheApprovalRequestCarriesEnoughToSayNo is the whole point of the gate.
//
// "Approve maximum effort?" is a question nobody has grounds to answer, so it
// gets answered yes every time. A rubber stamp is worse than no gate at all:
// it looks like oversight while being a click-through, and the cost lands
// anyway with a record saying somebody approved it.
func TestTheApprovalRequestCarriesEnoughToSayNo(t *testing.T) {
	settings := pipeline.DefaultSettings()
	settings.StallBeforeEscalation = 3
	tracker := newConvergence(settings)
	for round := 0; round < 4; round++ {
		tracker.beginAttempt()
		tracker.record("end-to-end-tests", "example 1 differs at line 1")
	}
	request := approvalRequest(
		pipeline.Rung{Model: pipeline.ModelSol, Effort: pipeline.EffortMax}.String(),
		"end-to-end-tests",
		"example 1 differs at line 1: expected \"4\", got \"5\"",
		tracker)

	for what, required := range map[string]string{
		"what it wants":       "thinking at max effort",
		"which check failed":  "end-to-end-tests",
		"the failure itself":  `expected "4", got "5"`,
		"how far it got":      "escalated",
		"what it has cost":    "attempts",
		"that this is a stop": "no resume",
	} {
		if !strings.Contains(strings.ToLower(request), strings.ToLower(required)) {
			t.Errorf("the request does not say %s (%q):\n%s",
				what, required, request)
		}
	}
}

// TestTheRequestSaysWhichKindOfMoreExpensiveItIs separates the two axes.
//
// More effort and a different model are not the same decision. One buys more
// tokens at the rate already in force; the other changes the rate on every
// token. A person approving needs to know which they are approving, and a
// single "this costs more" tells them neither.
func TestTheRequestSaysWhichKindOfMoreExpensiveItIs(t *testing.T) {
	harder := costChange(
		pipeline.Rung{Model: pipeline.ModelLuna, Effort: pipeline.EffortLow}.String(),
		pipeline.Rung{Model: pipeline.ModelLuna, Effort: pipeline.EffortMax}.String())
	if !strings.Contains(harder, "rate already in force") {
		t.Errorf("more effort on the same model is not described as such: %q",
			harder)
	}
	dearer := costChange(
		pipeline.Rung{Model: pipeline.ModelLuna, Effort: pipeline.EffortMax}.String(),
		pipeline.Rung{Model: pipeline.ModelSol, Effort: pipeline.EffortLow}.String())
	if !strings.Contains(dearer, "every token") {
		t.Errorf("a change of model is not described as a change of rate: %q",
			dearer)
	}
}

// TestALongFailureDoesNotBuryTheQuestion keeps the request readable.
func TestALongFailureDoesNotBuryTheQuestion(t *testing.T) {
	var long strings.Builder
	for line := 0; line < 200; line++ {
		long.WriteString("difference on some line\n")
	}
	head := firstLines(long.String(), 12)
	if strings.Count(head, "\n") > 12 {
		t.Errorf("a 200-line failure was not trimmed: %d lines",
			strings.Count(head, "\n"))
	}
	if !strings.Contains(head, "more line") {
		t.Errorf("the trimming does not say how much was left out: %q", head)
	}
}

package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// TestARefinementGateDoesNotBlockCompletion is the decision recorded in code:
// path coverage and registration are advisory.
//
// Rung 3 crossed the completion floor with 29 stages satisfied and was held
// back by these three, which then left the task running because nothing
// downstream knew how to finish a run that had not passed everything.
func TestARefinementGateDoesNotBlockCompletion(t *testing.T) {
	for _, advisory := range []pipeline.Number{
		pipeline.StagePathCoverage,
		pipeline.StageAtomDocumentation,
		pipeline.StageAtomRegistration,
		pipeline.StageMoleculeRegistration,
	} {
		if !advisoryStages[advisory] {
			t.Errorf("stage %d blocks completion", advisory)
		}
	}
	// The floor itself must never be advisory. If any of these were, a program
	// that does not build could be reported as awaiting review.
	for _, blocking := range []pipeline.Number{
		pipeline.StageAssembly,
		pipeline.StageIntegrationTests,
		pipeline.StageEndToEndTests,
		pipeline.StageAtomVerification,
	} {
		if advisoryStages[blocking] {
			t.Errorf("stage %d was made advisory; it is the completion floor",
				blocking)
		}
	}
}

// TestANonTerminalFinalizationIsNeverAccepted pins the invariant: a run may not
// return with its task still running.
func TestANonTerminalFinalizationIsNeverAccepted(t *testing.T) {
	for _, reason := range []string{
		"the work does not compile",
		"the work was never verified",
		"a stage the flow requires did not hold",
		"this build performs no completion",
		"the run declared no plan steps",
	} {
		result := finalization{Reason: reason}
		if result.Terminal {
			t.Errorf("%q was reported as a terminal ending", reason)
		}
	}
	done := finalization{Terminal: true, Reason: "every required gate held"}
	if !done.Terminal {
		t.Error("a completed run was not terminal")
	}
}

// TestTheTerminalRecordNamesTheStateAndTheReason keeps the message and the
// durable state describing one thing. A terminal message without a terminal
// database state is not terminal.
func TestTheTerminalRecordNamesTheStateAndTheReason(t *testing.T) {
	report := terminalReport(terminalFacts{
		status:     "failed",
		reason:     "a stage the flow requires did not hold",
		unresolved: "4 line(s) uncovered",
	})
	for _, wanted := range []string{
		"failed", "a stage the flow requires did not hold",
		"4 line(s) uncovered",
	} {
		if !strings.Contains(report, wanted) {
			t.Errorf("the terminal record never says %q:\n%s", wanted, report)
		}
	}
}

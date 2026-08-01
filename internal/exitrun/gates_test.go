package exitrun

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestM24_G01_TenGatesAreDeclaredAndEachCanFail(t *testing.T) {
	if err := ValidateGates(); err != nil {
		t.Fatalf("the declared gates are not valid: %v", err)
	}
	if got, want := len(DeclaredGates()), 10; got != want {
		t.Fatalf("%d gates declared, M24-G01..G10 needs %d", got, want)
	}
}

func TestM24_G01_NoGateRestsOnTheAgentsAccountOfItself(t *testing.T) {
	// §0 prohibits `agent self-report -> accepted outcome`. The source is
	// declared as a value so the prohibition is enforced rather than assumed.
	if EvidenceAgentSelfReport.Acceptable() {
		t.Fatal("the agent's self-report is listed as acceptable evidence")
	}
	for _, gate := range DeclaredGates() {
		if slices.Contains(gate.Sources, EvidenceAgentSelfReport) {
			t.Errorf("gate %q rests on the agent's self-report", gate.ID)
		}
	}

	compromised := DeclaredGates()[0]
	compromised.Sources = append(compromised.Sources, EvidenceAgentSelfReport)
	err := compromised.Validate()
	if err == nil {
		t.Fatal("a gate resting on the agent's self-report was accepted")
	}
	if !strings.Contains(err.Error(), "§0") {
		t.Errorf("the refusal does not cite the invariant it enforces: %v", err)
	}
}

func TestM24_G01_AGateWithNoDisqualifyingFindingIsRefused(t *testing.T) {
	for name, damage := range map[string]func(*Gate){
		"no question":     func(gate *Gate) { gate.Question = "" },
		"no source":       func(gate *Gate) { gate.Sources = nil },
		"no disqualifier": func(gate *Gate) { gate.DisqualifyingFinding = "" },
		"unknown source": func(gate *Gate) {
			gate.Sources = []EvidenceSource{"somebody said so"}
		},
		"not a gate todo": func(gate *Gate) { gate.ID = "M24-201" },
		"agreement from one source": func(gate *Gate) {
			gate.Sources = []EvidenceSource{EvidenceDurableState}
			gate.RequiresAllSources = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			gate := DeclaredGates()[0]
			damage(&gate)
			if err := gate.Validate(); err == nil {
				t.Fatalf("a gate with %s was accepted", name)
			}
		})
	}
}

func passingResults() map[GateID]GateResult {
	results := map[GateID]GateResult{}
	for _, gate := range DeclaredGates() {
		results[gate.ID] = GateResult{
			ID: gate.ID, Answered: gate.Sources, Passed: true,
		}
	}
	return results
}

func TestM24_G01_AFullyEvidencedRunIsReady(t *testing.T) {
	readiness, err := EvaluateExit(passingResults())
	if err != nil {
		t.Fatalf("evaluating a complete run failed: %v", err)
	}
	if !readiness.Ready() {
		t.Fatalf("a run answering every gate was not ready: %+v", readiness)
	}
	if got, want := len(readiness.Passed), len(DeclaredGates()); got != want {
		t.Errorf("%d gates passed, want %d", got, want)
	}
	if len(readiness.UnestablishedCriteria) != 0 {
		t.Errorf("criteria remained unestablished: %v", readiness.UnestablishedCriteria)
	}
}

func TestM24_G01_AnUnmeasuredGateBlocksRatherThanPasses(t *testing.T) {
	// The default for a gate nobody measured must be the one that blocks, or
	// the protocol rewards not measuring.
	for _, omitted := range DeclaredGates() {
		t.Run(string(omitted.ID), func(t *testing.T) {
			results := passingResults()
			delete(results, omitted.ID)
			readiness, err := EvaluateExit(results)
			if err != nil {
				t.Fatalf("evaluating failed: %v", err)
			}
			if readiness.Ready() {
				t.Fatalf("a run that never measured %q was ready to exit", omitted.ID)
			}
			if !slices.Contains(readiness.Unanswered, omitted.ID) {
				t.Errorf("gate %q was not reported unanswered", omitted.ID)
			}
			if slices.Contains(readiness.Failed, omitted.ID) {
				t.Errorf(
					"gate %q was reported failed rather than unanswered; the two call for "+
						"different work", omitted.ID)
			}
		})
	}
}

func TestM24_G03_APartiallyAnsweredAgreementGateIsUnanswered(t *testing.T) {
	var agreementGate Gate
	for _, gate := range DeclaredGates() {
		if gate.RequiresAllSources {
			agreementGate = gate
			break
		}
	}
	if agreementGate.ID == "" {
		t.Fatal("no gate requires agreement between sources")
	}

	outcome, reason, err := agreementGate.Evaluate(GateResult{
		ID:       agreementGate.ID,
		Answered: agreementGate.Sources[:1],
		Passed:   true,
	})
	if err != nil {
		t.Fatalf("evaluating failed: %v", err)
	}
	if outcome != GateUnanswered {
		t.Fatalf("a gate answered by one of %d required sources was %q, want %q",
			len(agreementGate.Sources), outcome, GateUnanswered)
	}
	if !strings.Contains(reason, string(agreementGate.Sources[1])) {
		t.Errorf("the reason does not name the silent source: %q", reason)
	}
}

func TestM24_G01_ADisqualifyingFindingOverridesAPass(t *testing.T) {
	// A run cannot report a gate as passed while also recording the single
	// observation the gate declares as fatal.
	for _, gate := range DeclaredGates() {
		outcome, reason, err := gate.Evaluate(GateResult{
			ID:                           gate.ID,
			Answered:                     gate.Sources,
			Passed:                       true,
			DisqualifyingFindingObserved: true,
		})
		if err != nil {
			t.Fatalf("evaluating %q failed: %v", gate.ID, err)
		}
		if outcome != GateFailed {
			t.Errorf("gate %q passed while its disqualifying finding was observed", gate.ID)
		}
		if reason != gate.DisqualifyingFinding {
			t.Errorf("gate %q failed for %q, want its declared disqualifier", gate.ID, reason)
		}
	}
}

func TestM24_G01_AFailedGateMustRecordWhy(t *testing.T) {
	gate := DeclaredGates()[0]
	if _, _, err := gate.Evaluate(GateResult{
		ID: gate.ID, Answered: gate.Sources, Passed: false,
	}); err == nil {
		t.Fatal("a gate failed with no recorded reason")
	}

	outcome, reason, err := gate.Evaluate(GateResult{
		ID: gate.ID, Answered: gate.Sources, Passed: false,
		Note: "the approval for the migration command was never presented",
	})
	if err != nil {
		t.Fatalf("evaluating failed: %v", err)
	}
	if outcome != GateFailed || !strings.Contains(reason, "approval") {
		t.Errorf("a recorded failure was reported as %q / %q", outcome, reason)
	}
}

func TestM24_G01_AGateCannotBeAnsweredBySourcesItDoesNotDeclare(t *testing.T) {
	var narrow Gate
	for _, gate := range DeclaredGates() {
		if len(gate.Sources) == 1 {
			narrow = gate
			break
		}
	}
	if narrow.ID == "" {
		t.Skip("no single-source gate is declared")
	}
	undeclared := EvidenceObservedSession
	if slices.Contains(narrow.Sources, undeclared) {
		undeclared = EvidenceGitHistory
	}
	if _, _, err := narrow.Evaluate(GateResult{
		ID: narrow.ID, Answered: []EvidenceSource{undeclared}, Passed: true,
	}); err == nil {
		t.Fatalf("gate %q accepted an answer from %q, which it does not declare",
			narrow.ID, undeclared)
	}

	if _, _, err := narrow.Evaluate(GateResult{
		ID: narrow.ID, Answered: []EvidenceSource{EvidenceAgentSelfReport}, Passed: true,
	}); err == nil {
		t.Fatal("a gate accepted the agent's own account as an answer")
	}
}

func TestM24_G01_AResultCannotBeAppliedToTheWrongGate(t *testing.T) {
	gates := DeclaredGates()
	if _, _, err := gates[0].Evaluate(GateResult{
		ID: gates[1].ID, Answered: gates[0].Sources, Passed: true,
	}); err == nil {
		t.Fatal("a result for one gate was applied to another")
	}

	if _, err := EvaluateExit(map[GateID]GateResult{
		"M24-G99": {ID: "M24-G99", Passed: true},
	}); err == nil {
		t.Fatal("a result for an undeclared gate was accepted")
	}
}

func TestDONE_001_EveryCompletionCriterionIsBoundToAGate(t *testing.T) {
	if err := ValidateExitCriteria(); err != nil {
		t.Fatalf("the declared exit criteria are not valid: %v", err)
	}
	criteria := DeclaredExitCriteria()
	if got, want := len(criteria), 16; got != want {
		t.Fatalf("%d criteria declared, DONE-001..016 needs %d", got, want)
	}
	for index, criterion := range criteria {
		if got, want := criterion.ID, fmt.Sprintf("DONE-%03d", index+1); got != want {
			t.Errorf("criterion %d is %q, want %q", index, got, want)
		}
		if len(criterion.Gates) == 0 {
			t.Errorf("criterion %q is established by no gate", criterion.ID)
		}
	}
}

func TestDONE_001_AFailedGateUnestablishesEveryCriterionItSupports(t *testing.T) {
	for _, gate := range DeclaredGates() {
		t.Run(string(gate.ID), func(t *testing.T) {
			results := passingResults()
			results[gate.ID] = GateResult{
				ID: gate.ID, Answered: gate.Sources,
				Passed: true, DisqualifyingFindingObserved: true,
			}
			readiness, err := EvaluateExit(results)
			if err != nil {
				t.Fatalf("evaluating failed: %v", err)
			}
			if readiness.Ready() {
				t.Fatalf("a run with %q failed was ready to exit", gate.ID)
			}
			var expected []string
			for _, criterion := range DeclaredExitCriteria() {
				if slices.Contains(criterion.Gates, gate.ID) {
					expected = append(expected, criterion.ID)
				}
			}
			// Every gate supports at least one criterion; ValidateExitCriteria
			// refuses a gate that supports none.
			if len(expected) == 0 {
				t.Fatalf("gate %q supports no criterion", gate.ID)
			}
			for _, id := range expected {
				if !slices.Contains(readiness.UnestablishedCriteria, id) {
					t.Errorf("criterion %q still stood after %q failed", id, gate.ID)
				}
			}
		})
	}
}

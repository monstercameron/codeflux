package retrievalgate

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
)

// TestEvaluateEligibility_AcceptsLegitimateCandidate proves the suite does
// not over-reject: a well-formed candidate that satisfies every gate must
// come back eligible with no reason.
func TestEvaluateEligibility_AcceptsLegitimateCandidate(t *testing.T) {
	project := mustProjectID(t)
	repository := mustRepositoryID(t)
	task := sampleFingerprint(t, project, repository)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}
	candidate := eligibleCandidate(t, domain.MemoryProjectScope{Project: project, Repository: repository}, task)

	decision, err := EvaluateEligibility(candidate, boundary, task)
	if err != nil {
		t.Fatalf("EvaluateEligibility: unexpected error %v", err)
	}
	if !decision.Eligible {
		t.Fatalf("EvaluateEligibility: want eligible, got reject reason %q detail %q", decision.Reason, decision.Detail)
	}
	if decision.Reason != "" {
		t.Fatalf("EvaluateEligibility: an eligible decision must carry no reason, got %q", decision.Reason)
	}
	// M21-072: every gate that ran is preserved for audit, not only the
	// deciding one.
	wantGates := []string{"project-boundary", "toolchain-compatibility", "dependency-compatibility", "invalidated-evidence", "assurance-below-requirement"}
	if len(decision.GateResults) != len(wantGates) {
		t.Fatalf("GateResults has %d entries, want %d: %+v", len(decision.GateResults), len(wantGates), decision.GateResults)
	}
	for i, name := range wantGates {
		if decision.GateResults[i].Gate != name {
			t.Fatalf("GateResults[%d].Gate = %q, want %q", i, decision.GateResults[i].Gate, name)
		}
		if !decision.GateResults[i].Outcome.Eligible {
			t.Fatalf("GateResults[%d] (%s) unexpectedly rejected: %+v", i, name, decision.GateResults[i].Outcome)
		}
	}
}

// TestEvaluateEligibility_RunsEveryGateEvenAfterAnEarlyFailure (M21-136):
// discovery and eligibility are different phases, and within eligibility
// every required gate still runs so the full picture is auditable, even
// though the aggregate Reason reports only the first (project-boundary)
// failure in the declared fixed order.
func TestEvaluateEligibility_RunsEveryGateEvenAfterAnEarlyFailure(t *testing.T) {
	project := mustProjectID(t)
	otherProject := mustProjectID(t)
	repository := mustRepositoryID(t)
	task := sampleFingerprint(t, project, repository)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}

	candidate := eligibleCandidate(t, domain.MemoryProjectScope{Project: otherProject, Repository: repository}, task)
	// Also break toolchain compatibility to prove it is still evaluated and
	// recorded even though project-boundary already failed first.
	candidate.RequiredToolchainBindings = []fingerprint.ToolchainBinding{{Name: "go", Version: "9.9.9"}}

	decision, err := EvaluateEligibility(candidate, boundary, task)
	if err != nil {
		t.Fatalf("EvaluateEligibility: unexpected error %v", err)
	}
	if decision.Eligible {
		t.Fatal("EvaluateEligibility: want reject, got eligible")
	}
	if decision.Reason != RejectionReasonProjectBoundaryMismatch {
		t.Fatalf("Reason = %q, want %q (first gate in the declared order)", decision.Reason, RejectionReasonProjectBoundaryMismatch)
	}
	var sawToolchainFailure bool
	for _, gate := range decision.GateResults {
		if gate.Gate == "toolchain-compatibility" {
			if gate.Outcome.Eligible {
				t.Fatal("toolchain-compatibility gate should have failed too, but was recorded eligible")
			}
			sawToolchainFailure = true
		}
	}
	if !sawToolchainFailure {
		t.Fatal("toolchain-compatibility gate must still run and be recorded after project-boundary already failed (M21-136: gates are not short-circuited out of the audit trail)")
	}
}

// TestEvaluateEligibility_ApplicabilityGateRunsAfterDiscovery is the
// M21-136 phase-ordering test: an applicability predicate that fails
// rejects the candidate through EligibilityCandidate.Applicability, which
// EvaluateEligibility only ever receives as part of the post-discovery
// EligibilityCandidate -- never as part of a DiscoveredCandidate.
func TestEvaluateEligibility_ApplicabilityGateRunsAfterDiscovery(t *testing.T) {
	project := mustProjectID(t)
	repository := mustRepositoryID(t)
	otherRepository := mustRepositoryID(t)
	task := sampleFingerprint(t, project, repository)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}

	candidate := eligibleCandidate(t, domain.MemoryProjectScope{Project: project, Repository: repository}, task)
	candidate.Applicability = []ApplicabilityPredicate{{
		Kind:                     ApplicabilityPredicateKindRepositoryMatch,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		RepositoryMatch:          otherRepository,
	}}

	decision, err := EvaluateEligibility(candidate, boundary, task)
	if err != nil {
		t.Fatalf("EvaluateEligibility: unexpected error %v", err)
	}
	if decision.Eligible {
		t.Fatal("EvaluateEligibility: want reject for a failed applicability predicate, got eligible")
	}
	if decision.Reason != RejectionReasonApplicabilityPredicateFailed {
		t.Fatalf("Reason = %q, want %q", decision.Reason, RejectionReasonApplicabilityPredicateFailed)
	}
}

func TestEvaluateEligibility_InvalidFingerprintIsError(t *testing.T) {
	project := mustProjectID(t)
	repository := mustRepositoryID(t)
	task := sampleFingerprint(t, project, repository)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}
	candidate := eligibleCandidate(t, domain.MemoryProjectScope{Project: project, Repository: repository}, task)

	broken := task
	broken.SchemaVersion = task.SchemaVersion + 1
	if _, err := EvaluateEligibility(candidate, boundary, broken); err == nil {
		t.Fatal("EvaluateEligibility: want error for an invalid task fingerprint, got nil")
	}
}

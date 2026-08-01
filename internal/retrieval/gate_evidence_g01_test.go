package retrieval

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/retrievalgate"
	"codeflux.dev/codeflux/internal/storage"
)

// TestGateEvidenceG01_NoInfluenceWithoutFourChecks is the M21-G01 gate-
// evidence test: "No memory item influences a task without project,
// compatibility, validity, and assurance checks." Each subtest drives
// retrieval.Service.RunPreWorkGate against a real, temporary, migrated
// SQLite database and independently breaks exactly one of the four checks,
// proving that break alone is sufficient to keep the candidate out of
// PreWorkGateResult.Eligible and to durably record its rejection --
// regardless of the fact that every OTHER check on the same candidate would
// have passed. A final control candidate satisfies all four and is shown
// eligible and successfully recordable as influence, so the four rejection
// cases are not merely testing an unreachable code path.
//
// Compatibility note: internal/retrieval.buildEligibilityCandidate documents
// that no memory-artifact kind today declares its own top-level
// RequiredToolchainBindings/RequiredDependencyBindings (they are always
// nil), so retrievalgate.EvaluateToolchainCompatibility/
// EvaluateDependencyCompatibility are structurally vacuous on every real
// candidate the live service builds. The "compatibility" check IS live-
// wired today, but only through the M21-022 applicability-predicate
// mechanism (ApplicabilityPredicateKindDependencyBinding), which is what
// this test exercises. The pure toolchain/dependency-binding gate functions
// themselves are covered directly, at the unit level, by
// internal/retrievalgate/gates_test.go.
func TestGateEvidenceG01_NoInfluenceWithoutFourChecks(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}

	// Each subtest gets its own fresh project/repository/task so that
	// discoverByKind's repository-scoped scan in one subtest can never pick
	// up an artifact created by a different subtest -- every subtest's
	// eligibility outcome depends only on the one check it is proving.

	t.Run("project-boundary", func(t *testing.T) {
		projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
		task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelContractChecked)
		otherRepositoryID, err := domain.NewRepositoryID()
		if err != nil {
			t.Fatal(err)
		}
		_, revisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./... (boundary case)")
		mustLinkAssuranceEvidence(t, repositories, projectID, repositoryID, revisionID, domain.AssuranceLevelContractChecked)

		queryID := newTestQueryID(t, "project-boundary")
		if _, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
			QueryID: queryID, ProjectID: projectID,
			// The boundary restricts the query to a DIFFERENT repository
			// than the one this candidate (and the task itself) belongs to.
			// Every other check on this candidate would pass.
			Boundary: domain.MemoryQueryProjectBoundary{Project: projectID, IncludeRepositories: []domain.RepositoryID{otherRepositoryID}},
			Task:     task,
		}); err != nil {
			t.Fatal(err)
		}
		assertRejectedNeverEligible(t, ctx, repositories, queryID, revisionID, retrievalgate.RejectionReasonProjectBoundaryMismatch)
	})

	t.Run("compatibility", func(t *testing.T) {
		projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
		task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelContractChecked)
		_, revisionID := mustCreateExecutionRecipeArtifact(t, repositories, projectID, repositoryID)
		mustLinkAssuranceEvidence(t, repositories, projectID, repositoryID, revisionID, domain.AssuranceLevelContractChecked)
		// The task fingerprint (mustBuildTaskFingerprint) carries no
		// toolchain/dependency bindings at all; a dependency-binding
		// predicate requiring one therefore always fails to match, exactly
		// the live wiring for M21-069 "toolchain/dependency mismatch" today.
		mustCreateDependencyBindingPredicate(t, repositories, revisionID, task.SchemaVersion, "go", "9.9.9")

		queryID := newTestQueryID(t, "compatibility")
		result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
			QueryID: queryID, ProjectID: projectID,
			Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
			Task:     task,
		})
		if err != nil {
			t.Fatal(err)
		}
		assertRejectedNeverEligible(t, ctx, repositories, queryID, revisionID, retrievalgate.RejectionReasonApplicabilityPredicateFailed)
		_ = result
	})

	t.Run("validity", func(t *testing.T) {
		projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
		task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelContractChecked)
		_, revisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go vet ./... (validity case)")
		mustLinkAssuranceEvidence(t, repositories, projectID, repositoryID, revisionID, domain.AssuranceLevelContractChecked)
		if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, storage.TransitionMemoryArtifactMaturity{
			RevisionID: revisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateInvalidated,
			ReasonKind:     storage.MemoryArtifactInvalidationReasonOther,
			DetailRedacted: "gate-evidence test: force this revision invalid to prove the validity check alone blocks eligibility",
			IdempotencyKey: "g01-validity-invalidate-" + revisionID.String(),
		}); err != nil {
			t.Fatal(err)
		}

		queryID := newTestQueryID(t, "validity")
		result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
			QueryID: queryID, ProjectID: projectID,
			Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
			Task:     task,
		})
		if err != nil {
			t.Fatal(err)
		}
		assertRejectedNeverEligible(t, ctx, repositories, queryID, revisionID, retrievalgate.RejectionReasonInvalidatedEvidence)
		_ = result
	})

	t.Run("assurance", func(t *testing.T) {
		projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
		task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelContractChecked)
		_, revisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go test ./... (assurance case)")
		// Deliberately no supporting evidence at all: achieved assurance
		// defaults to runtime-only, below the task's contract-checked
		// requirement.

		queryID := newTestQueryID(t, "assurance")
		result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
			QueryID: queryID, ProjectID: projectID,
			Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
			Task:     task,
		})
		if err != nil {
			t.Fatal(err)
		}
		assertRejectedNeverEligible(t, ctx, repositories, queryID, revisionID, retrievalgate.RejectionReasonAssuranceBelowRequirement)
		_ = result
	})

	t.Run("control-eligible-and-influenced", func(t *testing.T) {
		projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
		task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelContractChecked)
		_, revisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./... (control case)")
		mustLinkAssuranceEvidence(t, repositories, projectID, repositoryID, revisionID, domain.AssuranceLevelContractChecked)

		queryID := newTestQueryID(t, "control")
		result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
			QueryID: queryID, ProjectID: projectID,
			Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
			Task:     task,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Eligible) != 1 || result.Eligible[0].RevisionID != revisionID {
			t.Fatalf("control candidate satisfying all four checks = %#v, want exactly one eligible item", result.Eligible)
		}
		if err := service.RecordInfluence(ctx, result.Eligible[0], retrievalgate.AgentInfluenceActionUsed, "control case: all four checks passed"); err != nil {
			t.Fatalf("RecordInfluence on the fully-eligible control candidate: %v", err)
		}
		candidateID := deriveCandidateID(queryID, revisionID)
		decision, found, err := repositories.GetMemoryRetrievalDecision(ctx, candidateID)
		if err != nil {
			t.Fatal(err)
		}
		if !found || decision.Decision != "accepted" {
			t.Fatalf("control decision = %#v (found=%v), want accepted", decision, found)
		}
	})
}

// assertRejectedNeverEligible asserts the shared G01 rejection shape: the
// query fell back (the only candidate discovered was rejected), the
// candidate never appears in any eligible list, and its rejection was
// durably recorded with wantReason immediately -- never as an
// "eligible-and-*" reason, and never later overwritten into one.
func assertRejectedNeverEligible(
	t *testing.T,
	ctx context.Context,
	repositories interface {
		GetMemoryRetrievalDecision(context.Context, string) (storage.MemoryRetrievalDecisionRecord, bool, error)
	},
	queryID string,
	revisionID domain.MemoryArtifactRevisionID,
	wantReason retrievalgate.RejectionReason,
) {
	t.Helper()
	candidateID := deriveCandidateID(queryID, revisionID)
	decision, found, err := repositories.GetMemoryRetrievalDecision(ctx, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected a decision to be recorded for candidate %s", candidateID)
	}
	if decision.Decision != "rejected" {
		t.Fatalf("decision.Decision = %q, want rejected", decision.Decision)
	}
	if string(decision.ReasonKind) != string(wantReason) {
		t.Fatalf("decision.ReasonKind = %q, want %q", decision.ReasonKind, wantReason)
	}
}

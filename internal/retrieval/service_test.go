package retrieval

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/retrievalgate"
)

func newTestQueryID(t *testing.T, suffix string) string {
	t.Helper()
	return "query-" + t.Name() + "-" + suffix
}

// TestRunPreWorkGateRetrievedButRejectedNeverRecordsInfluence proves the
// central §31 distinction this whole package exists to enforce: a
// candidate that was discovered and evaluated, but failed the eligibility
// gates, is logged as rejected and can never become an "eligible" item --
// so it structurally can never reach RecordInfluence, and never records as
// having influenced the outcome. Meanwhile a second, genuinely eligible
// candidate for the same query IS presented and, once the agent calls
// RecordInfluence, is durably recorded as used.
func TestRunPreWorkGateRetrievedButRejectedNeverRecordsInfluence(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelContractChecked)

	_, eligibleRevision := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go test ./... (eligible)")
	mustLinkAssuranceEvidence(t, repositories, projectID, repositoryID, eligibleRevision, domain.AssuranceLevelContractChecked)

	_, rejectedRevision := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go test ./... (insufficient evidence)")
	// Deliberately no supporting evidence: achieved assurance defaults to
	// runtime-only, below the task's required contract-checked level.

	queryID := newTestQueryID(t, "1")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Considered != 2 {
		t.Fatalf("considered = %d, want 2", result.Considered)
	}
	if result.FellBack {
		t.Fatal("expected the genuinely eligible candidate to prevent a fallback")
	}
	if len(result.Eligible) != 1 || result.Eligible[0].RevisionID != eligibleRevision {
		t.Fatalf("eligible = %#v, want exactly the eligible-evidence revision %v", result.Eligible, eligibleRevision)
	}

	// The rejected candidate must have an immediately-recorded rejection,
	// never an "eligible-and-*" reason.
	rejectedCandidateID := deriveCandidateID(queryID, rejectedRevision)
	rejectedDecision, found, err := repositories.GetMemoryRetrievalDecision(ctx, rejectedCandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected a decision to be recorded for the rejected candidate %s", rejectedCandidateID)
	}
	if rejectedDecision.Decision != "rejected" || string(rejectedDecision.ReasonKind) != string(retrievalgate.RejectionReasonAssuranceBelowRequirement) {
		t.Fatalf("rejected candidate decision = (%q, %q), want (rejected, assurance-below-requirement)", rejectedDecision.Decision, rejectedDecision.ReasonKind)
	}

	// Now the agent acts on the ONE eligible item. This is the only way a
	// decision can ever be recorded as "used": nothing lets the rejected
	// candidate reach this call, because it never appears in result.Eligible.
	if err := service.RecordInfluence(ctx, result.Eligible[0], retrievalgate.AgentInfluenceActionUsed, "reused the passing test command verbatim"); err != nil {
		t.Fatal(err)
	}
	eligibleCandidateID := deriveCandidateID(queryID, eligibleRevision)
	eligibleDecision, found, err := repositories.GetMemoryRetrievalDecision(ctx, eligibleCandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected a decision to be recorded for the eligible candidate %s", eligibleCandidateID)
	}
	if eligibleDecision.Decision != "accepted" || string(eligibleDecision.ReasonKind) != string(retrievalgate.RejectionReasonEligibleAndUsed) {
		t.Fatalf("eligible candidate decision = (%q, %q), want (accepted, eligible-and-used)", eligibleDecision.Decision, eligibleDecision.ReasonKind)
	}

	// The rejected candidate's decision must still read exactly as
	// originally logged: retrieval never silently became influence.
	rejectedAfter, found, err := repositories.GetMemoryRetrievalDecision(ctx, rejectedCandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected the rejected candidate's decision to still exist")
	}
	if rejectedAfter.Decision != "rejected" || string(rejectedAfter.ReasonKind) != string(retrievalgate.RejectionReasonAssuranceBelowRequirement) {
		t.Fatalf("rejected candidate decision after unrelated influence recording = (%q, %q), want unchanged (rejected, assurance-below-requirement)", rejectedAfter.Decision, rejectedAfter.ReasonKind)
	}
}

// TestRunPreWorkGateFallsBackCleanlyWhenNoEligibleItemExists proves M21-076:
// when nothing is eligible, RunPreWorkGate records the fallback once, as a
// normal outcome (no error), and reports it in the typed result.
func TestRunPreWorkGateFallsBackCleanlyWhenNoEligibleItemExists(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	queryID := newTestQueryID(t, "empty")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatalf("RunPreWorkGate with nothing to discover must return a clean fallback, not an error: %v", err)
	}
	if !result.FellBack {
		t.Fatal("expected FellBack = true when nothing was discovered")
	}
	if result.Considered != 0 || len(result.Eligible) != 0 {
		t.Fatalf("result = %#v, want zero considered and zero eligible", result)
	}

	fallback, found, err := repositories.GetMemoryRetrievalFallback(ctx, queryID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected the fallback to be durably recorded")
	}
	if fallback.CandidatesConsidered != 0 {
		t.Fatalf("fallback.CandidatesConsidered = %d, want 0", fallback.CandidatesConsidered)
	}

	// A second scenario: a candidate WAS discovered, but it was rejected --
	// this must also fall back cleanly, distinguishing "nothing existed"
	// from "everything was ineligible" only by the CandidatesConsidered
	// count, never by treating the second case as an error either.
	mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go test ./...")

	// Raise the task's own required assurance so the one discovered
	// artifact (which carries no supporting evidence) fails the assurance
	// gate.
	strictTask := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelFullyEvaluated)
	secondQueryID := newTestQueryID(t, "all-rejected")
	secondResult, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: secondQueryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     strictTask,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !secondResult.FellBack {
		t.Fatal("expected FellBack = true when every discovered candidate was rejected")
	}
	if secondResult.Considered == 0 {
		t.Fatal("expected at least one candidate to have been considered and rejected")
	}
	secondFallback, found, err := repositories.GetMemoryRetrievalFallback(ctx, secondQueryID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || secondFallback.CandidatesConsidered != secondResult.Considered {
		t.Fatalf("second fallback = %#v (found=%v), want CandidatesConsidered = %d", secondFallback, found, secondResult.Considered)
	}
}

// TestRunPreWorkGateFailingApplicabilityNeverReachesEligibleList proves
// M21-067: a recipe/atom candidate discovered through the ordinary
// structured-field channel, but whose declared applicability predicate does
// not hold for the current task (here: scoped to a different repository),
// is rejected by EvaluateApplicabilityPredicate and never appears in the
// presented Eligible list or influences planning -- regardless of how
// confidently it was discovered. (No embedding/similarity generation
// exists in this package; the discovery channel here is the same
// deterministic structured-field lookup M21-067 requires atoms/recipes to
// route through applicability for, never a similarity threshold.)
func TestRunPreWorkGateFailingApplicabilityNeverReachesEligibleList(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	_, recipeRevision := mustCreateExecutionRecipeArtifact(t, repositories, projectID, repositoryID)
	// The predicate demands a DIFFERENT repository than the task's own,
	// so it must fail regardless of the recipe's own declared textual
	// applicability statement or how strongly it would have been ranked.
	otherRepositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	mustCreateRepositoryMatchPredicate(t, repositories, recipeRevision, task.SchemaVersion, otherRepositoryID)

	queryID := newTestQueryID(t, "applicability")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Considered != 1 {
		t.Fatalf("considered = %d, want 1", result.Considered)
	}
	for _, item := range result.Eligible {
		if item.RevisionID == recipeRevision {
			t.Fatalf("recipe with a failing applicability predicate must never appear in Eligible: %#v", result.Eligible)
		}
	}
	if !result.FellBack {
		t.Fatal("expected a fallback: the only discovered candidate failed applicability")
	}

	candidateID := deriveCandidateID(queryID, recipeRevision)
	decision, found, err := repositories.GetMemoryRetrievalDecision(ctx, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected a decision to be recorded for candidate %s", candidateID)
	}
	if decision.Decision != "rejected" || string(decision.ReasonKind) != string(retrievalgate.RejectionReasonApplicabilityPredicateFailed) {
		t.Fatalf("decision = (%q, %q), want (rejected, applicability-predicate-failed)", decision.Decision, decision.ReasonKind)
	}
}

// TestRunPreWorkGateMergesExactIdentityAndStructuredFieldChannels proves the
// M21-137 storage gap is actually closed end-to-end: a memory artifact
// discovered through BOTH the M21-064 exact-identity channel (it is
// supported by a prior episode whose fingerprint hash matches the current
// task exactly) and the M21-065 structured-field channel (it is also a
// repository fact bound to the task's own repository) is logged as ONE
// candidate carrying BOTH channels, losslessly, never as two separate
// candidates and never collapsed down to a single channel.
func TestRunPreWorkGateMergesExactIdentityAndStructuredFieldChannels(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	artifactID, revisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go vet ./...")
	episodeID := mustCreateClosedEpisode(t, repositories, projectID, repositoryID, task)
	if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, artifactID, episodeID); err != nil {
		t.Fatal(err)
	}

	queryID := newTestQueryID(t, "merge")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Considered != 1 {
		t.Fatalf("considered = %d, want exactly 1 merged candidate (not one per discovery channel)", result.Considered)
	}
	if len(result.Eligible) != 1 {
		t.Fatalf("eligible = %#v, want exactly 1", result.Eligible)
	}
	if len(result.Eligible[0].Channels) != 2 {
		t.Fatalf("channels = %#v, want both exact-match and applicability-pass preserved", result.Eligible[0].Channels)
	}

	candidateID := deriveCandidateID(queryID, revisionID)
	storedChannels, err := repositories.ListMemoryRetrievalCandidateChannels(ctx, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedChannels) != 2 {
		t.Fatalf("stored channels = %#v, want both channels preserved losslessly in storage", storedChannels)
	}
}

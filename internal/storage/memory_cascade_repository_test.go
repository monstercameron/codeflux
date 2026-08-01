package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// cascadeChain builds artifacts[0] <- artifacts[1] <- ... via derived_from
// (each later artifact derives from the previous one) and returns them.
func cascadeChain(t *testing.T, repositories *Repositories, projectID domain.ProjectID, repositoryID domain.RepositoryID, seed int, length int) []domain.MemoryArtifactID {
	t.Helper()
	ctx := t.Context()
	artifacts := make([]domain.MemoryArtifactID, 0, length)
	for index := 0; index < length; index++ {
		artifacts = append(artifacts, createMemoryArtifactFixture(t, repositories, projectID, repositoryID, seed+index*3))
	}
	for index := 1; index < length; index++ {
		if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, artifacts[index], artifacts[index-1]); err != nil {
			t.Fatal(err)
		}
	}
	return artifacts
}

func maturityOf(t *testing.T, repositories *Repositories, artifactID domain.MemoryArtifactID) domain.MaturityState {
	t.Helper()
	revision, err := repositories.GetLatestMemoryArtifactRevision(t.Context(), artifactID)
	if err != nil {
		t.Fatal(err)
	}
	return revision.Maturity
}

// TestCascadeQuarantinesTransitiveDerivedFromDescendants is the M21-G04
// repair: a four-deep chain must quarantine every descendant, not just the
// first hop. Depth 3 is the case the previous implementation missed.
func TestCascadeQuarantinesTransitiveDerivedFromDescendants(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9600)
	repositoryID := testRepositoryID(t, 9601)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	chain := cascadeChain(t, repositories, projectID, repositoryID, 9602, 4)
	origin := chain[0]

	originRevision, err := repositories.GetLatestMemoryArtifactRevision(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: originRevision.RevisionID, From: domain.MaturityStateCandidate,
		To: domain.MaturityStateInvalidated, ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
		DetailRedacted: "cascade test: origin support changed", IdempotencyKey: "cascade-origin",
	}); err != nil {
		t.Fatal(err)
	}

	outcome, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: origin,
		ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.QuarantinedArtifacts) != 3 {
		t.Fatalf("QuarantinedArtifacts = %d, want 3 (every descendant at depth 1, 2 and 3)", len(outcome.QuarantinedArtifacts))
	}
	for depth, artifactID := range chain[1:] {
		if got := maturityOf(t, repositories, artifactID); got != domain.MaturityStateQuarantined {
			t.Fatalf("descendant at depth %d maturity = %s, want Quarantined", depth+1, got)
		}
	}
	// The origin keeps the caller's own reason; the cascade must not
	// re-transition it out of Invalidated.
	if got := maturityOf(t, repositories, origin); got != domain.MaturityStateInvalidated {
		t.Fatalf("origin maturity = %s, want Invalidated (cascade must not re-transition the origin)", got)
	}
}

// TestCascadeFlagsInfluencedByWithoutInvalidatingThem holds §31's
// distinction: semantic dependents are quarantined automatically,
// contextually influenced descendants are only flagged.
func TestCascadeFlagsInfluencedByWithoutInvalidatingThem(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9620)
	repositoryID := testRepositoryID(t, 9621)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	origin := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9622)
	semantic := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9625)
	contextual := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9628)
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, semantic, origin); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, contextual, origin); err != nil {
		t.Fatal(err)
	}

	outcome, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: origin,
		ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := maturityOf(t, repositories, semantic); got != domain.MaturityStateQuarantined {
		t.Fatalf("semantic dependent maturity = %s, want Quarantined", got)
	}
	if got := maturityOf(t, repositories, contextual); got != domain.MaturityStateCandidate {
		t.Fatalf("contextually influenced maturity = %s, want unchanged Candidate: §31 flags but never auto-invalidates contextual exposure", got)
	}
	if len(outcome.FlaggedInfluenced) != 1 || outcome.FlaggedInfluenced[0] != contextual {
		t.Fatalf("FlaggedInfluenced = %v, want exactly the contextually exposed artifact %s", outcome.FlaggedInfluenced, contextual)
	}
}

// TestCascadeTerminatesOnDerivedFromCycle proves the traversal is cycle
// safe. A derived_from cycle is legal storage-side (only direct
// self-reference is rejected), so an unguarded walk would not terminate.
func TestCascadeTerminatesOnDerivedFromCycle(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9640)
	repositoryID := testRepositoryID(t, 9641)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	chain := cascadeChain(t, repositories, projectID, repositoryID, 9642, 3)
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, chain[0], chain[2]); err != nil {
		t.Fatal(err)
	}

	outcome, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: chain[0],
		ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.QuarantinedArtifacts) != 2 {
		t.Fatalf("QuarantinedArtifacts = %d, want 2 (both descendants once, cycle visited once)", len(outcome.QuarantinedArtifacts))
	}
}

// TestCascadeSkipsAlreadyTerminalDescendants proves quarantine stays
// terminal: an already-quarantined descendant is reported, never
// re-transitioned (which the domain transition table would reject).
func TestCascadeSkipsAlreadyTerminalDescendants(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9660)
	repositoryID := testRepositoryID(t, 9661)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	chain := cascadeChain(t, repositories, projectID, repositoryID, 9662, 3)
	middleRevision, err := repositories.GetLatestMemoryArtifactRevision(ctx, chain[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: middleRevision.RevisionID, From: domain.MaturityStateCandidate,
		To: domain.MaturityStateQuarantined, ReasonKind: MemoryArtifactInvalidationReasonOther,
		DetailRedacted: "already quarantined before the cascade", IdempotencyKey: "cascade-pre-quarantine",
	}); err != nil {
		t.Fatal(err)
	}

	outcome, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: chain[0],
		ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.AlreadyTerminal) != 1 || outcome.AlreadyTerminal[0] != chain[1] {
		t.Fatalf("AlreadyTerminal = %v, want exactly the pre-quarantined artifact", outcome.AlreadyTerminal)
	}
	// The deeper descendant must still be reached through the terminal one.
	if got := maturityOf(t, repositories, chain[2]); got != domain.MaturityStateQuarantined {
		t.Fatalf("descendant beyond an already-terminal node maturity = %s, want Quarantined", got)
	}
}

// TestCascadeIsIdempotentOnRetry proves a retried cascade reuses each
// descendant's original transition instead of failing on a duplicate key.
func TestCascadeIsIdempotentOnRetry(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9680)
	repositoryID := testRepositoryID(t, 9681)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	chain := cascadeChain(t, repositories, projectID, repositoryID, 9682, 3)
	request := CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: chain[0],
		ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
	}
	first, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, request)
	if err != nil {
		t.Fatalf("retry must not fail: %v", err)
	}
	if len(first.QuarantinedArtifacts) != 2 {
		t.Fatalf("first cascade quarantined %d, want 2", len(first.QuarantinedArtifacts))
	}
	if len(second.QuarantinedArtifacts) != 0 || len(second.AlreadyTerminal) != 2 {
		t.Fatalf("retry quarantined=%d alreadyTerminal=%d, want 0 and 2", len(second.QuarantinedArtifacts), len(second.AlreadyTerminal))
	}
}

// TestCascadeReportsAssuranceOwnerReviewPastFivePercent holds §31's bulk
// bound: quarantine still happens, but crossing five percent of active
// artifacts flags that permanent bulk invalidation needs owner review.
func TestCascadeReportsAssuranceOwnerReviewPastFivePercent(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9700)
	repositoryID := testRepositoryID(t, 9701)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	// 3 artifacts total, 2 of them descendants: 2/3 is far past 5%.
	chain := cascadeChain(t, repositories, projectID, repositoryID, 9702, 3)
	outcome, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: chain[0],
		ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.RequiresAssuranceOwnerReview {
		t.Fatalf("RequiresAssuranceOwnerReview = false with %d of %d active artifacts quarantined, want true past 5%%",
			len(outcome.QuarantinedArtifacts), outcome.ActiveArtifactCount)
	}
	// Quarantine still occurred; the flag reports, it does not block.
	if got := maturityOf(t, repositories, chain[1]); got != domain.MaturityStateQuarantined {
		t.Fatalf("descendant maturity = %s, want Quarantined: §31 says quarantine still occurs past the threshold", got)
	}
}

// TestCascadeFailsLoudlyPastItsBound proves a runaway lineage errors rather
// than silently truncating the cascade and leaving descendants standing.
func TestCascadeFailsLoudlyPastItsBound(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9720)
	repositoryID := testRepositoryID(t, 9721)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	chain := cascadeChain(t, repositories, projectID, repositoryID, 9722, 4)
	_, err := repositories.cascadeMemoryArtifactInvalidationBounded(t.Context(), CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: chain[0],
		ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
	}, 2)
	if !errors.Is(err, ErrMemoryArtifactLineageTooLarge) {
		t.Fatalf("error = %v, want ErrMemoryArtifactLineageTooLarge", err)
	}
}

// TestCascadeRejectsUndeclaredReason keeps the invalidation-reason
// requirement from being bypassed through the cascade entry point.
func TestCascadeRejectsUndeclaredReason(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9740)
	repositoryID := testRepositoryID(t, 9741)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	origin := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9742)
	if _, err := repositories.CascadeMemoryArtifactInvalidationTransitively(t.Context(), CascadeMemoryArtifactInvalidation{
		ProjectID: projectID, OriginArtifactID: origin,
	}); err == nil {
		t.Fatal("cascade with no declared invalidation reason must be rejected")
	}
}

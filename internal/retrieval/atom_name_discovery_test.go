package retrieval

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
)

// TestDiscoverByAtomNameRecordsCanonicalNormalizedAndAliasMatchedText proves
// M21-167 ("search canonical name, normalized phrase, and active aliases
// before vector similarity") and M21-168 ("record which name or alias caused
// an atom to enter the candidate set") together: an ExecutableAtomReference
// candidate matched by its exact canonical name is attributed under the
// exact-match channel with the matched canonical name text; a second
// candidate matched only through a reviewed alias is attributed under the
// applicability-pass channel with the matched alias text -- so a reviewer
// can see WHY each candidate appeared, not merely which channel found it.
func TestDiscoverByAtomNameRecordsCanonicalNormalizedAndAliasMatchedText(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)

	canonicalAtomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatal(err)
	}
	canonical := mustCanonicalNameFixture(t, "ReserveAccountFundsUntilAuthorizationExpires")
	mustCreateAtomNameFixture(t, repositories, projectID, canonicalAtomID, canonical)
	_, canonicalRevisionID := mustCreateExecutableAtomReferenceArtifact(
		t, repositories, projectID, repositoryID, canonicalAtomID, canonical.String(),
	)

	aliasAtomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatal(err)
	}
	aliasCanonical := mustCanonicalNameFixture(t, "ReleaseWidgetInventoryHold")
	mustCreateAtomNameFixture(t, repositories, projectID, aliasAtomID, aliasCanonical)
	mustCreateAtomAliasFixture(t, repositories, aliasAtomID, "hold funds pending authorization")
	_, aliasRevisionID := mustCreateExecutableAtomReferenceArtifact(
		t, repositories, projectID, repositoryID, aliasAtomID, aliasCanonical.String(),
	)

	task, err := fingerprint.BuildExactFingerprint(fingerprint.ExactFingerprintInput{
		Project: projectID, Repository: repositoryID,
		BaseRevision:      domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
		TaskClass:         fingerprint.TaskClassBugFix,
		Risk:              domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		// "ReserveAccountFundsUntilAuthorizationExpires" matches the first
		// atom's canonical name exactly; "HoldFundsPendingAuthorization"
		// word-splits and normalizes to the same phrase as the second
		// atom's reviewed alias, but matches neither atom's canonical name.
		AffectedSymbols: []string{"ReserveAccountFundsUntilAuthorizationExpires", "HoldFundsPendingAuthorization"},
	})
	if err != nil {
		t.Fatal(err)
	}

	queryID := newTestQueryID(t, "atom-name")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FellBack || len(result.Eligible) != 2 {
		t.Fatalf("result = %#v, want both atom-reference candidates eligible", result)
	}

	byRevision := map[domain.MemoryArtifactRevisionID]InfluentialMemoryItem{}
	for _, item := range result.Eligible {
		byRevision[item.RevisionID] = item
	}

	canonicalItem, ok := byRevision[canonicalRevisionID]
	if !ok {
		t.Fatalf("expected the canonical-name-matched candidate %v among %#v", canonicalRevisionID, result.Eligible)
	}
	if !containsChannel(canonicalItem.Channels, storage.RetrievalCandidateExactMatch) {
		t.Fatalf("canonical item channels = %#v, want exact-match present", canonicalItem.Channels)
	}
	if got := canonicalItem.MatchedText[storage.RetrievalCandidateExactMatch]; got != canonical.String() {
		t.Fatalf("canonical item matched text = %q, want %q", got, canonical.String())
	}

	aliasItem, ok := byRevision[aliasRevisionID]
	if !ok {
		t.Fatalf("expected the alias-matched candidate %v among %#v", aliasRevisionID, result.Eligible)
	}
	if !containsChannel(aliasItem.Channels, storage.RetrievalCandidateApplicabilityPass) {
		t.Fatalf("alias item channels = %#v, want applicability-pass present", aliasItem.Channels)
	}
	if got := aliasItem.MatchedText[storage.RetrievalCandidateApplicabilityPass]; got != "hold funds pending authorization" {
		t.Fatalf("alias item matched text = %q, want the matched alias text", got)
	}
	// The alias must never be attributed under exact-match: an alias is a
	// structured-field discovery, never treated as if it were the
	// candidate's own exact canonical identity.
	if _, exactMatched := aliasItem.MatchedText[storage.RetrievalCandidateExactMatch]; exactMatched {
		t.Fatalf("alias item must not carry an exact-match attribution: %#v", aliasItem.MatchedText)
	}
}

func containsChannel(channels []storage.MemoryRetrievalCandidateSource, want storage.MemoryRetrievalCandidateSource) bool {
	for _, channel := range channels {
		if channel == want {
			return true
		}
	}
	return false
}

// TestDiscoverCandidatesOnlyAddsSimilarityAfterNameAndStructuredChannels
// proves M21-167's ordering requirement: "exact and structured name
// channels are consulted first, and a similarity channel (when one ever
// exists) may only ADD candidates afterward." No similarity channel exists
// in production (similarityHook is nil in every NewService); this test
// injects a test-only stand-in, unreachable from any production code path
// because the field is unexported, purely to prove discoverCandidates' call
// order and merge behavior structurally rather than by convention.
func TestDiscoverCandidatesOnlyAddsSimilarityAfterNameAndStructuredChannels(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)

	_, structuredRevisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./...")
	_, similarityOnlyRevisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go vet ./... (similarity-only)")
	similarityOnlyRevision, err := repositories.GetMemoryArtifactRevision(ctx, similarityOnlyRevisionID)
	if err != nil {
		t.Fatal(err)
	}

	var callOrder []string
	service.similarityHook = func(ctx context.Context, input PreWorkGateInput) ([]similarityDiscovery, error) {
		callOrder = append(callOrder, "similarity")
		return []similarityDiscovery{{Revision: similarityOnlyRevision, Score: 0.42}}, nil
	}

	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)
	order, index, err := service.discoverCandidates(ctx, PreWorkGateInput{
		QueryID: "ordering-query", ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(callOrder) != 1 || callOrder[0] != "similarity" {
		t.Fatalf("callOrder = %#v, want exactly one similarity-hook invocation", callOrder)
	}
	if len(order) != 2 {
		t.Fatalf("order = %v, want exactly 2 discovered candidates", order)
	}
	if order[0] != structuredRevisionID {
		t.Fatalf("order[0] = %v, want the structured-field candidate ranked ahead of the similarity-only candidate", order[0])
	}
	if order[1] != similarityOnlyRevisionID {
		t.Fatalf("order[1] = %v, want the similarity-only candidate strictly last", order[1])
	}
	if _, structured := index[structuredRevisionID].channels[storage.RetrievalCandidateApplicabilityPass]; !structured {
		t.Fatal("expected the structured candidate's own channel to remain applicability-pass, undisturbed by the later similarity phase")
	}
	similarityEntry := index[similarityOnlyRevisionID]
	if _, viaSimilarity := similarityEntry.channels[storage.RetrievalCandidateVectorSimilarity]; !viaSimilarity {
		t.Fatalf("similarity-only candidate channels = %#v, want vector-similarity", similarityEntry.channels)
	}
	if similarityEntry.similarityScore == nil || *similarityEntry.similarityScore != 0.42 {
		t.Fatalf("similarity score = %#v, want 0.42 carried through", similarityEntry.similarityScore)
	}
}

// TestRunPreWorkGateEligibleButNeverActedOnNeverRecordsInfluence proves the
// central §31 distinction from the opposite direction of
// TestRunPreWorkGateRetrievedButRejectedNeverRecordsInfluence: a candidate
// that passed every eligibility gate and IS presented in result.Eligible,
// but on which the caller never calls RecordInfluence, has no decision row
// at all -- eligibility alone is never silently treated as influence. This
// is exactly what a "retrieved but unused" item looks like end-to-end.
func TestRunPreWorkGateEligibleButNeverActedOnNeverRecordsInfluence(t *testing.T) {
	ctx := context.Background()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	_, revisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go test ./... (never acted on)")

	queryID := newTestQueryID(t, "unused")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FellBack || len(result.Eligible) != 1 || result.Eligible[0].RevisionID != revisionID {
		t.Fatalf("result = %#v, want exactly one eligible, non-fallback candidate", result)
	}

	// Deliberately never call service.RecordInfluence. The candidate was
	// retrieved and evaluated eligible; it must not read as influential.
	candidateID := deriveCandidateID(queryID, revisionID)
	_, found, err := repositories.GetMemoryRetrievalDecision(ctx, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("an eligible candidate the caller never acted on must have no recorded decision at all -- retrieval must never silently become influence")
	}
}

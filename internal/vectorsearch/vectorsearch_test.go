package vectorsearch

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func mustProject(t *testing.T) domain.ProjectID {
	t.Helper()
	value, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustRevision(t *testing.T) domain.MemoryArtifactRevisionID {
	t.Helper()
	value, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestM21_079_ProviderSelectionRefusedUntilMeasuredMissesExist is the §0
// branch gate in executable form: with no reviewed fallback there is no
// measurement, and the branch stays closed.
func TestM21_079_ProviderSelectionRefusedUntilMeasuredMissesExist(t *testing.T) {
	cases := []struct {
		name        string
		measurement RecallMeasurement
		wantErr     bool
	}{
		{
			name:        "no traffic at all",
			measurement: RecallMeasurement{},
			wantErr:     true,
		},
		{
			name:        "fallbacks recorded but none reviewed",
			measurement: RecallMeasurement{QueriesInWindow: 40, FallbacksInWindow: 12},
			wantErr:     true,
		},
		{
			name:        "reviewed but no genuine miss found",
			measurement: RecallMeasurement{QueriesInWindow: 40, FallbacksInWindow: 12, ReviewedFallbacksInWindow: 12},
			wantErr:     true,
		},
		{
			name: "a reviewed genuine miss exists",
			measurement: RecallMeasurement{
				QueriesInWindow: 40, FallbacksInWindow: 12,
				ReviewedFallbacksInWindow: 12, GenuineMissesInWindow: 3,
			},
			wantErr: false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := AuthorizeEmbeddingProviderSelection(testCase.measurement)
			if testCase.wantErr {
				if !errors.Is(err, ErrEmbeddingBranchNotJustified) {
					t.Fatalf("err = %v, want ErrEmbeddingBranchNotJustified", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil once a measured genuine miss exists", err)
			}
		})
	}
}

// TestM21_084_ProjectScopeIsAppliedBeforeRanking proves an out-of-project
// vector cannot occupy a rank slot even when it is the closest match.
// Filtering after ranking would let foreign data displace legitimate
// candidates; this asserts it never enters the ordering at all.
func TestM21_084_ProjectScopeIsAppliedBeforeRanking(t *testing.T) {
	ownProject := mustProject(t)
	foreignProject := mustProject(t)
	query := []float32{1, 0, 0}

	foreign := Candidate{RevisionID: mustRevision(t), Project: foreignProject, Vector: []float32{1, 0, 0}}
	own := Candidate{RevisionID: mustRevision(t), Project: ownProject, Vector: []float32{0.6, 0.8, 0}}

	ranked, err := Rank(query, []Candidate{foreign, own}, domain.MemoryQueryProjectBoundary{Project: ownProject}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 {
		t.Fatalf("ranked = %d, want only the in-project candidate", len(ranked))
	}
	if ranked[0].RevisionID != own.RevisionID {
		t.Fatalf("ranked revision = %s, want the in-project candidate %s", ranked[0].RevisionID, own.RevisionID)
	}
	if ranked[0].Rank != 1 {
		t.Fatalf("rank = %d, want 1", ranked[0].Rank)
	}
}

// TestM21_083_CosineRankingOrdersByCloseness checks the search itself.
func TestM21_083_CosineRankingOrdersByCloseness(t *testing.T) {
	project := mustProject(t)
	query := []float32{1, 0}
	near := Candidate{RevisionID: mustRevision(t), Project: project, Vector: []float32{0.99, 0.14}}
	far := Candidate{RevisionID: mustRevision(t), Project: project, Vector: []float32{0, 1}}

	ranked, err := Rank(query, []Candidate{far, near}, domain.MemoryQueryProjectBoundary{Project: project}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 2 {
		t.Fatalf("ranked = %d, want 2", len(ranked))
	}
	if ranked[0].RevisionID != near.RevisionID {
		t.Fatal("the nearer vector must rank first")
	}
	if ranked[0].Similarity <= ranked[1].Similarity {
		t.Fatalf("similarities not descending: %v then %v", ranked[0].Similarity, ranked[1].Similarity)
	}
	if ranked[1].Rank != 2 {
		t.Fatalf("second rank = %d, want 2", ranked[1].Rank)
	}
}

// TestRankIsDeterministicForEqualSimilarities proves ties never reorder
// between runs, so a recorded candidate rank means the same thing later.
func TestRankIsDeterministicForEqualSimilarities(t *testing.T) {
	project := mustProject(t)
	query := []float32{1, 0}
	first := Candidate{RevisionID: mustRevision(t), Project: project, Vector: []float32{1, 0}}
	second := Candidate{RevisionID: mustRevision(t), Project: project, Vector: []float32{1, 0}}

	var previous string
	for attempt := 0; attempt < 25; attempt++ {
		ranked, err := Rank(query, []Candidate{first, second}, domain.MemoryQueryProjectBoundary{Project: project}, 10)
		if err != nil {
			t.Fatal(err)
		}
		order := ranked[0].RevisionID.String() + "|" + ranked[1].RevisionID.String()
		if previous != "" && order != previous {
			t.Fatalf("tie ordering varied: %q then %q", previous, order)
		}
		previous = order
	}
}

// TestRankRejectsMismatchedDimensionsRatherThanFabricatingSimilarity proves a
// vector from a different embedding space is excluded, not coerced.
func TestRankRejectsMismatchedDimensionsRatherThanFabricatingSimilarity(t *testing.T) {
	project := mustProject(t)
	wrongSpace := Candidate{RevisionID: mustRevision(t), Project: project, Vector: []float32{1, 0, 0, 0}}
	ranked, err := Rank([]float32{1, 0}, []Candidate{wrongSpace}, domain.MemoryQueryProjectBoundary{Project: project}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 0 {
		t.Fatalf("ranked = %#v, want a dimension-mismatched candidate excluded", ranked)
	}
}

// TestRankFailsLoudlyPastThePrototypeBound proves brute force does not
// silently degrade past the scale it was designed for.
func TestRankFailsLoudlyPastThePrototypeBound(t *testing.T) {
	project := mustProject(t)
	candidates := make([]Candidate, MaximumRankedCandidates+1)
	for index := range candidates {
		candidates[index] = Candidate{RevisionID: mustRevision(t), Project: project, Vector: []float32{1, 0}}
	}
	if _, err := Rank([]float32{1, 0}, candidates, domain.MemoryQueryProjectBoundary{Project: project}, 10); err == nil {
		t.Fatal("a candidate set past the brute-force bound must error")
	}
}

// TestRankRejectsAZeroValueBoundary keeps scoping fail-closed, matching
// domain.MemoryQueryProjectBoundary's own behaviour.
func TestRankRejectsAZeroValueBoundary(t *testing.T) {
	if _, err := Rank([]float32{1, 0}, nil, domain.MemoryQueryProjectBoundary{}, 10); err == nil {
		t.Fatal("a zero-value project boundary must be rejected")
	}
}

// TestCosineSimilarityBoundsAndUndefinedCases covers the arithmetic itself.
func TestCosineSimilarityBoundsAndUndefinedCases(t *testing.T) {
	same, err := CosineSimilarity([]float32{1, 2, 3}, []float32{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(same-1) > 1e-9 {
		t.Fatalf("identical vectors similarity = %v, want 1", same)
	}
	opposite, err := CosineSimilarity([]float32{1, 0}, []float32{-1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(opposite+1) > 1e-9 {
		t.Fatalf("opposite vectors similarity = %v, want -1", opposite)
	}
	if _, err := CosineSimilarity([]float32{0, 0}, []float32{1, 0}); err == nil {
		t.Fatal("a zero-magnitude vector has no direction and must error, not report 0")
	}
	if _, err := CosineSimilarity([]float32{1, 0}, []float32{1, 0, 0}); err == nil {
		t.Fatal("mismatched lengths must error")
	}
}

// TestRankedCandidateCarriesNoEligibilitySignal is the structural half of
// §0's `vector similarity -> applicability or authority` prohibition: a
// ranked result names a candidate and its closeness, and carries no field
// that could be mistaken for permission.
func TestRankedCandidateCarriesNoEligibilitySignal(t *testing.T) {
	forbidden := []string{"Assurance", "Maturity", "Eligible", "Applicability", "Evidence", "Approved", "Authorized"}
	rankedType := reflectTypeOfRankedCandidate()
	for index := 0; index < rankedType.NumField(); index++ {
		name := rankedType.Field(index).Name
		for _, banned := range forbidden {
			if name == banned {
				t.Fatalf("RankedCandidate.%s would let a similarity result carry an eligibility signal", name)
			}
		}
	}
}

// reflectTypeOfRankedCandidate isolates the reflection import to one place.
func reflectTypeOfRankedCandidate() reflect.Type {
	return reflect.TypeOf(RankedCandidate{})
}

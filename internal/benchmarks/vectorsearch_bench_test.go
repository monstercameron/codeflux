package benchmarks

import (
	"math"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/vectorsearch"
)

// prototypeCorpusSize is the scale docs/plan.md expects a local prototype to
// reach: a single developer's project memory, not a hosted index. Ranking is
// bounded at vectorsearch.MaximumRankedCandidates, so the corpus is sized
// against that bound rather than against an arbitrary round number.
const prototypeCorpusSize = vectorsearch.MaximumRankedCandidates

// BenchmarkVectorSearchAtPrototypeScale is M22-087.
//
// It is registered unconditionally even though embedding discovery is an
// opt-in branch: the plan requires the cost be known BEFORE the branch is
// justified, because "we will measure it if we turn it on" is how an
// unaffordable feature gets shipped.
func BenchmarkVectorSearchAtPrototypeScale(b *testing.B) {
	LogEnvironment(b)
	project, err := domain.NewProjectID()
	if err != nil {
		b.Fatalf("new project ID: %v", err)
	}

	const dimensions = 384
	candidates := make([]vectorsearch.Candidate, 0, prototypeCorpusSize)
	for index := range prototypeCorpusSize {
		revisionID, revisionErr := domain.NewMemoryArtifactRevisionID()
		if revisionErr != nil {
			b.Fatalf("new revision ID: %v", revisionErr)
		}
		candidates = append(candidates, vectorsearch.Candidate{
			RevisionID: revisionID,
			Project:    project,
			Vector:     syntheticUnitVector(dimensions, index),
		})
	}
	query := syntheticUnitVector(dimensions, prototypeCorpusSize/2)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}

	var ranked []vectorsearch.RankedCandidate
	Measure(b, nil, func() {
		result, rankErr := vectorsearch.Rank(query, candidates, boundary, 10)
		if rankErr != nil {
			b.Fatalf("rank candidates: %v", rankErr)
		}
		ranked = result
	})
	if len(ranked) != 10 {
		b.Fatalf("ranking returned %d candidates, want 10", len(ranked))
	}
	b.ReportMetric(float64(len(candidates)), "corpus")
	b.ReportMetric(float64(dimensions), "dimensions")
}

// syntheticUnitVector produces a deterministic unit-length vector. It is
// deterministic so a benchmark rerun compares against the same corpus, and
// unit-length because Rank's cosine similarity is only meaningful for
// normalized input.
func syntheticUnitVector(dimensions, seed int) []float32 {
	vector := make([]float32, dimensions)
	var sumSquares float64
	for index := range dimensions {
		// A cheap deterministic spread; the exact distribution does not matter
		// to the cost, only that the vectors differ from one another.
		value := math.Sin(float64((seed+1)*(index+1)) * 0.001)
		vector[index] = float32(value)
		sumSquares += value * value
	}
	norm := math.Sqrt(sumSquares)
	if norm == 0 {
		vector[0] = 1
		return vector
	}
	for index := range vector {
		vector[index] = float32(float64(vector[index]) / norm)
	}
	return vector
}

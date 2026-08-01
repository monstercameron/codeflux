// Package vectorsearch is the prototype-scale vector candidate-discovery
// mechanism (M21-079, 083, 084, 086).
//
// It is deliberately built and left CLOSED. docs/plan.md §0 keeps vector
// discovery off "unless deterministic retrieval has a measured recall
// problem", and M21-078's instrument is what measures that. Nothing here
// selects a provider, generates an embedding, or reaches a network:
// AuthorizeEmbeddingProviderSelection refuses every selection until a real
// measurement justifies it, and Rank operates only on vectors a caller
// already holds.
//
// The separation this package must never break: similarity DISCOVERS
// candidates, it never establishes eligibility. §0 prohibits the dependency
// `vector similarity -> applicability or authority`, so Rank returns ranked
// candidates and nothing resembling a verdict. internal/retrievalgate
// decides what may actually be used, and its own structural tests prove no
// eligibility input can carry a similarity score.
package vectorsearch

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"codeflux.dev/codeflux/internal/domain"
)

// MaximumRankedCandidates bounds one ranking pass. AGENTS.md forbids
// unbounded reads and queries; brute-force scan is only acceptable at
// prototype scale with an explicit ceiling.
const MaximumRankedCandidates = 2000

// MinimumJustifyingGenuineMisses is the smallest number of human-reviewed
// genuine misses that could justify opening the vector branch. It is a
// floor, not a trigger: §0 requires a deliberate decision, and
// AuthorizeEmbeddingProviderSelection never opens the branch on its own.
const MinimumJustifyingGenuineMisses = 1

// ErrEmbeddingBranchNotJustified reports that vector discovery may not be
// enabled because deterministic retrieval has not shown a measured recall
// problem.
var ErrEmbeddingBranchNotJustified = errors.New("vector discovery branch is not justified by measured deterministic-retrieval recall")

// RecallMeasurement is the subset of M21-078's measurement that bears on the
// §0 branch decision. It is a plain value so this package never imports
// storage, keeping the decision testable in isolation.
type RecallMeasurement struct {
	QueriesInWindow           int
	FallbacksInWindow         int
	ReviewedFallbacksInWindow int
	GenuineMissesInWindow     int
}

// AuthorizeEmbeddingProviderSelection implements M21-079: "select an
// embedding provider/model ONLY IF the measured problem justifies it."
//
// It is a refusal by default. With no reviewed fallbacks there is no
// measurement, and an unmeasured branch stays closed — §0's "not yet
// justified means stop, not add more design prose". Returning nil does not
// enable anything; it only records that the precondition a human decision
// would need is now present.
func AuthorizeEmbeddingProviderSelection(measurement RecallMeasurement) error {
	if measurement.ReviewedFallbacksInWindow <= 0 {
		return fmt.Errorf(
			"%w: no fallback has been reviewed yet, so deterministic retrieval has no measured miss rate",
			ErrEmbeddingBranchNotJustified,
		)
	}
	if measurement.GenuineMissesInWindow < MinimumJustifyingGenuineMisses {
		return fmt.Errorf(
			"%w: %d reviewed fallbacks recorded %d genuine misses",
			ErrEmbeddingBranchNotJustified,
			measurement.ReviewedFallbacksInWindow,
			measurement.GenuineMissesInWindow,
		)
	}
	return nil
}

// Candidate is one stored vector offered for ranking, carrying only the
// identity needed to name it and the project scope needed to exclude it.
//
// It has no assurance, maturity, applicability, or evidence field by
// construction, so a ranked result cannot be mistaken for an eligible one.
type Candidate struct {
	RevisionID domain.MemoryArtifactRevisionID
	Project    domain.ProjectID
	Vector     []float32
}

// RankedCandidate is a Candidate with its similarity to the query and its
// rank within the scoped result set (M21-086's "candidate rank").
type RankedCandidate struct {
	RevisionID domain.MemoryArtifactRevisionID
	Similarity float64
	Rank       int
}

// Rank implements M21-083 (brute-force cosine search at prototype scale)
// and M21-084 (apply project scope BEFORE similarity ranking).
//
// Scope is applied first and unconditionally: a candidate outside boundary
// is removed before any similarity is computed, so an out-of-project vector
// can never occupy a rank slot or influence ordering. That ordering is the
// whole point — filtering after ranking would let foreign data displace
// legitimate candidates even when it is ultimately discarded.
func Rank(
	query []float32,
	candidates []Candidate,
	boundary domain.MemoryQueryProjectBoundary,
	limit int,
) ([]RankedCandidate, error) {
	if len(query) == 0 {
		return nil, errors.New("query vector must not be empty")
	}
	if err := boundary.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaximumRankedCandidates {
		limit = MaximumRankedCandidates
	}
	if len(candidates) > MaximumRankedCandidates {
		return nil, fmt.Errorf(
			"candidate set of %d exceeds the prototype-scale brute-force bound of %d",
			len(candidates), MaximumRankedCandidates,
		)
	}

	scoped := make([]RankedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		// M21-084: project scope first, before any similarity is computed.
		if !boundary.Allows(domain.MemoryProjectScope{Project: candidate.Project}) {
			continue
		}
		if len(candidate.Vector) != len(query) {
			// A dimension mismatch means the candidate came from a
			// different embedding space. Silently coercing it would
			// fabricate a similarity, so it is excluded.
			continue
		}
		similarity, err := CosineSimilarity(query, candidate.Vector)
		if err != nil {
			continue
		}
		scoped = append(scoped, RankedCandidate{
			RevisionID: candidate.RevisionID,
			Similarity: similarity,
		})
	}

	// Deterministic ordering: similarity descending, then revision ID, so
	// equal scores never reorder between runs.
	sort.SliceStable(scoped, func(first int, second int) bool {
		if scoped[first].Similarity != scoped[second].Similarity {
			return scoped[first].Similarity > scoped[second].Similarity
		}
		return scoped[first].RevisionID.String() < scoped[second].RevisionID.String()
	})
	if len(scoped) > limit {
		scoped = scoped[:limit]
	}
	for index := range scoped {
		scoped[index].Rank = index + 1
	}
	return scoped, nil
}

// CosineSimilarity returns the cosine of the angle between two equal-length
// vectors, in [-1, 1]. A zero-magnitude vector has no direction, so it
// errors rather than reporting a similarity of zero, which would read as
// "unrelated" rather than "undefined".
func CosineSimilarity(first []float32, second []float32) (float64, error) {
	if len(first) != len(second) {
		return 0, fmt.Errorf("vector lengths differ: %d and %d", len(first), len(second))
	}
	if len(first) == 0 {
		return 0, errors.New("vectors must not be empty")
	}
	var dot, firstMagnitude, secondMagnitude float64
	for index := range first {
		a := float64(first[index])
		b := float64(second[index])
		dot += a * b
		firstMagnitude += a * a
		secondMagnitude += b * b
	}
	if firstMagnitude == 0 || secondMagnitude == 0 {
		return 0, errors.New("a zero-magnitude vector has no direction to compare")
	}
	return dot / (math.Sqrt(firstMagnitude) * math.Sqrt(secondMagnitude)), nil
}

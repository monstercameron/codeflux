package vectorsearch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func justifiedMeasurement() RecallMeasurement {
	return RecallMeasurement{
		QueriesInWindow: 40, FallbacksInWindow: 12,
		ReviewedFallbacksInWindow: 12, GenuineMissesInWindow: 3,
	}
}

// TestM21_081_EmbeddingsAreNotGeneratedBeforeTheBranchIsJustified proves
// generation itself is gated, not just provider selection. Vectors made
// before deterministic retrieval showed a measured miss are work the plan
// says not to do yet.
func TestM21_081_EmbeddingsAreNotGeneratedBeforeTheBranchIsJustified(t *testing.T) {
	_, err := GenerateEmbeddings(
		context.Background(), DeterministicEmbedder{Dimensions: 8},
		RecallMeasurement{}, []string{"reserve funds against an account"},
	)
	if !errors.Is(err, ErrEmbeddingBranchNotJustified) {
		t.Fatalf("err = %v, want ErrEmbeddingBranchNotJustified", err)
	}
}

// TestM21_081_GeneratesOneTraceableVectorPerScrubbedSegment is the M21-081
// happy path: scrubbed descriptive text in, vectors out, each carrying the
// identity needed to trace it (M21-G06's five bindings begin here).
func TestM21_081_GeneratesOneTraceableVectorPerScrubbedSegment(t *testing.T) {
	segments := []string{
		"Reserves an amount against an account without capturing it.",
		"Use when authorization must be held before settlement.",
	}
	generated, err := GenerateEmbeddings(
		context.Background(), DeterministicEmbedder{Dimensions: 16},
		justifiedMeasurement(), segments,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) != len(segments) {
		t.Fatalf("generated %d vectors for %d segments", len(generated), len(segments))
	}
	for index, embedding := range generated {
		if len(embedding.Vector) != 16 {
			t.Fatalf("segment %d: %d dimensions, want 16", index, len(embedding.Vector))
		}
		if err := embedding.Model.Validate(); err != nil {
			t.Fatalf("segment %d: model identity is not traceable: %v", index, err)
		}
		if embedding.Model.Provider == "" || embedding.Model.ModelVersion == "" {
			t.Fatalf("segment %d: every vector must name what produced it", index)
		}
	}
	if generated[0].Vector[0] == generated[1].Vector[0] && generated[0].Vector[1] == generated[1].Vector[1] {
		t.Fatal("distinct segments must not produce an identical vector prefix")
	}
}

// TestM21_081_GenerationIsDeterministic proves the same text always yields
// the same vector, so a stored vector stays comparable across runs.
func TestM21_081_GenerationIsDeterministic(t *testing.T) {
	segments := []string{"derive a stable idempotency key for a retried charge"}
	first, err := GenerateEmbeddings(context.Background(), DeterministicEmbedder{Dimensions: 12}, justifiedMeasurement(), segments)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		again, err := GenerateEmbeddings(context.Background(), DeterministicEmbedder{Dimensions: 12}, justifiedMeasurement(), segments)
		if err != nil {
			t.Fatal(err)
		}
		for index := range first[0].Vector {
			if first[0].Vector[index] != again[0].Vector[index] {
				t.Fatalf("vector drifted at dimension %d: %v then %v", index, first[0].Vector[index], again[0].Vector[index])
			}
		}
	}
}

// dishonestEmbedder claims L2 normalization and does not deliver it.
type dishonestEmbedder struct{}

func (dishonestEmbedder) Identity() EmbeddingModelIdentity {
	return EmbeddingModelIdentity{
		Provider: "fixture", ModelName: "dishonest", ModelVersion: "1",
		Dimensions: 3, Normalization: "l2",
	}
}

func (dishonestEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{5, 5, 5}}, nil
}

// TestM21_081_ANormalizationClaimIsVerifiedNotTrusted proves a model that
// declares L2 normalization is checked, not believed. An unnormalized vector
// silently accepted would corrupt every cosine comparison against it.
func TestM21_081_ANormalizationClaimIsVerifiedNotTrusted(t *testing.T) {
	if _, err := GenerateEmbeddings(
		context.Background(), dishonestEmbedder{}, justifiedMeasurement(), []string{"anything"},
	); err == nil {
		t.Fatal("a false l2-normalization claim must be rejected")
	}
}

// mismatchedEmbedder returns a different width than its identity declares.
type mismatchedEmbedder struct{}

func (mismatchedEmbedder) Identity() EmbeddingModelIdentity {
	return EmbeddingModelIdentity{
		Provider: "fixture", ModelName: "mismatched", ModelVersion: "1",
		Dimensions: 8, Normalization: "none",
	}
}

func (mismatchedEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 2}}, nil
}

// TestM21_081_DimensionMismatchIsRejected keeps a vector from being stored
// against an embedding space it does not belong to.
func TestM21_081_DimensionMismatchIsRejected(t *testing.T) {
	if _, err := GenerateEmbeddings(
		context.Background(), mismatchedEmbedder{}, justifiedMeasurement(), []string{"anything"},
	); err == nil {
		t.Fatal("a dimension mismatch between identity and output must be rejected")
	}
}

// TestM21_081_InputIsBoundedAndNonBlank keeps unbounded or empty text from
// reaching an embedder.
func TestM21_081_InputIsBoundedAndNonBlank(t *testing.T) {
	embedder := DeterministicEmbedder{Dimensions: 8}
	if _, err := GenerateEmbeddings(context.Background(), embedder, justifiedMeasurement(), nil); err == nil {
		t.Fatal("empty input must be rejected")
	}
	if _, err := GenerateEmbeddings(context.Background(), embedder, justifiedMeasurement(), []string{"  "}); err == nil {
		t.Fatal("blank segments must be rejected")
	}
	huge := strings.Repeat("x", MaximumEmbeddingInputBytes+1)
	if _, err := GenerateEmbeddings(context.Background(), embedder, justifiedMeasurement(), []string{huge}); err == nil {
		t.Fatal("input past the byte bound must be rejected")
	}
}

// TestDeterministicEmbedderNamesItselfHonestly keeps a local hash vector
// from being mistaken for a semantic embedding in stored provenance.
func TestDeterministicEmbedderNamesItselfHonestly(t *testing.T) {
	identity := DeterministicEmbedder{}.Identity()
	if !strings.Contains(identity.ModelName, "not-semantic") {
		t.Fatalf("model name %q must say plainly that it is not a semantic model", identity.ModelName)
	}
	if err := identity.Validate(); err != nil {
		t.Fatal(err)
	}
}

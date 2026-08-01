package vectorsearch

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// MaximumEmbeddingDimensions bounds a generated vector. AGENTS.md forbids
// unbounded buffers; a provider that claims a larger space is rejected
// rather than truncated.
const MaximumEmbeddingDimensions = 4096

// MaximumEmbeddingInputBytes bounds the text handed to an embedder.
const MaximumEmbeddingInputBytes = 64 * 1024

// Embedder turns already-scrubbed descriptive text into a vector (M21-081).
//
// The port takes TEXT, never a document, revision, or artifact. That is
// deliberate: scrubbing and field selection happen upstream in
// internal/atomdoc (M21-118/128/130), so an embedder cannot reach a source
// path, a hash, a timestamp, or an unredacted secret even by accident.
type Embedder interface {
	// Identity names the model producing the vectors, so every stored
	// vector can be traced to what made it (M21-080, M21-G06).
	Identity() EmbeddingModelIdentity
	// Embed returns one vector per input segment, in order.
	Embed(ctx context.Context, segments []string) ([][]float32, error)
}

// EmbeddingModelIdentity records exactly what produced a vector.
type EmbeddingModelIdentity struct {
	Provider      string
	ModelName     string
	ModelVersion  string
	Dimensions    int
	Normalization string
}

// Validate rejects an identity that could not be traced back later.
func (identity EmbeddingModelIdentity) Validate() error {
	switch {
	case strings.TrimSpace(identity.Provider) == "":
		return errors.New("embedding provider must be named")
	case strings.TrimSpace(identity.ModelName) == "":
		return errors.New("embedding model name must be named")
	case strings.TrimSpace(identity.ModelVersion) == "":
		return errors.New("embedding model version must be named")
	case identity.Dimensions <= 0 || identity.Dimensions > MaximumEmbeddingDimensions:
		return fmt.Errorf("embedding dimensions must be between 1 and %d", MaximumEmbeddingDimensions)
	case identity.Normalization != "l2" && identity.Normalization != "none":
		return errors.New(`embedding normalization must be "l2" or "none"`)
	}
	return nil
}

// GeneratedEmbedding is one vector plus the identity needed to trace it.
type GeneratedEmbedding struct {
	Vector []float32
	Model  EmbeddingModelIdentity
}

// GenerateEmbeddings turns scrubbed descriptive segments into vectors
// (M21-081).
//
// It refuses to run unless the §0 branch gate has opened: an embedding
// generated before deterministic retrieval showed a measured recall problem
// would be work the plan explicitly says not to do yet. The measurement is
// the caller's, taken from M21-078's instrument.
func GenerateEmbeddings(
	ctx context.Context,
	embedder Embedder,
	measurement RecallMeasurement,
	segments []string,
) ([]GeneratedEmbedding, error) {
	if embedder == nil {
		return nil, errors.New("embedder must not be nil")
	}
	if err := AuthorizeEmbeddingProviderSelection(measurement); err != nil {
		return nil, err
	}
	identity := embedder.Identity()
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, errors.New("embedding input must not be empty")
	}
	total := 0
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return nil, errors.New("embedding input segments must not be blank")
		}
		total += len(segment)
	}
	if total > MaximumEmbeddingInputBytes {
		return nil, fmt.Errorf("embedding input of %d bytes exceeds the %d byte bound", total, MaximumEmbeddingInputBytes)
	}

	vectors, err := embedder.Embed(ctx, segments)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(segments) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d segments", len(vectors), len(segments))
	}
	generated := make([]GeneratedEmbedding, 0, len(vectors))
	for index, vector := range vectors {
		if len(vector) != identity.Dimensions {
			return nil, fmt.Errorf(
				"segment %d: embedder returned %d dimensions, but its identity declares %d",
				index, len(vector), identity.Dimensions,
			)
		}
		if identity.Normalization == "l2" {
			if err := requireUnitLength(vector); err != nil {
				return nil, fmt.Errorf("segment %d: %w", index, err)
			}
		}
		generated = append(generated, GeneratedEmbedding{Vector: vector, Model: identity})
	}
	return generated, nil
}

// requireUnitLength verifies a model that claims L2 normalization actually
// produced unit vectors, rather than trusting the claim.
func requireUnitLength(vector []float32) error {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	length := math.Sqrt(sum)
	if length == 0 {
		return errors.New("a zero-magnitude vector cannot be L2 normalized")
	}
	if math.Abs(length-1) > 1e-4 {
		return fmt.Errorf("model declares l2 normalization but produced a vector of length %v", length)
	}
	return nil
}

// DeterministicEmbedder is a local, offline embedder used for tests and for
// exercising the vector path without selecting a real provider.
//
// It is NOT a semantic model: similar text does not produce similar vectors.
// It exists so the storage, traceability, and ranking machinery can be
// tested end to end, and it names itself honestly so a vector produced by it
// can never be mistaken for a real embedding.
type DeterministicEmbedder struct {
	Dimensions int
}

// Identity names this embedder honestly.
func (embedder DeterministicEmbedder) Identity() EmbeddingModelIdentity {
	dimensions := embedder.Dimensions
	if dimensions <= 0 {
		dimensions = 8
	}
	return EmbeddingModelIdentity{
		Provider:      "codeflux-local",
		ModelName:     "deterministic-hash-not-semantic",
		ModelVersion:  "1",
		Dimensions:    dimensions,
		Normalization: "l2",
	}
}

// Embed hashes each segment into a stable unit vector.
func (embedder DeterministicEmbedder) Embed(_ context.Context, segments []string) ([][]float32, error) {
	dimensions := embedder.Identity().Dimensions
	vectors := make([][]float32, 0, len(segments))
	for _, segment := range segments {
		vector := make([]float32, dimensions)
		digest := sha256.Sum256([]byte(segment))
		for index := range vector {
			// Re-hash per block so dimensions beyond the digest stay stable
			// and deterministic rather than repeating.
			block := sha256.Sum256(append(digest[:], byte(index/8), byte(index%8)))
			bits := binary.BigEndian.Uint32(block[:4])
			vector[index] = float32(bits%2000)/1000 - 1
		}
		normalizeUnit(vector)
		vectors = append(vectors, vector)
	}
	return vectors, nil
}

// normalizeUnit scales a vector to unit length in place.
func normalizeUnit(vector []float32) {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	length := math.Sqrt(sum)
	if length == 0 {
		if len(vector) > 0 {
			vector[0] = 1
		}
		return
	}
	for index := range vector {
		vector[index] = float32(float64(vector[index]) / length)
	}
}

// atomembedding.go implements the combined, full-schema atom embedding
// input this package's doc.go anticipates as "the separate,
// not-yet-implemented atom embedding-input schema (M21-127)": the naming
// lane's canonical-name/normalized-phrase/alias contribution
// (ComposeNameEmbeddingInput, already implemented) combined with
// internal/atomdoc's documentation-lane contribution
// (atomdoc.ComposeDocumentationEmbeddingInput) into one ordered
// AtomEmbeddingInput, plus the typed EmbeddingProvenance record (M21-132)
// and the M21-164 extension of DetermineEmbeddingInvalidationTriggers.
//
// This file lives in atomname rather than atomdoc because the combined input
// needs both packages' types, and atomname already imports atomdoc
// (atomdoc must not import atomname back, so the reverse direction is not
// available). It creates, stores, or looks up no vector: every type here is
// a typed record or pure decision function for a later, currently-gated
// lane to persist and act on.

package atomname

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/atomdoc"
)

// AtomEmbeddingInput is the complete, schema-v1 embedding input for one
// atom: the naming lane's contribution (Name) followed by the documentation
// lane's contribution (Documentation), in that order (M21-127). AGENTS.md
// "Atom Naming Style" requires the canonical name and normalized phrase
// exactly once in embedding input: Name alone already guarantees that
// (ComposeNameEmbeddingInput, M21-161), and Documentation cannot duplicate
// it even if a Document field's prose happens to mention the atom's name in
// passing, because atomdoc.ComposeDocumentationEmbeddingInput only ever
// emits atomdoc.EmbeddingInputFieldRole values — a type wholly distinct from
// atomname.EmbeddingSegmentRole — so no documentation segment can ever be
// mistaken for, or counted as, a canonical-name or normalized-phrase
// segment.
type AtomEmbeddingInput struct {
	SchemaVersion uint32
	Name          NameEmbeddingInput
	Documentation atomdoc.DocumentationEmbeddingInput
}

// ComposeAtomEmbeddingInput composes the complete embedding input for one
// atom from its current naming revision, alias lineage, and admitted
// documentation (M21-127..131, M21-161..163).
func ComposeAtomEmbeddingInput(record AtomNameRecord, aliases []AtomNameAliasRecord, doc atomdoc.Document) (AtomEmbeddingInput, error) {
	nameInput, err := ComposeNameEmbeddingInput(record, aliases)
	if err != nil {
		return AtomEmbeddingInput{}, err
	}
	documentationInput, err := atomdoc.ComposeDocumentationEmbeddingInput(doc)
	if err != nil {
		return AtomEmbeddingInput{}, err
	}
	return AtomEmbeddingInput{
		SchemaVersion: atomdoc.EmbeddingInputSchemaVersion,
		Name:          nameInput,
		Documentation: documentationInput,
	}, nil
}

// ErrInvalidEmbeddingInputHash classifies a malformed embedding-input hash.
var ErrInvalidEmbeddingInputHash = errors.New("invalid atom embedding-input hash")

// EmbeddingInputHash identifies the exact composed AtomEmbeddingInput (name
// segments plus documentation segments, in schema order) that produced one
// derived vector (M21-132). It is a distinct Go type and a distinct hash
// domain from atomdoc.NormalizedInputHash, which hashes the full 19-field
// documentation record rather than the reduced embedding-input subset, so
// the two can never be interchanged by the type system.
type EmbeddingInputHash struct{ value string }

// ParseEmbeddingInputHash validates a lowercase hex SHA-256 digest.
func ParseEmbeddingInputHash(raw string) (EmbeddingInputHash, error) {
	if !isLowerHexOfLength(raw, 64) {
		return EmbeddingInputHash{}, fmt.Errorf("%w: must be 64 lowercase hexadecimal characters", ErrInvalidEmbeddingInputHash)
	}
	return EmbeddingInputHash{value: raw}, nil
}

// String returns the lowercase hex digest.
func (hash EmbeddingInputHash) String() string { return hash.value }

// IsZero reports whether no embedding-input hash has been assigned.
func (hash EmbeddingInputHash) IsZero() bool { return hash.value == "" }

func isLowerHexOfLength(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// ComputeAtomEmbeddingInputHash hashes the canonical serialization of input
// (M21-132). Every role and text value is length-prefixed rather than
// separator-joined, so no segment's content can forge a hash collision by
// embedding a byte this function would otherwise use as a delimiter. This
// matters here specifically because, unlike atomdoc's own
// canonicalizeDocument (hash.go), this function cannot assume its inputs
// were already rejected for embedded control bytes: an AtomNameAliasRecord's
// free-text AliasText carries no such restriction (alias.go only trims and
// bounds its length), so a naive separator-joined encoding would reopen the
// same collision class atomdoc/validate.go's firstDisallowedControlByte
// check closes for documentation content.
func ComputeAtomEmbeddingInputHash(input AtomEmbeddingInput) EmbeddingInputHash {
	hasher := sha256.New()
	writeUint32(hasher, input.SchemaVersion)

	writeUint32(hasher, uint32(len(input.Name.Segments)))
	for _, segment := range input.Name.Segments {
		writeLengthPrefixed(hasher, string(segment.Role))
		writeLengthPrefixed(hasher, segment.Text)
		writeBool(hasher, segment.LowWeight)
	}

	writeUint32(hasher, uint32(len(input.Documentation.Segments)))
	for _, segment := range input.Documentation.Segments {
		writeLengthPrefixed(hasher, string(segment.Role))
		writeLengthPrefixed(hasher, segment.Text)
		writeBool(hasher, segment.Normalized)
	}

	sum := hasher.Sum(nil)
	return EmbeddingInputHash{value: hex.EncodeToString(sum[:])}
}

func writeUint32(hasher interface{ Write([]byte) (int, error) }, value uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	hasher.Write(buf[:])
}

func writeBool(hasher interface{ Write([]byte) (int, error) }, value bool) {
	if value {
		hasher.Write([]byte{1})
		return
	}
	hasher.Write([]byte{0})
}

func writeLengthPrefixed(hasher interface{ Write([]byte) (int, error) }, s string) {
	writeUint32(hasher, uint32(len(s)))
	hasher.Write([]byte(s))
}

// Explicit bounds on embedding-provenance inputs.
const (
	MaximumEmbeddingModelFieldBytes    = 128
	MaximumEmbeddingNormalizationBytes = 64
)

// ErrInvalidEmbeddingModelIdentity classifies a malformed embedding model
// identity.
var ErrInvalidEmbeddingModelIdentity = errors.New("invalid embedding model identity")

// EmbeddingModelIdentity names the exact embedding provider, model, and
// model version one derived vector was produced by (M21-132; docs/plan.md
// §23 Vector Storage: "embedding provider, model, and model version"). This
// package never calls a provider or generates a vector; it only types the
// binding a later, currently-gated lane persists and compares.
type EmbeddingModelIdentity struct {
	Provider string
	Model    string
	Version  string
}

// NewEmbeddingModelIdentity validates provider, model, and version.
func NewEmbeddingModelIdentity(provider, model, version string) (EmbeddingModelIdentity, error) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	version = strings.TrimSpace(version)
	if provider == "" {
		return EmbeddingModelIdentity{}, fmt.Errorf("%w: provider must not be empty", ErrInvalidEmbeddingModelIdentity)
	}
	if model == "" {
		return EmbeddingModelIdentity{}, fmt.Errorf("%w: model must not be empty", ErrInvalidEmbeddingModelIdentity)
	}
	if version == "" {
		return EmbeddingModelIdentity{}, fmt.Errorf("%w: version must not be empty", ErrInvalidEmbeddingModelIdentity)
	}
	if len(provider) > MaximumEmbeddingModelFieldBytes || len(model) > MaximumEmbeddingModelFieldBytes || len(version) > MaximumEmbeddingModelFieldBytes {
		return EmbeddingModelIdentity{}, fmt.Errorf("%w: a field exceeds the maximum bound of %d bytes", ErrInvalidEmbeddingModelIdentity, MaximumEmbeddingModelFieldBytes)
	}
	return EmbeddingModelIdentity{Provider: provider, Model: model, Version: version}, nil
}

// IsZero reports whether no embedding model identity has been assigned.
func (identity EmbeddingModelIdentity) IsZero() bool {
	return identity.Provider == "" && identity.Model == "" && identity.Version == ""
}

// Equal reports whether identity and other name the same provider, model,
// and version.
func (identity EmbeddingModelIdentity) Equal(other EmbeddingModelIdentity) bool {
	return identity.Provider == other.Provider && identity.Model == other.Model && identity.Version == other.Version
}

// String renders a stable, human-readable model identity.
func (identity EmbeddingModelIdentity) String() string {
	if identity.IsZero() {
		return ""
	}
	return identity.Provider + "/" + identity.Model + "@" + identity.Version
}

// ErrInvalidEmbeddingNormalization classifies a malformed normalization
// rule.
var ErrInvalidEmbeddingNormalization = errors.New("invalid embedding normalization rule")

// EmbeddingNormalization names the numeric normalization rule applied to a
// raw embedding vector (M21-132; docs/plan.md §23 Vector Storage:
// "dimensions, numeric encoding, and normalization"). It is a validated
// non-empty label rather than a closed enum, because Codeflux does not yet
// own a fixed catalog of normalization schemes across every embedding
// provider it might eventually bind to.
type EmbeddingNormalization struct{ value string }

// NewEmbeddingNormalization validates raw as a normalization-rule label.
func NewEmbeddingNormalization(raw string) (EmbeddingNormalization, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return EmbeddingNormalization{}, fmt.Errorf("%w: must not be empty", ErrInvalidEmbeddingNormalization)
	}
	if len(trimmed) > MaximumEmbeddingNormalizationBytes {
		return EmbeddingNormalization{}, fmt.Errorf("%w: exceeds the maximum bound of %d bytes", ErrInvalidEmbeddingNormalization, MaximumEmbeddingNormalizationBytes)
	}
	return EmbeddingNormalization{value: trimmed}, nil
}

// String returns the normalization-rule label.
func (normalization EmbeddingNormalization) String() string { return normalization.value }

// IsZero reports whether no normalization rule has been assigned.
func (normalization EmbeddingNormalization) IsZero() bool { return normalization.value == "" }

// Equal reports whether normalization and other name the same rule.
func (normalization EmbeddingNormalization) Equal(other EmbeddingNormalization) bool {
	return normalization.value == other.value
}

// ErrInvalidEmbeddingProvenance classifies a rejected embedding-provenance
// record.
var ErrInvalidEmbeddingProvenance = errors.New("invalid atom embedding provenance")

// EmbeddingProvenance is the typed record the schema lane persists to bind
// one derived vector to the exact model, dimensionality, normalization
// rule, embedding-input schema version, and input hash that produced it
// (M21-132; docs/plan.md §7, §23 Vector Storage). This package never
// creates, stores, looks up, or deletes a vector: it only defines the typed
// binding for a later, currently-gated lane to persist. Nothing in this
// package can construct an EmbeddingProvenance carrying a stale InputHash
// for a different AtomEmbeddingInput than the one that produced it, because
// NewEmbeddingProvenance always derives InputHash from the input it is
// given rather than accepting a caller-supplied hash.
type EmbeddingProvenance struct {
	Model              EmbeddingModelIdentity
	Dimensions         uint32
	Normalization      EmbeddingNormalization
	InputSchemaVersion uint32
	InputHash          EmbeddingInputHash
}

// NewEmbeddingProvenance constructs a validated EmbeddingProvenance for
// input, deriving InputSchemaVersion and InputHash from input itself
// (M21-132).
func NewEmbeddingProvenance(model EmbeddingModelIdentity, dimensions uint32, normalization EmbeddingNormalization, input AtomEmbeddingInput) (EmbeddingProvenance, error) {
	if model.IsZero() {
		return EmbeddingProvenance{}, fmt.Errorf("%w: embedding model identity is required", ErrInvalidEmbeddingProvenance)
	}
	if dimensions == 0 {
		return EmbeddingProvenance{}, fmt.Errorf("%w: dimensions must be greater than zero", ErrInvalidEmbeddingProvenance)
	}
	if normalization.IsZero() {
		return EmbeddingProvenance{}, fmt.Errorf("%w: normalization rule is required", ErrInvalidEmbeddingProvenance)
	}
	if input.SchemaVersion != atomdoc.EmbeddingInputSchemaVersion {
		return EmbeddingProvenance{}, fmt.Errorf("%w: unsupported embedding-input schema version %d (expected %d)",
			ErrInvalidEmbeddingProvenance, input.SchemaVersion, atomdoc.EmbeddingInputSchemaVersion)
	}
	if len(input.Name.Segments) == 0 && len(input.Documentation.Segments) == 0 {
		return EmbeddingProvenance{}, fmt.Errorf("%w: embedding input has no segments", ErrInvalidEmbeddingProvenance)
	}
	return EmbeddingProvenance{
		Model:              model,
		Dimensions:         dimensions,
		Normalization:      normalization,
		InputSchemaVersion: input.SchemaVersion,
		InputHash:          ComputeAtomEmbeddingInputHash(input),
	}, nil
}

// DetermineEmbeddingConfigChanged reports whether the embedding model,
// dimensions, normalization rule, or input-schema version differs between
// before and after (M21-133: "embedding configuration changes").
func DetermineEmbeddingConfigChanged(before, after EmbeddingProvenance) bool {
	return !before.Model.Equal(after.Model) ||
		before.Dimensions != after.Dimensions ||
		!before.Normalization.Equal(after.Normalization) ||
		before.InputSchemaVersion != after.InputSchemaVersion
}

// DetermineAtomEmbeddingLifecycleTriggers extends
// DetermineEmbeddingInvalidationTriggers — the naming-lane-only trigger
// detector already implemented for M21-161..163 — by folding its result
// into the documentation lane's combined lifecycle-trigger determination,
// producing ONE trigger set that governs an atom's derived vector regardless
// of which lane changed (M21-164). It calls
// DetermineEmbeddingInvalidationTriggers rather than reimplementing
// canonical-name/normalized-phrase/active-alias comparison.
//
// AGENTS.md requires a canonical rename, normalized-phrase change, or
// active-alias-set change to "invalidate and regenerate" the derived
// vector — the same combined effect
// (EmbeddingLifecycleTriggerNamingLaneChanged.RequiresQueuedReembedding AND
// RequiresImmediateInvalidation both true) as a semantic documentation-
// comment change, because in both cases the vector now describes an atom
// whose retrieval-relevant description has changed, not merely its
// technical representation.
func DetermineAtomEmbeddingLifecycleTriggers(
	nameBefore, nameAfter NameEmbeddingInput,
	surface atomdoc.EmbeddingLifecycleChangeSurface,
) []atomdoc.EmbeddingLifecycleTrigger {
	triggers := atomdoc.DetermineDocumentationEmbeddingLifecycleTriggers(surface)
	if len(DetermineEmbeddingInvalidationTriggers(nameBefore, nameAfter)) > 0 {
		triggers = append(triggers, atomdoc.EmbeddingLifecycleTriggerNamingLaneChanged)
	}
	return triggers
}

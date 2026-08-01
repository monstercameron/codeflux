package atomdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// ErrInvalidAtomVersionID classifies a malformed atom-version identity.
var ErrInvalidAtomVersionID = errors.New("invalid atom-version identity")

// AtomVersionID identifies one specific, immutable version of an atom. It is
// deliberately distinct from domain.AtomID (the atom identity that survives
// across versions) so the two can never be interchanged by the type system
// (M21-102). A version is meaningless without its owning atom, so it is
// modeled as an atom identity paired with a positive version number rather
// than a free-standing opaque identity.
type AtomVersionID struct {
	atomID domain.AtomID
	number uint32
}

// NewAtomVersionID constructs a validated atom-version identity.
func NewAtomVersionID(atomID domain.AtomID, number uint32) (AtomVersionID, error) {
	if atomID.IsZero() {
		return AtomVersionID{}, fmt.Errorf("%w: atom identity must not be empty", ErrInvalidAtomVersionID)
	}
	if number == 0 {
		return AtomVersionID{}, fmt.Errorf("%w: version number must be greater than zero", ErrInvalidAtomVersionID)
	}
	return AtomVersionID{atomID: atomID, number: number}, nil
}

// AtomID returns the owning, version-independent atom identity.
func (id AtomVersionID) AtomID() domain.AtomID { return id.atomID }

// Number returns the one-based version number within the owning atom.
func (id AtomVersionID) Number() uint32 { return id.number }

// IsZero reports whether no validated atom-version identity has been assigned.
func (id AtomVersionID) IsZero() bool { return id.number == 0 }

// String renders a stable, human-readable atom-version identity.
func (id AtomVersionID) String() string {
	if id.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s@v%d", id.atomID.String(), id.number)
}

// ErrInvalidContractHash classifies a malformed atom contract hash.
var ErrInvalidContractHash = errors.New("invalid atom contract hash")

// ContractHash identifies the exact atom contract (input/output types,
// effect classification, capability requirements, determinism tier, and
// response variants) that an atom-documentation revision is bound to
// (M21-122, plan.md §7 "Pinned Atom Contract").
type ContractHash struct{ value string }

// ParseContractHash validates a lowercase hex SHA-256 contract hash.
func ParseContractHash(raw string) (ContractHash, error) {
	if !validLowerHex(raw, 64) {
		return ContractHash{}, fmt.Errorf("%w: must be 64 lowercase hexadecimal characters", ErrInvalidContractHash)
	}
	return ContractHash{value: raw}, nil
}

// String returns the lowercase hex digest.
func (hash ContractHash) String() string { return hash.value }

// IsZero reports whether no contract hash has been assigned.
func (hash ContractHash) IsZero() bool { return hash.value == "" }

// DependencyBinding names one semantic dependency and the version or
// configuration that affects the bound atom's behavior (plan.md §7 "Pinned
// Atom Contract": dependency_bindings). It intentionally has no meaning
// without an explanation of relevance; a transient module list is not a
// dependency binding.
type DependencyBinding struct {
	Name    string
	Version string
}

// Validate rejects an incomplete dependency binding.
func (binding DependencyBinding) Validate() error {
	if strings.TrimSpace(binding.Name) == "" {
		return errors.New("dependency binding name must not be empty")
	}
	if strings.TrimSpace(binding.Version) == "" {
		return fmt.Errorf("dependency binding %q version must not be empty", binding.Name)
	}
	return nil
}

// ErrInvalidDocumentationRevisionID classifies a malformed revision identity.
var ErrInvalidDocumentationRevisionID = errors.New("invalid atom-documentation revision identity")

const documentationRevisionIDPrefix = "adr_"

// DocumentationRevisionID is the immutable identity of one admitted
// atom-documentation revision, kept distinct in the type system from both
// domain.AtomID and AtomVersionID (M21-102). It is content-addressed: it is
// derived deterministically from the owning atom identity, atom-version
// identity, documentation schema version, normalized-input hash, and
// contract hash, so identical semantic content always resolves to the same
// revision identity and any semantic difference resolves to a different one.
type DocumentationRevisionID struct{ value string }

// ParseDocumentationRevisionID validates a canonical revision identity.
func ParseDocumentationRevisionID(raw string) (DocumentationRevisionID, error) {
	if !strings.HasPrefix(raw, documentationRevisionIDPrefix) {
		return DocumentationRevisionID{}, fmt.Errorf("%w: missing %q prefix", ErrInvalidDocumentationRevisionID, documentationRevisionIDPrefix)
	}
	if !validLowerHex(strings.TrimPrefix(raw, documentationRevisionIDPrefix), 64) {
		return DocumentationRevisionID{}, fmt.Errorf("%w: payload must be 64 lowercase hexadecimal characters", ErrInvalidDocumentationRevisionID)
	}
	return DocumentationRevisionID{value: raw}, nil
}

// String returns the canonical "adr_<hex>" revision identity.
func (id DocumentationRevisionID) String() string { return id.value }

// IsZero reports whether no revision identity has been assigned.
func (id DocumentationRevisionID) IsZero() bool { return id.value == "" }

// deriveDocumentationRevisionID computes the immutable, content-addressed
// identity of one atom-documentation revision (M21-102).
func deriveDocumentationRevisionID(
	atomID domain.AtomID,
	atomVersion AtomVersionID,
	schemaVersion uint32,
	normalizedInputHash NormalizedInputHash,
	contractHash ContractHash,
) DocumentationRevisionID {
	hasher := sha256.New()
	fmt.Fprintf(
		hasher,
		"codeflux-atomdoc-revision-v1\x1f%s\x1f%s\x1f%d\x1f%s\x1f%s",
		atomID.String(), atomVersion.String(), schemaVersion,
		normalizedInputHash.String(), contractHash.String(),
	)
	return DocumentationRevisionID{value: documentationRevisionIDPrefix + hex.EncodeToString(hasher.Sum(nil))}
}

func validLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

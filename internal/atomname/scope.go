package atomname

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
)

// ErrInvalidNamingScope classifies a rejected naming scope.
var ErrInvalidNamingScope = errors.New("invalid atom naming scope")

// NamingScope is the project and semantic-scope pair within which canonical
// atom names must stay unique (M21-155; AGENTS.md "Atom Naming Style": "Two
// active atoms within the same project and semantic scope cannot share the
// same normalized canonical name."). The storage lane owns the actual SQL
// uniqueness constraint over (project, semantic scope, normalized phrase);
// this type and DetectCanonicalNameCollision give it, and this package's
// own tests, a pure, storage-independent definition of what "same scope"
// means.
type NamingScope struct {
	ProjectID     domain.ProjectID
	SemanticScope string
}

// NewNamingScope validates and constructs a NamingScope.
func NewNamingScope(projectID domain.ProjectID, semanticScope string) (NamingScope, error) {
	if projectID.IsZero() {
		return NamingScope{}, fmt.Errorf("%w: project identity is required", ErrInvalidNamingScope)
	}
	trimmed := strings.TrimSpace(semanticScope)
	if trimmed == "" {
		return NamingScope{}, fmt.Errorf("%w: semantic scope must not be empty", ErrInvalidNamingScope)
	}
	if len(trimmed) > MaximumSemanticScopeBytes {
		return NamingScope{}, fmt.Errorf("%w: semantic scope exceeds the maximum bound of %d bytes", ErrInvalidNamingScope, MaximumSemanticScopeBytes)
	}
	return NamingScope{ProjectID: projectID, SemanticScope: trimmed}, nil
}

// Equal reports whether scope and other name the same project and semantic
// scope.
func (scope NamingScope) Equal(other NamingScope) bool {
	return scope.ProjectID == other.ProjectID && scope.SemanticScope == other.SemanticScope
}

// IsZero reports whether scope is missing its project identity or its
// semantic-scope label — either an uninitialized NamingScope{} or a
// partially malformed scope hand-constructed by a caller that bypassed
// NewNamingScope (NamingScope's fields are exported). Every other identity
// type in this package (CanonicalName, ContractHash, AtomVersionID,
// NamingRationale) already carries this guard, enforced at construction; a
// zero NamingScope carries no such enforcement on its own, so
// NewAtomNameRecord and DetectCanonicalNameCollision call IsZero explicitly
// rather than silently accepting or matching against an unset scope.
func (scope NamingScope) IsZero() bool {
	return scope.ProjectID.IsZero() || strings.TrimSpace(scope.SemanticScope) == ""
}

// ErrInvalidAtomNameRevisionID classifies a malformed atom-name revision
// identity.
var ErrInvalidAtomNameRevisionID = errors.New("invalid atom-name revision identity")

const atomNameRevisionIDPrefix = "anr_"

// AtomNameRevisionID is the immutable, content-addressed identity of one
// atom-naming revision, kept distinct in the type system from
// domain.AtomID, atomdoc.AtomVersionID, and atomdoc.DocumentationRevisionID
// (M21-153's "revision metadata"). Identical naming content for the same
// atom, atom version, and revision sequence always resolves to the same
// revision identity.
type AtomNameRevisionID struct{ value string }

// ParseAtomNameRevisionID validates a canonical revision identity.
func ParseAtomNameRevisionID(raw string) (AtomNameRevisionID, error) {
	if !strings.HasPrefix(raw, atomNameRevisionIDPrefix) {
		return AtomNameRevisionID{}, fmt.Errorf("%w: missing %q prefix", ErrInvalidAtomNameRevisionID, atomNameRevisionIDPrefix)
	}
	payload := strings.TrimPrefix(raw, atomNameRevisionIDPrefix)
	if len(payload) != 64 || !isLowerHexString(payload) {
		return AtomNameRevisionID{}, fmt.Errorf("%w: payload must be 64 lowercase hexadecimal characters", ErrInvalidAtomNameRevisionID)
	}
	return AtomNameRevisionID{value: raw}, nil
}

// String returns the canonical "anr_<hex>" revision identity.
func (id AtomNameRevisionID) String() string { return id.value }

// IsZero reports whether no revision identity has been assigned.
func (id AtomNameRevisionID) IsZero() bool { return id.value == "" }

func deriveAtomNameRevisionID(atomID domain.AtomID, atomVersion atomdoc.AtomVersionID, sequence uint32, canonical CanonicalName) AtomNameRevisionID {
	hasher := sha256.New()
	fmt.Fprintf(hasher, "codeflux-atomname-revision-v1\x1f%s\x1f%s\x1f%d\x1f%s\x1f%d",
		atomID.String(), atomVersion.String(), sequence, canonical.String(), SchemaVersion)
	return AtomNameRevisionID{value: atomNameRevisionIDPrefix + hex.EncodeToString(hasher.Sum(nil))}
}

func isLowerHexString(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// ErrInvalidAtomNameRecord classifies a rejected atom-name record.
var ErrInvalidAtomNameRecord = errors.New("invalid atom name record")

// AtomNameRecord is the typed, immutable naming revision this package
// returns for the storage lane to persist as one row of atom_names
// (M21-153: atom ID, atom version, canonical name, display name,
// normalized phrase, schema version, rationale, validity, and revision
// metadata). Exactly one AtomNameRecord per atom (and atom version) should
// carry Valid == true at a time; making that swap transactional is the
// storage lane's responsibility, not this package's.
type AtomNameRecord struct {
	RevisionID           AtomNameRevisionID
	AtomID               domain.AtomID
	AtomVersion          atomdoc.AtomVersionID
	Scope                NamingScope
	SchemaVersion        uint32
	CanonicalName        CanonicalName
	DisplayName          DisplayName
	NormalizedPhrase     NormalizedPhrase
	Rationale            NamingRationale
	RevisionSequence     uint32
	SupersedesRevisionID AtomNameRevisionID
	Valid                bool
	CreatedAt            time.Time
}

// NewAtomNameRecord constructs one validated, current (Valid == true) atom
// naming revision, deriving the display name and normalized phrase from the
// canonical name (M21-148, M21-149) and stamping naming-schema version 1
// (M21-146). revisionSequence is one-based; the first revision for an atom
// must not name a superseded revision, and every later revision must.
func NewAtomNameRecord(
	atomID domain.AtomID,
	atomVersion atomdoc.AtomVersionID,
	scope NamingScope,
	canonical CanonicalName,
	rationale NamingRationale,
	revisionSequence uint32,
	supersedes AtomNameRevisionID,
	createdAt time.Time,
) (AtomNameRecord, error) {
	if atomID.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: atom identity is required", ErrInvalidAtomNameRecord)
	}
	if atomVersion.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: atom-version identity is required", ErrInvalidAtomNameRecord)
	}
	if scope.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: naming scope is required", ErrInvalidAtomNameRecord)
	}
	if canonical.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: canonical name is required", ErrInvalidAtomNameRecord)
	}
	if rationale.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: naming rationale is required", ErrInvalidAtomNameRecord)
	}
	if revisionSequence == 0 {
		return AtomNameRecord{}, fmt.Errorf("%w: revision sequence must be greater than zero", ErrInvalidAtomNameRecord)
	}
	if revisionSequence == 1 && !supersedes.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: the first revision must not supersede another revision", ErrInvalidAtomNameRecord)
	}
	if revisionSequence > 1 && supersedes.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: revision %d must name the revision it supersedes", ErrInvalidAtomNameRecord, revisionSequence)
	}
	if createdAt.IsZero() {
		return AtomNameRecord{}, fmt.Errorf("%w: created-at timestamp is required", ErrInvalidAtomNameRecord)
	}

	return AtomNameRecord{
		RevisionID:           deriveAtomNameRevisionID(atomID, atomVersion, revisionSequence, canonical),
		AtomID:               atomID,
		AtomVersion:          atomVersion,
		Scope:                scope,
		SchemaVersion:        SchemaVersion,
		CanonicalName:        canonical,
		DisplayName:          DeriveDisplayName(canonical),
		NormalizedPhrase:     DeriveNormalizedPhrase(canonical),
		Rationale:            rationale,
		RevisionSequence:     revisionSequence,
		SupersedesRevisionID: supersedes,
		Valid:                true,
		CreatedAt:            createdAt,
	}, nil
}

// DetectCanonicalNameCollision reports whether candidate's normalized
// phrase is already claimed by a different, currently valid atom in the
// same naming scope (M21-155, tested by M21-170 and M21-171).
// excludeAtomID lets a caller re-validate an atom's own current name (for
// example while re-deriving a rename) without the atom colliding with
// itself. A zero scope is rejected with an explicit error rather than
// silently matching nothing: an uninitialized NamingScope{} cannot equal
// any real record's scope, so without this check a caller that passed a
// zero scope by mistake would see "no collision" — a fail-open false
// negative — instead of a loud signal that its own input was malformed.
func DetectCanonicalNameCollision(
	scope NamingScope,
	existing []AtomNameRecord,
	candidate NormalizedPhrase,
	excludeAtomID domain.AtomID,
) (bool, *AtomNameRecord, error) {
	if scope.IsZero() {
		return false, nil, fmt.Errorf("%w: naming scope is required", ErrInvalidNamingScope)
	}
	for index := range existing {
		record := existing[index]
		if !record.Valid {
			continue
		}
		if !record.Scope.Equal(scope) {
			continue
		}
		if record.AtomID == excludeAtomID {
			continue
		}
		if record.NormalizedPhrase == candidate {
			return true, &existing[index], nil
		}
	}
	return false, nil, nil
}

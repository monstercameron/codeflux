package atomcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// CatalogSchemaVersion versions the catalog's own identity derivation,
// independently of the documentation schema. Changing how an entry's atom
// identity is derived must change this, or two different derivations would
// produce colliding identities for the same entry.
const CatalogSchemaVersion uint32 = 1

// SeedResult reports what one seeding run did, separating entries that were
// written from entries that were already present. A run that reports nothing
// cannot be told apart from one that never looked, so both counts are always
// returned even when zero.
type SeedResult struct {
	Written   []string
	Unchanged []string
}

// AtomDocumentationWriter is the storage capability seeding needs. It is
// declared here as a narrow interface so this package depends on one method
// rather than on the whole repository surface, and so a test can exercise
// seeding without a database.
type AtomDocumentationWriter interface {
	CreateAtomDocumentationRevision(
		ctx context.Context,
		input storage.CreateAtomDocumentationRevision,
	) (storage.AtomDocumentationRevision, error)
}

// SeedControlFlowCatalog writes every control-flow entry as an admitted
// atom-documentation revision, so the entries are retrievable through the same
// path as any other atom.
//
// Seeding is idempotent by construction rather than by checking first: a
// documentation revision identity is content-addressed, so re-running with
// unchanged entries resolves to the same identities and the repository accepts
// them as no-ops. An entry whose text changed derives a different identity and
// is written as a new revision, which is the correct outcome — documentation
// revisions are immutable, and an edit is a new revision rather than a rewrite.
//
// createdAt is supplied by the caller rather than read from the clock here,
// because a package that reads the clock cannot be replayed and cannot be
// tested for the identity stability this function's idempotence depends on.
func SeedControlFlowCatalog(
	ctx context.Context,
	writer AtomDocumentationWriter,
	projectID domain.ProjectID,
	sourceRepositoryRevision string,
	createdAt time.Time,
) (SeedResult, error) {
	if writer == nil {
		return SeedResult{}, fmt.Errorf("atomcatalog: an atom documentation writer is required")
	}
	if projectID.IsZero() {
		return SeedResult{}, fmt.Errorf("atomcatalog: a project identity is required")
	}
	if sourceRepositoryRevision == "" {
		return SeedResult{}, fmt.Errorf("atomcatalog: a source repository revision is required")
	}
	if createdAt.IsZero() || createdAt.Location() != time.UTC {
		return SeedResult{}, fmt.Errorf("atomcatalog: created-at must be a non-zero UTC timestamp")
	}

	result := SeedResult{}
	for _, entry := range controlFlowEntries {
		revision, err := entry.Revision(sourceRepositoryRevision, createdAt)
		if err != nil {
			return SeedResult{}, fmt.Errorf("atomcatalog: entry %d %q: %w", entry.Ordinal, entry.Name, err)
		}
		stored, err := writer.CreateAtomDocumentationRevision(ctx, storage.CreateAtomDocumentationRevision{
			ProjectID: projectID,
			Revision:  revision,
		})
		if err != nil {
			return SeedResult{}, fmt.Errorf("atomcatalog: persist entry %d %q: %w", entry.Ordinal, entry.Name, err)
		}
		// A repository accepting an identical revision returns the stored row
		// unchanged, so comparing creation times distinguishes a fresh write
		// from an accepted repeat without a second query.
		if stored.CreatedAt.Equal(createdAt) {
			result.Written = append(result.Written, entry.Name)
		} else {
			result.Unchanged = append(result.Unchanged, entry.Name)
		}
	}
	return result, nil
}

// Revision derives one entry's complete documentation revision, including the
// identities the storage lane requires.
//
// Every identity here is derived deterministically from the entry's ordinal
// and name rather than generated, so seeding the same catalog twice produces
// the same atom identities. A generated identity would create a duplicate atom
// on every run and make the catalog unusable as a stable retrieval corpus.
func (entry ControlFlowEntry) Revision(
	sourceRepositoryRevision string,
	createdAt time.Time,
) (atomdoc.DocumentationRevision, error) {
	atomID, err := entry.AtomID()
	if err != nil {
		return atomdoc.DocumentationRevision{}, err
	}
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		return atomdoc.DocumentationRevision{}, err
	}

	// Tier 1 graph-native: a control-flow construct has no Go implementation of
	// its own, which is exactly the tier whose documentation is authored as a
	// structured record rather than extracted from a source comment.
	authoring, err := atomdoc.ClassifyAtomDocumentationAuthoring(atomdoc.AtomTierGraphNative)
	if err != nil {
		return atomdoc.DocumentationRevision{}, err
	}

	document := entry.Document()
	normalizedHash := atomdoc.ComputeNormalizedDocumentationInputHash(atomdoc.SchemaVersion, document)

	// A SQLite-authored entry has no source comment, so the comment hash is
	// taken over the generated projection of this record. That keeps the field
	// meaningful — it still detects a changed rendering — without pretending a
	// source comment exists.
	comment, err := atomdoc.GenerateAtomDocumentationComment(
		entry.Name, entry.Name+" "+lowerFirst(entry.Purpose), atomdoc.SchemaVersion, document)
	if err != nil {
		return atomdoc.DocumentationRevision{}, fmt.Errorf("generate comment: %w", err)
	}
	sourceHash := atomdoc.ComputeSourceCommentHash(comment)

	contractHash, err := entry.ContractHash()
	if err != nil {
		return atomdoc.DocumentationRevision{}, err
	}

	return atomdoc.DocumentationRevision{
		RevisionID:               deriveRevisionID(atomID, atomVersion, normalizedHash, contractHash),
		AtomID:                   atomID,
		AtomVersion:              atomVersion,
		SchemaVersion:            atomdoc.SchemaVersion,
		Authoring:                authoring,
		SourceRepositoryRevision: sourceRepositoryRevision,
		SourceCommentHash:        sourceHash,
		NormalizedInputHash:      normalizedHash,
		ContractHash:             contractHash,
		Document:                 document,
		CreatedAt:                createdAt,
	}, nil
}

// catalogEpochMillis is the fixed timestamp every catalog identity carries.
//
// Domain identities are UUIDv7, whose leading 48 bits are a millisecond
// timestamp. A catalog entry has no meaningful creation instant — it is a
// declaration, not an event — and reading the clock here would make identities
// differ between runs, which is exactly what seeding must not do. A fixed
// epoch makes the whole catalog sort as one block and says plainly that these
// identities are derived rather than minted.
const catalogEpochMillis int64 = 1_767_225_600_000 // 2026-01-01T00:00:00Z

// AtomID derives this entry's stable atom identity from its ordinal and name.
//
// The identity is derived, never generated, so re-seeding resolves to the same
// atom rather than creating a duplicate on every run.
func (entry ControlFlowEntry) AtomID() (domain.AtomID, error) {
	return domain.ParseAtomID("atm_" + catalogUUIDv7(catalogDigestBytes("atom", entry)))
}

// catalogUUIDv7 renders a deterministic UUIDv7-shaped payload: the fixed
// catalog epoch in the leading 48 bits, digest bytes in place of entropy, and
// the version and variant bits the format requires.
func catalogUUIDv7(digest [32]byte) string {
	milliseconds := uint64(catalogEpochMillis)
	var value [16]byte
	value[0] = byte(milliseconds >> 40)
	value[1] = byte(milliseconds >> 32)
	value[2] = byte(milliseconds >> 24)
	value[3] = byte(milliseconds >> 16)
	value[4] = byte(milliseconds >> 8)
	value[5] = byte(milliseconds)
	copy(value[6:], digest[:10])
	value[6] = value[6]&0x0f | 0x70
	value[8] = value[8]&0x3f | 0x80

	var encoded [32]byte
	hex.Encode(encoded[:], value[:])
	return string(encoded[0:8]) + "-" +
		string(encoded[8:12]) + "-" +
		string(encoded[12:16]) + "-" +
		string(encoded[16:20]) + "-" +
		string(encoded[20:32])
}

// ContractHash is a placeholder derived from the entry's declared shape.
//
// This is emphatically not a real contract hash: no atom contract exists to
// hash, and complexity, effects, and capability requirements are not
// contract-expressible today. It is derived rather than left empty because the
// storage lane requires the field, and it is derived from the entry's own text
// so that a changed entry changes the hash rather than silently reusing one.
func (entry ControlFlowEntry) ContractHash() (atomdoc.ContractHash, error) {
	return atomdoc.ParseContractHash(catalogDigest("contract-placeholder", entry))
}

// catalogDigestBytes hashes an entry's identity-bearing fields under a domain
// separator, so an atom identity and a contract placeholder derived from the
// same entry cannot collide.
func catalogDigestBytes(purpose string, entry ControlFlowEntry) [32]byte {
	hasher := sha256.New()
	fmt.Fprintf(hasher, "codeflux-atomcatalog-v%d\x1f%s\x1f%d\x1f%s\x1f%s\x1f%s",
		CatalogSchemaVersion, purpose, entry.Ordinal, entry.Name, entry.GraphForm, entry.Readiness)
	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

// catalogDigest renders catalogDigestBytes as lowercase hexadecimal.
func catalogDigest(purpose string, entry ControlFlowEntry) string {
	digest := catalogDigestBytes(purpose, entry)
	return hex.EncodeToString(digest[:])
}

// deriveRevisionID mirrors the content-addressing atomdoc uses internally, so
// a catalog revision's identity changes exactly when its content does.
func deriveRevisionID(
	atomID domain.AtomID,
	atomVersion atomdoc.AtomVersionID,
	normalized atomdoc.NormalizedInputHash,
	contract atomdoc.ContractHash,
) atomdoc.DocumentationRevisionID {
	hasher := sha256.New()
	fmt.Fprintf(hasher, "codeflux-atomdoc-revision-v1\x1f%s\x1f%s\x1f%d\x1f%s\x1f%s",
		atomID.String(), atomVersion.String(), atomdoc.SchemaVersion,
		normalized.String(), contract.String())
	id, err := atomdoc.ParseDocumentationRevisionID("adr_" + hex.EncodeToString(hasher.Sum(nil)))
	if err != nil {
		// Unreachable: the payload is a fixed-width hex digest under a constant
		// prefix, which is exactly what the parser accepts.
		panic("atomcatalog: derived revision identity is malformed: " + err.Error())
	}
	return id
}

// lowerFirst lowercases the first rune so a purpose sentence reads correctly
// after the identifier that opens a Go doc comment.
func lowerFirst(text string) string {
	if text == "" {
		return text
	}
	runes := []rune(text)
	if runes[0] >= 'A' && runes[0] <= 'Z' {
		runes[0] = runes[0] - 'A' + 'a'
	}
	return string(runes)
}

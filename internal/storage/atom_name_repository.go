// Package storage: atom-naming revision and alias storage (M21-153..156).
// See docs/plan.md §7 "Atom Naming and Retrieval Identity" and §23 "Atom
// Storage", and internal/atomname.AtomNameRecord/AtomNameAliasRecord/
// NamingScope/DetectCanonicalNameCollision, whose exact fields and
// semantics this file persists and mirrors.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/internal/domain"
)

// AtomNameRevision is one persisted, immutable atom-naming revision plus its
// mutable one-way valid overlay.
type AtomNameRevision struct {
	RevisionID           atomname.AtomNameRevisionID
	AtomID               domain.AtomID
	AtomVersion          atomdoc.AtomVersionID
	Scope                atomname.NamingScope
	SchemaVersion        uint32
	CanonicalName        atomname.CanonicalName
	DisplayName          atomname.DisplayName
	NormalizedPhrase     atomname.NormalizedPhrase
	Rationale            atomname.NamingRationale
	RevisionSequence     uint32
	SupersedesRevisionID atomname.AtomNameRevisionID
	Valid                bool
	CreatedAt            time.Time
}

// CreateAtomNameRevision persists one atomname.AtomNameRecord (M21-153).
// record.RevisionSequence == 1 inserts the atom's first naming revision;
// RevisionSequence > 1 additionally invalidates
// record.SupersedesRevisionID's row in the same transaction, making the
// "exactly one AtomNameRecord per atom (and atom version) carries Valid ==
// true at a time" swap atomname.AtomNameRecord's doc comment requires
// transactional, as that comment says is this storage lane's
// responsibility. The naming-scope collision (M21-155) and project-
// stability triggers run as part of the INSERT.
func (repositories *Repositories) CreateAtomNameRevision(
	ctx context.Context,
	record atomname.AtomNameRecord,
) (AtomNameRevision, error) {
	if record.RevisionID.IsZero() {
		return AtomNameRevision{}, errors.New("atom name revision ID must not be empty")
	}
	if record.AtomID.IsZero() {
		return AtomNameRevision{}, errors.New("atom name atom ID must not be empty")
	}
	if record.AtomVersion.IsZero() {
		return AtomNameRevision{}, errors.New("atom name atom version must not be empty")
	}
	if record.AtomVersion.AtomID() != record.AtomID {
		return AtomNameRevision{}, errors.New("atom name atom version does not belong to the declared atom")
	}
	if record.Scope.IsZero() {
		return AtomNameRevision{}, fmt.Errorf("%w: naming scope is required", atomname.ErrInvalidNamingScope)
	}
	if record.CanonicalName.IsZero() {
		return AtomNameRevision{}, errors.New("atom name canonical name must not be empty")
	}
	if record.Rationale.IsZero() {
		return AtomNameRevision{}, errors.New("atom name rationale must not be empty")
	}
	if record.SchemaVersion == 0 {
		return AtomNameRevision{}, errors.New("atom name schema version must not be zero")
	}
	if record.RevisionSequence == 0 {
		return AtomNameRevision{}, errors.New("atom name revision sequence must be greater than zero")
	}
	if !record.Valid {
		return AtomNameRevision{}, errors.New("a newly created atom name revision must be valid")
	}
	if record.RevisionSequence == 1 && !record.SupersedesRevisionID.IsZero() {
		return AtomNameRevision{}, errors.New("the first atom name revision must not supersede another revision")
	}
	if record.RevisionSequence > 1 && record.SupersedesRevisionID.IsZero() {
		return AtomNameRevision{}, fmt.Errorf("atom name revision %d must name the revision it supersedes", record.RevisionSequence)
	}

	_, micros := repositories.timestamp()
	var result AtomNameRevision
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findAtomNameRevision(ctx, transaction.sql, record.RevisionID)
		if err != nil {
			return err
		}
		if found {
			if existing.CanonicalName != record.CanonicalName || existing.Scope.ProjectID != record.Scope.ProjectID {
				return typedError(ErrConflict, "create atom name revision", errors.New("revision identity retried with different content"))
			}
			result = existing
			return nil
		}
		if !record.SupersedesRevisionID.IsZero() {
			prior, found, err := findAtomNameRevision(ctx, transaction.sql, record.SupersedesRevisionID)
			if err != nil {
				return err
			}
			if !found {
				return typedError(ErrConflict, "create atom name revision", errors.New("superseded revision does not exist"))
			}
			if !prior.Valid {
				return typedError(ErrConflict, "create atom name revision", errors.New("superseded revision is no longer valid"))
			}
			if prior.AtomID != record.AtomID || prior.AtomVersion != record.AtomVersion {
				return typedError(ErrConflict, "create atom name revision", errors.New("superseded revision belongs to a different atom or atom version"))
			}
			if _, err := transaction.sql.ExecContext(
				ctx, `UPDATE atom_names SET valid = 0 WHERE id = ?`, record.SupersedesRevisionID.String(),
			); err != nil {
				return repositoryWriteError("invalidate superseded atom name revision", err)
			}
		}
		if err := insertAtomNameRevision(ctx, transaction, record, micros); err != nil {
			return err
		}
		result = atomNameRevisionFromRecord(record, repositoryTime(micros))
		return nil
	})
	if err != nil {
		return AtomNameRevision{}, err
	}
	return result, nil
}

// GetAtomNameRevision reads one naming revision by its exact identity.
func (repositories *Repositories) GetAtomNameRevision(
	ctx context.Context,
	id atomname.AtomNameRevisionID,
) (AtomNameRevision, error) {
	if id.IsZero() {
		return AtomNameRevision{}, errors.New("atom name revision ID must not be empty")
	}
	record, found, err := findAtomNameRevision(ctx, repositories.database.sql, id)
	if err != nil {
		return AtomNameRevision{}, err
	}
	if !found {
		return AtomNameRevision{}, typedError(ErrNotFound, "get atom name revision", errors.New("atom name revision does not exist"))
	}
	return record, nil
}

// GetCurrentAtomName reads the currently valid naming revision for one exact
// atom version, or ErrNotFound when none exists.
func (repositories *Repositories) GetCurrentAtomName(
	ctx context.Context,
	atomVersion atomdoc.AtomVersionID,
) (AtomNameRevision, error) {
	if atomVersion.IsZero() {
		return AtomNameRevision{}, errors.New("atom version must not be empty")
	}
	record, err := scanAtomNameRevision(
		repositories.database.sql.QueryRowContext(
			ctx,
			atomNameRevisionSelect+` WHERE atom_id = ? AND atom_version_number = ? AND valid = 1`,
			atomVersion.AtomID(), atomVersion.Number(),
		),
		"get current atom name",
	)
	if errors.Is(err, ErrNotFound) {
		return AtomNameRevision{}, typedError(ErrNotFound, "get current atom name", errors.New("no valid atom name revision exists"))
	}
	return record, err
}

// ListAtomNameRevisions reads every naming revision recorded for one atom
// (across every atom version), oldest first.
func (repositories *Repositories) ListAtomNameRevisions(ctx context.Context, atomID domain.AtomID) ([]AtomNameRevision, error) {
	if atomID.IsZero() {
		return nil, errors.New("atom ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		atomNameRevisionSelect+` WHERE atom_id = ? ORDER BY atom_version_number, revision_sequence`,
		atomID,
	)
	if err != nil {
		return nil, classify("list atom name revisions", err)
	}
	defer rows.Close()
	var revisions []AtomNameRevision
	for rows.Next() {
		revision, err := scanAtomNameRevision(rows, "scan atom name revision")
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, classify("list atom name revisions", rows.Err())
}

// ListAtomNamesByProject reads every currently valid canonical naming
// revision within projectID, across every atom (M21-167: searching canonical
// name and normalized phrase requires a project-scoped candidate pool, not
// the per-atom ListAtomNameRevisions/GetCurrentAtomName lookups above, both
// of which already assume the atom identity is known). project_id is
// already a plain column on atom_names (M21-153); this adds no schema.
func (repositories *Repositories) ListAtomNamesByProject(ctx context.Context, projectID domain.ProjectID) ([]AtomNameRevision, error) {
	if projectID.IsZero() {
		return nil, errors.New("project ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		atomNameRevisionSelect+` WHERE project_id = ? AND valid = 1 ORDER BY atom_id, atom_version_number`,
		projectID,
	)
	if err != nil {
		return nil, classify("list atom names by project", err)
	}
	defer rows.Close()
	var revisions []AtomNameRevision
	for rows.Next() {
		revision, err := scanAtomNameRevision(rows, "scan atom name by project")
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, classify("list atom names by project", rows.Err())
}

// ListActiveAtomNameAliasesByProject reads every currently active alias
// within projectID, across every atom (M21-167's alias channel; mirrors
// ListAtomNamesByProject's project-wide scope). project_id is already a
// plain column on atom_name_aliases (M21-154); this adds no schema.
func (repositories *Repositories) ListActiveAtomNameAliasesByProject(ctx context.Context, projectID domain.ProjectID) ([]AtomNameAlias, error) {
	if projectID.IsZero() {
		return nil, errors.New("project ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		atomNameAliasSelect+` WHERE project_id = ? AND active_until_unix_micros IS NULL ORDER BY atom_id, active_from_unix_micros`,
		projectID,
	)
	if err != nil {
		return nil, classify("list active atom name aliases by project", err)
	}
	defer rows.Close()
	var aliases []AtomNameAlias
	for rows.Next() {
		alias, err := scanAtomNameAlias(rows, "scan active atom name alias by project")
		if err != nil {
			return nil, err
		}
		aliases = append(aliases, alias)
	}
	return aliases, classify("list active atom name aliases by project", rows.Err())
}

func insertAtomNameRevision(ctx context.Context, transaction *Transaction, record atomname.AtomNameRecord, micros int64) error {
	var supersedes any
	if !record.SupersedesRevisionID.IsZero() {
		supersedes = record.SupersedesRevisionID.String()
	}
	_, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO atom_names (
			id, atom_id, atom_version_number, project_id, semantic_scope, schema_version,
			canonical_name, display_name, normalized_phrase,
			rationale_nearest_alternative, rationale_distinguishing_qualifier,
			revision_sequence, supersedes_revision_id, valid, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		record.RevisionID.String(), record.AtomID, record.AtomVersion.Number(),
		record.Scope.ProjectID, record.Scope.SemanticScope, record.SchemaVersion,
		record.CanonicalName.String(), record.DisplayName.String(), record.NormalizedPhrase.String(),
		record.Rationale.NearestConfusingAlternative(), record.Rationale.DistinguishingQualifier(),
		record.RevisionSequence, supersedes, micros,
	)
	return repositoryWriteError("insert atom name revision", err)
}

func atomNameRevisionFromRecord(record atomname.AtomNameRecord, createdAt time.Time) AtomNameRevision {
	return AtomNameRevision{
		RevisionID: record.RevisionID, AtomID: record.AtomID, AtomVersion: record.AtomVersion, Scope: record.Scope,
		SchemaVersion: record.SchemaVersion, CanonicalName: record.CanonicalName, DisplayName: record.DisplayName,
		NormalizedPhrase: record.NormalizedPhrase, Rationale: record.Rationale, RevisionSequence: record.RevisionSequence,
		SupersedesRevisionID: record.SupersedesRevisionID, Valid: record.Valid, CreatedAt: createdAt,
	}
}

const atomNameRevisionSelect = `SELECT
	id, atom_id, atom_version_number, project_id, semantic_scope, schema_version,
	canonical_name, display_name, normalized_phrase,
	rationale_nearest_alternative, rationale_distinguishing_qualifier,
	revision_sequence, supersedes_revision_id, valid, created_at_unix_micros
 FROM atom_names`

func findAtomNameRevision(ctx context.Context, queries queryRower, id atomname.AtomNameRevisionID) (AtomNameRevision, bool, error) {
	record, err := scanAtomNameRevision(
		queries.QueryRowContext(ctx, atomNameRevisionSelect+` WHERE id = ?`, id.String()),
		"find atom name revision",
	)
	if errors.Is(err, ErrNotFound) {
		return AtomNameRevision{}, false, nil
	}
	return record, err == nil, err
}

func scanAtomNameRevision(row rowScanner, operation string) (AtomNameRevision, error) {
	var (
		idRaw, atomIDRaw                                      string
		atomVersionNumber                                     uint32
		projectIDRaw, semanticScope                           string
		schemaVersion                                         uint32
		canonicalNameRaw, displayNameRaw, normalizedPhraseRaw string
		nearestAlternative, distinguishingQualifier           string
		revisionSequence                                      uint32
		supersedesRaw                                         sql.NullString
		valid                                                 bool
		createdMicros                                         int64
	)
	err := row.Scan(
		&idRaw, &atomIDRaw, &atomVersionNumber, &projectIDRaw, &semanticScope, &schemaVersion,
		&canonicalNameRaw, &displayNameRaw, &normalizedPhraseRaw,
		&nearestAlternative, &distinguishingQualifier,
		&revisionSequence, &supersedesRaw, &valid, &createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AtomNameRevision{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return AtomNameRevision{}, classify(operation, err)
	}

	revisionID, err := atomname.ParseAtomNameRevisionID(idRaw)
	if err != nil {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
	}
	atomID, err := domain.ParseAtomID(atomIDRaw)
	if err != nil {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
	}
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, atomVersionNumber)
	if err != nil {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
	}
	projectID, err := domain.ParseProjectID(projectIDRaw)
	if err != nil {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
	}
	scope, err := atomname.NewNamingScope(projectID, semanticScope)
	if err != nil {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
	}
	canonicalName, err := atomname.NewCanonicalName(canonicalNameRaw)
	if err != nil {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
	}
	rationale, err := atomname.NewNamingRationale(nearestAlternative, distinguishingQualifier)
	if err != nil {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
	}
	var supersedes atomname.AtomNameRevisionID
	if supersedesRaw.Valid {
		supersedes, err = atomname.ParseAtomNameRevisionID(supersedesRaw.String)
		if err != nil {
			return AtomNameRevision{}, typedError(ErrCorrupt, operation, err)
		}
	}

	// Defense-in-depth: display_name/normalized_phrase are stored columns
	// (for query and collision-index use), but atomname exposes no
	// constructor from an arbitrary string -- only derivation from a
	// CanonicalName. Re-derive and compare rather than trust the stored
	// text, so a row corrupted outside this repository (for example by a
	// raw-SQL write that bypasses insertAtomNameRevision) is surfaced as
	// ErrCorrupt instead of silently returned.
	derivedDisplay := atomname.DeriveDisplayName(canonicalName)
	if derivedDisplay.String() != displayNameRaw {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, fmt.Errorf(
			"stored display name %q does not match the name derived from canonical name %q", displayNameRaw, canonicalName.String(),
		))
	}
	derivedPhrase := atomname.DeriveNormalizedPhrase(canonicalName)
	if derivedPhrase.String() != normalizedPhraseRaw {
		return AtomNameRevision{}, typedError(ErrCorrupt, operation, fmt.Errorf(
			"stored normalized phrase %q does not match the phrase derived from canonical name %q", normalizedPhraseRaw, canonicalName.String(),
		))
	}

	return AtomNameRevision{
		RevisionID: revisionID, AtomID: atomID, AtomVersion: atomVersion, Scope: scope, SchemaVersion: schemaVersion,
		CanonicalName: canonicalName, DisplayName: derivedDisplay, NormalizedPhrase: derivedPhrase,
		Rationale: rationale, RevisionSequence: revisionSequence, SupersedesRevisionID: supersedes, Valid: valid,
		CreatedAt: repositoryTime(createdMicros),
	}, nil
}

// AtomNameAlias is one persisted, immutable alias-activation record
// (M21-154), mirroring internal/atomname.AtomNameAliasRecord.
type AtomNameAlias struct {
	ID                  string
	AtomID              domain.AtomID
	ProjectID           domain.ProjectID
	AliasText           string
	NormalizedAliasText string
	Source              atomname.AtomNameAliasSource
	ActiveFrom          time.Time
	ActiveUntil         *time.Time
	InvalidatedReason   string
}

// RecordAtomNameAlias persists one atomname.AtomNameAliasRecord idempotently
// (retried with the same atom, normalized text, and active-from timestamp
// resolves to the same row). The alias's project is resolved from
// record.AtomID's existing atom_names rows rather than accepted from the
// caller, so it can never disagree with the atom's actual naming project
// (no atoms/atom_versions identity table exists yet to authoritatively pin
// that association; see migrations/000026's top-of-file note). A
// 'rename'-sourced alias must name a canonical name this atom has actually
// held (M21-156); the storage trigger enforces that.
func (repositories *Repositories) RecordAtomNameAlias(
	ctx context.Context,
	record atomname.AtomNameAliasRecord,
) (AtomNameAlias, error) {
	if record.AtomID.IsZero() {
		return AtomNameAlias{}, errors.New("atom name alias atom ID must not be empty")
	}
	if err := validateBounded("atom name alias text", record.AliasText, atomname.MaximumAliasTextBytes); err != nil {
		return AtomNameAlias{}, err
	}
	if err := validateBounded("atom name alias normalized text", record.NormalizedAliasText, atomname.MaximumAliasTextBytes); err != nil {
		return AtomNameAlias{}, err
	}
	if !record.Source.IsValid() {
		return AtomNameAlias{}, fmt.Errorf("%w: unknown alias source %q", atomname.ErrInvalidAtomNameAlias, record.Source)
	}
	if record.ActiveFrom.IsZero() {
		return AtomNameAlias{}, errors.New("atom name alias active-from timestamp must not be empty")
	}
	if record.ActiveUntil != nil {
		return AtomNameAlias{}, errors.New("a newly recorded atom name alias must not already carry a closed active interval")
	}

	id := deriveAtomNameAliasID(record)
	var alias AtomNameAlias
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		projectID, err := atomProjectIDFromNaming(ctx, transaction.sql, record.AtomID)
		if err != nil {
			return err
		}
		existing, found, err := findAtomNameAlias(ctx, transaction.sql, id)
		if err != nil {
			return err
		}
		if found {
			alias = existing
			return nil
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO atom_name_aliases (
				id, atom_id, project_id, alias_text, normalized_alias_text, source, active_from_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, record.AtomID, projectID, record.AliasText, record.NormalizedAliasText, string(record.Source),
			record.ActiveFrom.UnixMicro(),
		); err != nil {
			return repositoryWriteError("record atom name alias", err)
		}
		alias = AtomNameAlias{
			ID: id, AtomID: record.AtomID, ProjectID: projectID, AliasText: record.AliasText,
			NormalizedAliasText: record.NormalizedAliasText, Source: record.Source, ActiveFrom: record.ActiveFrom.UTC(),
		}
		return nil
	})
	if err != nil {
		return AtomNameAlias{}, err
	}
	return alias, nil
}

// InvalidateAtomNameAlias closes one alias's active interval (M21-163-style
// exclusion from active candidate generation), never deleting or rewording
// it: the row remains as immutable lineage (M21-156).
func (repositories *Repositories) InvalidateAtomNameAlias(ctx context.Context, id string, reason string) error {
	if id == "" {
		return errors.New("atom name alias ID must not be empty")
	}
	if err := validateBounded("atom name alias invalidation reason", reason, 2048); err != nil {
		return err
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE atom_name_aliases SET active_until_unix_micros = ?, invalidated_reason = ?
			 WHERE id = ? AND active_until_unix_micros IS NULL`,
			micros, reason, id,
		)
		if err != nil {
			return repositoryWriteError("invalidate atom name alias", err)
		}
		return requireOneAffected(result, "invalidate atom name alias")
	})
}

// ListActiveAtomNameAliases reads every currently active alias for one atom,
// oldest first (M21-163: obsolete/invalidated aliases are excluded from
// active candidate generation while their rows remain as lineage).
func (repositories *Repositories) ListActiveAtomNameAliases(ctx context.Context, atomID domain.AtomID) ([]AtomNameAlias, error) {
	if atomID.IsZero() {
		return nil, errors.New("atom ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		atomNameAliasSelect+` WHERE atom_id = ? AND active_until_unix_micros IS NULL ORDER BY active_from_unix_micros`,
		atomID,
	)
	if err != nil {
		return nil, classify("list active atom name aliases", err)
	}
	defer rows.Close()
	var aliases []AtomNameAlias
	for rows.Next() {
		alias, err := scanAtomNameAlias(rows, "scan active atom name alias")
		if err != nil {
			return nil, err
		}
		aliases = append(aliases, alias)
	}
	return aliases, classify("list active atom name aliases", rows.Err())
}

func deriveAtomNameAliasID(record atomname.AtomNameAliasRecord) string {
	hasher := sha256.New()
	fmt.Fprintf(hasher, "codeflux-atomname-alias-v1\x1f%s\x1f%s\x1f%d",
		record.AtomID.String(), record.NormalizedAliasText, record.ActiveFrom.UnixMicro())
	return hex.EncodeToString(hasher.Sum(nil))
}

func atomProjectIDFromNaming(ctx context.Context, queries queryRower, atomID domain.AtomID) (domain.ProjectID, error) {
	var projectID domain.ProjectID
	err := queries.QueryRowContext(ctx, `SELECT project_id FROM atom_names WHERE atom_id = ? LIMIT 1`, atomID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectID{}, typedError(ErrConflict, "resolve atom project from naming", errors.New("atom has no recorded canonical name"))
	}
	if err != nil {
		return domain.ProjectID{}, classify("resolve atom project from naming", err)
	}
	return projectID, nil
}

const atomNameAliasSelect = `SELECT
	id, atom_id, project_id, alias_text, normalized_alias_text, source,
	active_from_unix_micros, active_until_unix_micros, invalidated_reason
 FROM atom_name_aliases`

func findAtomNameAlias(ctx context.Context, queries queryRower, id string) (AtomNameAlias, bool, error) {
	alias, err := scanAtomNameAlias(queries.QueryRowContext(ctx, atomNameAliasSelect+` WHERE id = ?`, id), "find atom name alias")
	if errors.Is(err, ErrNotFound) {
		return AtomNameAlias{}, false, nil
	}
	return alias, err == nil, err
}

func scanAtomNameAlias(row rowScanner, operation string) (AtomNameAlias, error) {
	var (
		alias             AtomNameAlias
		sourceRaw         string
		activeFromMicros  int64
		activeUntilMicros sql.NullInt64
		invalidatedReason sql.NullString
	)
	err := row.Scan(
		&alias.ID, &alias.AtomID, &alias.ProjectID, &alias.AliasText, &alias.NormalizedAliasText, &sourceRaw,
		&activeFromMicros, &activeUntilMicros, &invalidatedReason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AtomNameAlias{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return AtomNameAlias{}, classify(operation, err)
	}
	alias.Source = atomname.AtomNameAliasSource(sourceRaw)
	alias.ActiveFrom = repositoryTime(activeFromMicros)
	if activeUntilMicros.Valid {
		until := repositoryTime(activeUntilMicros.Int64)
		alias.ActiveUntil = &until
	}
	if invalidatedReason.Valid {
		alias.InvalidatedReason = invalidatedReason.String
	}
	return alias, nil
}

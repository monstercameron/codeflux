-- Milestone 21 (M21-103..107, M21-153..156) atom-documentation and
-- atom-naming storage: immutable atom_documentation_revisions with the
-- exact identity/hash/binding fields internal/atomdoc.DocumentationRevision
-- already carries, per-field normalized (and un-discarded) structured
-- storage in atom_documentation_fields, a documentation-revision-scoped
-- vector link in atom_documentation_embeddings that reuses the
-- memory_embedding_models/memory_embedding_spaces registry 000025 already
-- created (schema and metadata only -- no embedding generation, similarity,
-- or ranking, per AGENTS.md and docs/plan.md §0/§23), and immutable
-- atom_names/atom_name_aliases naming history mirroring
-- internal/atomname.AtomNameRecord, internal/atomname.AtomNameAliasRecord,
-- internal/atomname.NamingScope, and internal/atomname.DetectCanonicalName
-- Collision exactly.
--
-- No `atoms` or `atom_versions` identity table exists yet (no TODOS.md task
-- schedules one): domain.AtomID and atomdoc.AtomVersionID are validated,
-- content-shaped identities with no owning SQL row today. Every table below
-- therefore treats atom_id as an opaque, format-checked TEXT identity
-- (never a foreign key) and additionally records project_id explicitly
-- (supplied by the storage caller, exactly as CreateMemoryArtifact.ProjectID
-- sits beside domain.MemoryArtifactContent) so the project-boundary
-- predicates AGENTS.md requires can still be enforced today; a later lane
-- that adds `atoms`/`atom_versions` can migrate these columns onto real
-- foreign keys without changing their meaning. Because project_id can never
-- be NULL and semantic_scope/atom_id can never be empty (CHECK-enforced
-- below), no row can ever represent an uninitialized/zero NamingScope,
-- matching NamingScope.IsZero()'s fail-closed contract structurally rather
-- than by runtime check.
--
-- Content is authoritative and immutable per revision; validation_status on
-- atom_documentation_revisions and valid on atom_names are the only mutable
-- overlay columns, each one-way (admitted -> invalidated; valid -> not
-- valid), matching 000025's memory-artifact-revision maturity-overlay
-- pattern and AGENTS.md "Preserve immutable historical records; represent
-- later correction through revisions or invalidation overlays."

CREATE TABLE atom_documentation_revisions (
    id TEXT PRIMARY KEY CHECK (
        length(id) = 68 AND id GLOB 'adr_*' AND substr(id, 5) NOT GLOB '*[^0-9a-f]*'
    ),
    project_id TEXT NOT NULL REFERENCES projects(id),
    atom_id TEXT NOT NULL CHECK (length(atom_id) = 40 AND atom_id GLOB 'atm_*'),
    atom_version_number INTEGER NOT NULL CHECK (atom_version_number > 0),
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
    authoring TEXT NOT NULL CHECK (authoring IN ('source-authored', 'sqlite-authored')),
    source_repository_revision TEXT NOT NULL CHECK (length(source_repository_revision) BETWEEN 1 AND 255),
    source_comment_hash TEXT NOT NULL CHECK (length(source_comment_hash) = 64 AND source_comment_hash NOT GLOB '*[^0-9a-f]*'),
    normalized_input_hash TEXT NOT NULL CHECK (length(normalized_input_hash) = 64 AND normalized_input_hash NOT GLOB '*[^0-9a-f]*'),
    contract_hash TEXT NOT NULL CHECK (length(contract_hash) = 64 AND contract_hash NOT GLOB '*[^0-9a-f]*'),
    dependency_bindings_json TEXT NOT NULL CHECK (json_valid(dependency_bindings_json)),
    validation_status TEXT NOT NULL DEFAULT 'admitted' CHECK (validation_status IN ('admitted', 'invalidated')),
    invalidated_reason TEXT CHECK (invalidated_reason IS NULL OR length(invalidated_reason) BETWEEN 1 AND 2048),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    invalidated_at_unix_micros INTEGER CHECK (invalidated_at_unix_micros >= created_at_unix_micros),
    -- M21-106: this UNIQUE constraint is defense-in-depth alongside id's own
    -- content-addressed derivation (deriveDocumentationRevisionID hashes
    -- exactly this five-tuple): even a caller that computed id incorrectly
    -- can never claim a second row for the same normalized document, and the
    -- immutability triggers below stop an existing id from later being
    -- overwritten with a *different* document.
    UNIQUE (atom_id, atom_version_number, schema_version, normalized_input_hash, contract_hash),
    CHECK ((validation_status = 'admitted') = (invalidated_at_unix_micros IS NULL AND invalidated_reason IS NULL)),
    CHECK ((validation_status = 'invalidated') = (invalidated_at_unix_micros IS NOT NULL AND invalidated_reason IS NOT NULL))
) STRICT;

-- M21-107: indexes for atom identity, comment hash, contract hash, and
-- validity.
CREATE INDEX atom_documentation_revisions_by_atom
    ON atom_documentation_revisions(atom_id, atom_version_number);
CREATE INDEX atom_documentation_revisions_by_comment_hash
    ON atom_documentation_revisions(source_comment_hash);
CREATE INDEX atom_documentation_revisions_by_contract_hash
    ON atom_documentation_revisions(contract_hash);
CREATE INDEX atom_documentation_revisions_by_validity
    ON atom_documentation_revisions(validation_status);

-- Reproduced-defect guard: without this, two atom_documentation_revisions
-- rows for the same atom_id could silently disagree about which project
-- owns the atom (no atoms/atom_versions table exists to pin that itself).
CREATE TRIGGER atom_documentation_revisions_project_stable_per_atom
BEFORE INSERT ON atom_documentation_revisions
WHEN EXISTS (
    SELECT 1 FROM atom_documentation_revisions WHERE atom_id = NEW.atom_id AND project_id != NEW.project_id
)
BEGIN SELECT RAISE(ABORT, 'atom documentation project must remain stable across all revisions of one atom'); END;

CREATE TRIGGER atom_documentation_revisions_immutable_content
BEFORE UPDATE ON atom_documentation_revisions
WHEN NEW.id != OLD.id OR NEW.project_id != OLD.project_id OR NEW.atom_id != OLD.atom_id
  OR NEW.atom_version_number != OLD.atom_version_number OR NEW.schema_version != OLD.schema_version
  OR NEW.authoring != OLD.authoring OR NEW.source_repository_revision != OLD.source_repository_revision
  OR NEW.source_comment_hash != OLD.source_comment_hash OR NEW.normalized_input_hash != OLD.normalized_input_hash
  OR NEW.contract_hash != OLD.contract_hash OR NEW.dependency_bindings_json != OLD.dependency_bindings_json
  OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'atom documentation revision content is immutable'); END;

CREATE TRIGGER atom_documentation_revisions_validation_status_one_way
BEFORE UPDATE ON atom_documentation_revisions
WHEN NEW.validation_status != OLD.validation_status
 AND NOT (OLD.validation_status = 'admitted' AND NEW.validation_status = 'invalidated')
BEGIN SELECT RAISE(ABORT, 'atom documentation revision validation status may only transition from admitted to invalidated'); END;

CREATE TRIGGER atom_documentation_revisions_reject_delete
BEFORE DELETE ON atom_documentation_revisions
BEGIN SELECT RAISE(ABORT, 'atom documentation revisions are immutable'); END;

-- atom_documentation_fields (M21-104): the complete, per-field normalized
-- and scrubbed atomdoc.Document -- exactly the nineteen schema-v1 fields
-- atomdoc.CanonicalFieldLabels() declares, in full (Text/Items preserved
-- verbatim, never summarized), so nothing about the scrubbed comment is
-- discarded merely because it was split into fields.
CREATE TABLE atom_documentation_fields (
    revision_id TEXT NOT NULL REFERENCES atom_documentation_revisions(id),
    field_label TEXT NOT NULL CHECK (field_label IN (
        'Purpose', 'Use when', 'Do not use when', 'Semantics', 'Inputs', 'Outputs',
        'Preconditions', 'Postconditions', 'Effects', 'Failure semantics', 'Determinism',
        'Idempotency and retry', 'Reconciliation and compensation', 'Security and privacy',
        'Dependencies and bindings', 'Complexity and limits', 'Examples', 'Verification',
        'Retrieval concepts'
    )),
    field_kind TEXT NOT NULL CHECK (field_kind IN ('prose', 'list')),
    is_none INTEGER NOT NULL CHECK (is_none IN (0, 1)),
    none_reason TEXT CHECK (none_reason IS NULL OR length(none_reason) BETWEEN 1 AND 2048),
    field_text TEXT CHECK (field_text IS NULL OR length(field_text) BETWEEN 1 AND 65536),
    field_items_json TEXT CHECK (field_items_json IS NULL OR json_valid(field_items_json)),
    PRIMARY KEY (revision_id, field_label),
    CHECK (
        (is_none = 1 AND none_reason IS NOT NULL AND field_text IS NULL AND field_items_json IS NULL)
        OR (is_none = 0 AND none_reason IS NULL AND field_kind = 'prose' AND field_text IS NOT NULL AND field_items_json IS NULL)
        OR (is_none = 0 AND none_reason IS NULL AND field_kind = 'list' AND field_items_json IS NOT NULL AND field_text IS NULL)
    )
) STRICT, WITHOUT ROWID;

CREATE TRIGGER atom_documentation_fields_immutable_update
BEFORE UPDATE ON atom_documentation_fields
BEGIN SELECT RAISE(ABORT, 'atom documentation fields are immutable'); END;
CREATE TRIGGER atom_documentation_fields_immutable_delete
BEFORE DELETE ON atom_documentation_fields
BEGIN SELECT RAISE(ABORT, 'atom documentation fields are immutable'); END;

-- atom_documentation_embeddings (M21-105): links one atom embedding to the
-- exact documentation revision that produced it. Reuses
-- memory_embedding_models/memory_embedding_spaces from 000025 (do not
-- duplicate the provider/model/space registry); only the revision-target
-- junction is new, because atom_documentation_revisions is a different
-- entity family from memory_artifact_revisions. Schema and metadata only:
-- no embedding generation, similarity search, or ranking (M21-078..089,
-- gated behind docs/plan.md §0 and not started here).
CREATE TABLE atom_documentation_embeddings (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    revision_id TEXT NOT NULL REFERENCES atom_documentation_revisions(id),
    embedding_space_id TEXT NOT NULL REFERENCES memory_embedding_spaces(id),
    source_content_sha256 TEXT NOT NULL CHECK (length(source_content_sha256) = 64 AND source_content_sha256 NOT GLOB '*[^0-9a-f]*'),
    vector BLOB NOT NULL CHECK (length(vector) > 0),
    valid INTEGER NOT NULL DEFAULT 1 CHECK (valid IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    invalidated_at_unix_micros INTEGER CHECK (invalidated_at_unix_micros >= created_at_unix_micros),
    UNIQUE (revision_id, embedding_space_id),
    CHECK ((valid = 1) = (invalidated_at_unix_micros IS NULL))
) STRICT;

CREATE INDEX atom_documentation_embeddings_by_space
    ON atom_documentation_embeddings(embedding_space_id, valid);
-- Supports the M21-107 "pending re-embedding" query: for one embedding
-- space, find every admitted revision with no matching valid row here.
CREATE INDEX atom_documentation_embeddings_by_revision_validity
    ON atom_documentation_embeddings(revision_id, valid);

CREATE TRIGGER atom_documentation_embeddings_project_boundary
BEFORE INSERT ON atom_documentation_embeddings
WHEN (SELECT project_id FROM memory_embedding_spaces WHERE id = NEW.embedding_space_id)
     != (SELECT project_id FROM atom_documentation_revisions WHERE id = NEW.revision_id)
BEGIN SELECT RAISE(ABORT, 'atom documentation embedding crosses the owning project boundary'); END;

CREATE TRIGGER atom_documentation_embeddings_immutable_content
BEFORE UPDATE ON atom_documentation_embeddings
WHEN NEW.revision_id != OLD.revision_id OR NEW.embedding_space_id != OLD.embedding_space_id
  OR NEW.source_content_sha256 != OLD.source_content_sha256 OR NEW.vector != OLD.vector
  OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'atom documentation embedding content is immutable'); END;
CREATE TRIGGER atom_documentation_embeddings_immutable_delete
BEFORE DELETE ON atom_documentation_embeddings
BEGIN SELECT RAISE(ABORT, 'atom documentation embeddings are logically invalidated, never deleted'); END;

-- atom_names (M21-153): one immutable naming revision per row, mirroring
-- internal/atomname.AtomNameRecord field-for-field. project_id and
-- semantic_scope together are internal/atomname.NamingScope.
CREATE TABLE atom_names (
    id TEXT PRIMARY KEY CHECK (
        length(id) = 68 AND id GLOB 'anr_*' AND substr(id, 5) NOT GLOB '*[^0-9a-f]*'
    ),
    atom_id TEXT NOT NULL CHECK (length(atom_id) = 40 AND atom_id GLOB 'atm_*'),
    atom_version_number INTEGER NOT NULL CHECK (atom_version_number > 0),
    project_id TEXT NOT NULL REFERENCES projects(id),
    semantic_scope TEXT NOT NULL CHECK (length(semantic_scope) BETWEEN 1 AND 256),
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
    canonical_name TEXT NOT NULL CHECK (length(canonical_name) BETWEEN 1 AND 256),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 512),
    normalized_phrase TEXT NOT NULL CHECK (length(normalized_phrase) BETWEEN 1 AND 512),
    rationale_nearest_alternative TEXT NOT NULL CHECK (length(rationale_nearest_alternative) BETWEEN 1 AND 2048),
    rationale_distinguishing_qualifier TEXT NOT NULL CHECK (length(rationale_distinguishing_qualifier) BETWEEN 1 AND 2048),
    revision_sequence INTEGER NOT NULL CHECK (revision_sequence >= 1),
    supersedes_revision_id TEXT REFERENCES atom_names(id),
    valid INTEGER NOT NULL CHECK (valid IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (atom_id, atom_version_number, revision_sequence),
    CHECK (
        (revision_sequence = 1 AND supersedes_revision_id IS NULL)
        OR (revision_sequence > 1 AND supersedes_revision_id IS NOT NULL)
    )
) STRICT;

CREATE INDEX atom_names_by_atom
    ON atom_names(atom_id, atom_version_number, revision_sequence DESC);
-- M21-153 "revision metadata": internal/atomname.AtomNameRecord's doc
-- comment requires "Exactly one AtomNameRecord per atom (and atom version)
-- should carry Valid == true at a time; making that swap transactional is
-- the storage lane's responsibility." This partial unique index is that
-- enforcement.
CREATE UNIQUE INDEX atom_names_one_valid_per_atom_version
    ON atom_names(atom_id, atom_version_number) WHERE valid = 1;
-- Supports both the M21-155 collision trigger below and M21-167-style name
-- search (not implemented by this lane).
CREATE INDEX atom_names_by_active_scope_phrase
    ON atom_names(project_id, semantic_scope, normalized_phrase) WHERE valid = 1;

CREATE TRIGGER atom_names_supersedes_immediate_prior
BEFORE INSERT ON atom_names
WHEN NEW.supersedes_revision_id IS NOT NULL
 AND NOT EXISTS (
     SELECT 1 FROM atom_names
     WHERE id = NEW.supersedes_revision_id AND atom_id = NEW.atom_id
       AND atom_version_number = NEW.atom_version_number
       AND revision_sequence = NEW.revision_sequence - 1
 )
BEGIN SELECT RAISE(ABORT, 'atom name revision does not immediately supersede its prior revision'); END;

-- M21-155: mirrors internal/atomname.DetectCanonicalNameCollision exactly --
-- same (project, semantic scope, normalized phrase) key, valid rows only,
-- and the candidate's own atom_id is excluded (DetectCanonicalNameCollision
-- takes excludeAtomID at the ATOM level, not atom+version level: two valid
-- names for two different VERSIONS of the SAME atom sharing a phrase is not
-- a collision, only a different atom's name is). A plain unique index
-- cannot express that self-exclusion, so this is a trigger rather than a
-- unique index.
CREATE TRIGGER atom_names_active_canonical_name_scope_collision
BEFORE INSERT ON atom_names
WHEN NEW.valid = 1
 AND EXISTS (
     SELECT 1 FROM atom_names
     WHERE valid = 1 AND project_id = NEW.project_id AND semantic_scope = NEW.semantic_scope
       AND normalized_phrase = NEW.normalized_phrase AND atom_id != NEW.atom_id
 )
BEGIN SELECT RAISE(ABORT, 'active canonical name collides within project and semantic scope'); END;

-- Reproduced-defect guard, symmetric with
-- atom_documentation_revisions_project_stable_per_atom above.
CREATE TRIGGER atom_names_project_stable_per_atom
BEFORE INSERT ON atom_names
WHEN EXISTS (
    SELECT 1 FROM atom_names WHERE atom_id = NEW.atom_id AND project_id != NEW.project_id
)
BEGIN SELECT RAISE(ABORT, 'atom name project must remain stable across all revisions of one atom'); END;

CREATE TRIGGER atom_names_project_matches_documentation
BEFORE INSERT ON atom_names
WHEN EXISTS (
    SELECT 1 FROM atom_documentation_revisions WHERE atom_id = NEW.atom_id AND project_id != NEW.project_id
)
BEGIN SELECT RAISE(ABORT, 'atom name project does not match this atom''s recorded documentation project'); END;

-- The symmetric direction of the check above, declared here (rather than
-- alongside atom_documentation_revisions' other triggers) because it
-- references atom_names, which SQLite requires to already exist at
-- CREATE TRIGGER time.
CREATE TRIGGER atom_documentation_revisions_project_matches_naming
BEFORE INSERT ON atom_documentation_revisions
WHEN EXISTS (
    SELECT 1 FROM atom_names WHERE atom_id = NEW.atom_id AND project_id != NEW.project_id
)
BEGIN SELECT RAISE(ABORT, 'atom documentation project does not match this atom''s recorded naming project'); END;

CREATE TRIGGER atom_names_immutable_content
BEFORE UPDATE ON atom_names
WHEN NEW.id != OLD.id OR NEW.atom_id != OLD.atom_id OR NEW.atom_version_number != OLD.atom_version_number
  OR NEW.project_id != OLD.project_id OR NEW.semantic_scope != OLD.semantic_scope
  OR NEW.schema_version != OLD.schema_version OR NEW.canonical_name != OLD.canonical_name
  OR NEW.display_name != OLD.display_name OR NEW.normalized_phrase != OLD.normalized_phrase
  OR NEW.rationale_nearest_alternative != OLD.rationale_nearest_alternative
  OR NEW.rationale_distinguishing_qualifier != OLD.rationale_distinguishing_qualifier
  OR NEW.revision_sequence != OLD.revision_sequence
  OR NEW.supersedes_revision_id IS NOT OLD.supersedes_revision_id
  OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'atom name revision content is immutable'); END;

CREATE TRIGGER atom_names_valid_one_way_transition
BEFORE UPDATE ON atom_names
WHEN NEW.valid != OLD.valid AND NOT (OLD.valid = 1 AND NEW.valid = 0)
BEGIN SELECT RAISE(ABORT, 'atom name validity may only transition from valid to invalid'); END;

CREATE TRIGGER atom_names_reject_delete
BEFORE DELETE ON atom_names
BEGIN SELECT RAISE(ABORT, 'atom name revisions are immutable'); END;

-- atom_name_aliases (M21-154): one row per alias activation event, mirroring
-- internal/atomname.AtomNameAliasRecord field-for-field. M21-156: a rename-
-- sourced alias is immutable lineage -- its text/atom/source/active-from can
-- never change, and its active interval can only close exactly once (never
-- reopen), so a prior canonical name preserved by a semantic-preserving
-- rename remains discoverable forever even after later renames.
CREATE TABLE atom_name_aliases (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    atom_id TEXT NOT NULL CHECK (length(atom_id) = 40 AND atom_id GLOB 'atm_*'),
    project_id TEXT NOT NULL REFERENCES projects(id),
    alias_text TEXT NOT NULL CHECK (length(alias_text) BETWEEN 1 AND 320),
    normalized_alias_text TEXT NOT NULL CHECK (length(normalized_alias_text) BETWEEN 1 AND 320),
    source TEXT NOT NULL CHECK (source IN ('rename', 'reviewed-manual-entry')),
    active_from_unix_micros INTEGER NOT NULL CHECK (active_from_unix_micros >= 0),
    active_until_unix_micros INTEGER CHECK (active_until_unix_micros >= active_from_unix_micros),
    invalidated_reason TEXT CHECK (invalidated_reason IS NULL OR length(invalidated_reason) BETWEEN 1 AND 2048),
    UNIQUE (atom_id, normalized_alias_text, active_from_unix_micros),
    CHECK ((active_until_unix_micros IS NULL) = (invalidated_reason IS NULL))
) STRICT;

CREATE INDEX atom_name_aliases_by_atom
    ON atom_name_aliases(atom_id, active_until_unix_micros);
-- M21-163-style active-alias lookup (not implemented by this lane): active
-- aliases only, by normalized text.
CREATE INDEX atom_name_aliases_by_normalized_text
    ON atom_name_aliases(normalized_alias_text, active_until_unix_micros);

-- M21-156: a rename-sourced alias must actually preserve a real canonical
-- name this atom has held, not an arbitrary string.
CREATE TRIGGER atom_name_aliases_rename_matches_canonical_name
BEFORE INSERT ON atom_name_aliases
WHEN NEW.source = 'rename'
 AND NOT EXISTS (
     SELECT 1 FROM atom_names WHERE atom_id = NEW.atom_id AND canonical_name = NEW.alias_text
 )
BEGIN SELECT RAISE(ABORT, 'rename alias does not match a recorded canonical name for this atom'); END;

-- Project boundary: no atoms/atom_versions identity table exists yet (see
-- the top-of-file note), so atom_names is the only available source of
-- "this atom belongs to project X"; an alias must land in a project this
-- atom has actually been named under.
CREATE TRIGGER atom_name_aliases_project_boundary
BEFORE INSERT ON atom_name_aliases
WHEN NOT EXISTS (
    SELECT 1 FROM atom_names WHERE atom_id = NEW.atom_id AND project_id = NEW.project_id
)
BEGIN SELECT RAISE(ABORT, 'atom name alias crosses the owning project boundary'); END;

CREATE TRIGGER atom_name_aliases_immutable_content
BEFORE UPDATE ON atom_name_aliases
WHEN NEW.id != OLD.id OR NEW.atom_id != OLD.atom_id OR NEW.project_id != OLD.project_id
  OR NEW.alias_text != OLD.alias_text OR NEW.normalized_alias_text != OLD.normalized_alias_text
  OR NEW.source != OLD.source OR NEW.active_from_unix_micros != OLD.active_from_unix_micros
BEGIN SELECT RAISE(ABORT, 'atom name alias identity is immutable'); END;

CREATE TRIGGER atom_name_aliases_active_interval_one_way
BEFORE UPDATE ON atom_name_aliases
WHEN OLD.active_until_unix_micros IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'atom name alias active interval is closed and immutable'); END;

CREATE TRIGGER atom_name_aliases_reject_delete
BEFORE DELETE ON atom_name_aliases
BEGIN SELECT RAISE(ABORT, 'atom name aliases are immutable lineage'); END;

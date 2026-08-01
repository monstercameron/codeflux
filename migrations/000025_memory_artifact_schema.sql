-- Milestone 21 (M21-014..027) project-memory schema: memory artifact
-- identity, immutable revisions, governed maturity, project/repository
-- binding, supporting evidence, derived_from/influenced_by influence
-- lineage, invalidation/quarantine records, applicability-predicate
-- records, the task-fingerprint schema-version registry, vector-model
-- metadata and revision-bound vector rows (schema only; no embedding
-- generation, similarity search, or ranking), and retrieval-candidate and
-- retrieval-decision logs (schema only; no retrieval gate).
--
-- Content is authoritative and immutable per revision; a mutable current
-- maturity column plus an append-only transition log form the
-- claim-status overlay described in docs/plan.md §31 "Historical Claim
-- Invalidation". Every table that reaches a memory artifact enforces the
-- owning project boundary with a trigger, per AGENTS.md "Add explicit
-- project-boundary predicates to memory, graph, vector, and retrieval
-- queries."

CREATE TABLE memory_artifacts (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN (
        'repository-fact', 'reviewed-command', 'file-to-test-mapping',
        'repository-convention', 'accepted-regression-case',
        'execution-recipe', 'executable-atom-reference',
        'observation-hypothesis'
    )),
    project_id TEXT NOT NULL REFERENCES projects(id),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    deleted_at_unix_micros INTEGER CHECK (deleted_at_unix_micros >= created_at_unix_micros),
    deletion_reason_redacted TEXT CHECK (
        deletion_reason_redacted IS NULL OR length(deletion_reason_redacted) BETWEEN 1 AND 2048
    ),
    CHECK ((deleted_at_unix_micros IS NULL) = (deletion_reason_redacted IS NULL))
) STRICT;

CREATE INDEX memory_artifacts_by_project
    ON memory_artifacts(project_id, kind, deleted_at_unix_micros);

-- memory_artifact_revisions holds immutable content per revision (M21-015)
-- plus the mutable current maturity overlay (M21-016) and the
-- repository-revision binding used to query facts without parsing JSON
-- (M21-017). scope_repository_id mirrors domain.MemoryProjectScope's
-- optional repository; content_repository_id mirrors the Repository field
-- every typed content payload carries.
CREATE TABLE memory_artifact_revisions (
    id TEXT PRIMARY KEY,
    artifact_id TEXT NOT NULL REFERENCES memory_artifacts(id),
    revision_number INTEGER NOT NULL CHECK (revision_number >= 1),
    kind TEXT NOT NULL CHECK (kind IN (
        'repository-fact', 'reviewed-command', 'file-to-test-mapping',
        'repository-convention', 'accepted-regression-case',
        'execution-recipe', 'executable-atom-reference',
        'observation-hypothesis'
    )),
    scope_repository_id TEXT REFERENCES repositories(id),
    content_repository_id TEXT NOT NULL REFERENCES repositories(id),
    content_json TEXT NOT NULL CHECK (json_valid(content_json)),
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64 AND content_sha256 NOT GLOB '*[^0-9a-f]*'),
    binding_known INTEGER CHECK (binding_known IN (0, 1)),
    binding_exact_revision TEXT CHECK (binding_exact_revision IS NULL OR length(binding_exact_revision) BETWEEN 1 AND 255),
    binding_valid_from_revision TEXT CHECK (binding_valid_from_revision IS NULL OR length(binding_valid_from_revision) BETWEEN 1 AND 255),
    binding_valid_until_revision TEXT CHECK (binding_valid_until_revision IS NULL OR length(binding_valid_until_revision) BETWEEN 1 AND 255),
    binding_unknown_reason TEXT CHECK (binding_unknown_reason IS NULL OR length(binding_unknown_reason) BETWEEN 1 AND 2048),
    maturity TEXT NOT NULL DEFAULT 'candidate' CHECK (maturity IN (
        'candidate', 'validated', 'preferred-for-experiment',
        'quarantined', 'invalidated', 'retired'
    )),
    supersedes_revision_id TEXT REFERENCES memory_artifact_revisions(id),
    created_from_correction INTEGER NOT NULL CHECK (created_from_correction IN (0, 1)),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (artifact_id, revision_number),
    UNIQUE (artifact_id, idempotency_key),
    CHECK (
        (kind IN ('execution-recipe', 'observation-hypothesis')
            AND binding_known IS NULL AND binding_exact_revision IS NULL
            AND binding_valid_from_revision IS NULL AND binding_valid_until_revision IS NULL
            AND binding_unknown_reason IS NULL)
        OR
        (kind NOT IN ('execution-recipe', 'observation-hypothesis') AND binding_known IS NOT NULL)
    ),
    CHECK (
        binding_known IS NULL
        OR (binding_known = 0 AND binding_exact_revision IS NULL AND binding_valid_from_revision IS NULL
            AND binding_valid_until_revision IS NULL AND binding_unknown_reason IS NOT NULL)
        OR (binding_known = 1 AND binding_unknown_reason IS NULL
            AND (binding_exact_revision IS NOT NULL OR binding_valid_from_revision IS NOT NULL))
    ),
    CHECK (
        (supersedes_revision_id IS NULL AND revision_number = 1 AND created_from_correction = 0)
        OR (supersedes_revision_id IS NOT NULL AND revision_number > 1 AND created_from_correction = 1)
    )
) STRICT;

CREATE INDEX memory_artifact_revisions_by_artifact
    ON memory_artifact_revisions(artifact_id, revision_number DESC);
CREATE INDEX memory_artifact_revisions_by_maturity
    ON memory_artifact_revisions(maturity, content_repository_id);

CREATE TRIGGER memory_artifact_revisions_kind_matches_artifact
BEFORE INSERT ON memory_artifact_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM memory_artifacts
    WHERE id = NEW.artifact_id AND kind = NEW.kind AND deleted_at_unix_micros IS NULL
)
BEGIN SELECT RAISE(ABORT, 'memory artifact revision kind does not match its artifact'); END;

CREATE TRIGGER memory_artifact_revisions_content_repository_project_boundary
BEFORE INSERT ON memory_artifact_revisions
WHEN (SELECT project_id FROM repositories WHERE id = NEW.content_repository_id)
     != (SELECT project_id FROM memory_artifacts WHERE id = NEW.artifact_id)
BEGIN SELECT RAISE(ABORT, 'memory artifact content repository crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_revisions_scope_repository_project_boundary
BEFORE INSERT ON memory_artifact_revisions
WHEN NEW.scope_repository_id IS NOT NULL
 AND (SELECT project_id FROM repositories WHERE id = NEW.scope_repository_id)
     != (SELECT project_id FROM memory_artifacts WHERE id = NEW.artifact_id)
BEGIN SELECT RAISE(ABORT, 'memory artifact scope repository crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_revisions_supersedes_same_artifact
BEFORE INSERT ON memory_artifact_revisions
WHEN NEW.supersedes_revision_id IS NOT NULL
 AND NOT EXISTS (
     SELECT 1 FROM memory_artifact_revisions
     WHERE id = NEW.supersedes_revision_id AND artifact_id = NEW.artifact_id
       AND revision_number = NEW.revision_number - 1
 )
BEGIN SELECT RAISE(ABORT, 'memory artifact revision does not immediately supersede its prior revision'); END;

-- Content, kind, binding, lineage anchors, and identity never change once
-- written; only the current maturity overlay may move, and only when a
-- corresponding transition row (inserted first, in the same transaction)
-- already recorded that exact step. This is the storage-layer mechanism
-- for AGENTS.md "Preserve immutable historical records; represent later
-- correction through revisions or invalidation overlays."
CREATE TRIGGER memory_artifact_revisions_immutable_content
BEFORE UPDATE ON memory_artifact_revisions
WHEN NEW.artifact_id != OLD.artifact_id
  OR NEW.revision_number != OLD.revision_number
  OR NEW.kind != OLD.kind
  OR NEW.scope_repository_id IS NOT OLD.scope_repository_id
  OR NEW.content_repository_id != OLD.content_repository_id
  OR NEW.content_json != OLD.content_json
  OR NEW.content_sha256 != OLD.content_sha256
  OR NEW.binding_known IS NOT OLD.binding_known
  OR NEW.binding_exact_revision IS NOT OLD.binding_exact_revision
  OR NEW.binding_valid_from_revision IS NOT OLD.binding_valid_from_revision
  OR NEW.binding_valid_until_revision IS NOT OLD.binding_valid_until_revision
  OR NEW.binding_unknown_reason IS NOT OLD.binding_unknown_reason
  OR NEW.supersedes_revision_id IS NOT OLD.supersedes_revision_id
  OR NEW.created_from_correction != OLD.created_from_correction
  OR NEW.idempotency_key != OLD.idempotency_key
  OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'memory artifact revision content is immutable'); END;

CREATE TRIGGER memory_artifact_revisions_reject_delete
BEFORE DELETE ON memory_artifact_revisions
BEGIN SELECT RAISE(ABORT, 'memory artifact revisions are immutable'); END;

-- memory_artifact_supporting_evidence (M21-018) links one revision to one
-- piece of evidence with the exact source/strength shape validated by
-- domain.SupportingEvidenceRecord.Validate. Declared before
-- memory_artifact_maturity_transitions because that table's
-- require-corroborated-evidence trigger reads it.
CREATE TABLE memory_artifact_supporting_evidence (
    revision_id TEXT NOT NULL REFERENCES memory_artifact_revisions(id),
    evidence_id TEXT NOT NULL REFERENCES evidence(id),
    source TEXT NOT NULL CHECK (source IN (
        'agent-self-report', 'automated-validation-run', 'user-approval',
        'independent-replay-comparison', 'mechanical-rule-check', 'regression-oracle-run'
    )),
    strength TEXT NOT NULL CHECK (strength IN ('none', 'corroborated')),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    PRIMARY KEY (revision_id, evidence_id),
    CHECK (NOT (source = 'agent-self-report' AND strength != 'none'))
) STRICT, WITHOUT ROWID;

CREATE INDEX memory_artifact_supporting_evidence_by_evidence
    ON memory_artifact_supporting_evidence(evidence_id);

CREATE TRIGGER memory_artifact_supporting_evidence_project_boundary
BEFORE INSERT ON memory_artifact_supporting_evidence
WHEN (
    SELECT thread.project_id FROM evidence
    JOIN tasks AS task ON task.id = evidence.task_id
    JOIN threads AS thread ON thread.id = task.thread_id
    WHERE evidence.id = NEW.evidence_id
) != (
    SELECT memory_artifacts.project_id
    FROM memory_artifact_revisions
    JOIN memory_artifacts ON memory_artifacts.id = memory_artifact_revisions.artifact_id
    WHERE memory_artifact_revisions.id = NEW.revision_id
)
BEGIN SELECT RAISE(ABORT, 'memory artifact supporting evidence crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_supporting_evidence_immutable_update
BEFORE UPDATE ON memory_artifact_supporting_evidence
BEGIN SELECT RAISE(ABORT, 'memory artifact supporting evidence links are immutable'); END;
CREATE TRIGGER memory_artifact_supporting_evidence_immutable_delete
BEFORE DELETE ON memory_artifact_supporting_evidence
BEGIN SELECT RAISE(ABORT, 'memory artifact supporting evidence links are immutable'); END;

-- memory_artifact_maturity_transitions is the append-only governed-maturity
-- log (M21-016) that doubles as the invalidation/quarantine record
-- (M21-021): every transition into a terminal-for-authority state
-- (quarantined, invalidated, retired) must carry a reason and detail, and
-- may reference the counterexample evidence retained for an independently
-- derived successor per docs/plan.md §31 "Artifact Failure Protocol". The
-- from/to CHECK below is the exact state machine encoded in
-- internal/domain/memory.go's maturityTransitions table; because it omits
-- every pair with from_state = 'quarantined' other than
-- quarantined -> invalidated/retired, no row can ever restore inherited
-- authority to a quarantined revision (M21-021 terminality).
CREATE TABLE memory_artifact_maturity_transitions (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    revision_id TEXT NOT NULL REFERENCES memory_artifact_revisions(id),
    from_state TEXT NOT NULL CHECK (from_state IN (
        'candidate', 'validated', 'preferred-for-experiment', 'quarantined'
    )),
    to_state TEXT NOT NULL CHECK (to_state IN (
        'validated', 'preferred-for-experiment', 'quarantined', 'invalidated', 'retired'
    )),
    reason_kind TEXT CHECK (reason_kind IS NULL OR reason_kind IN (
        'lesson-arm-worse', 'both-arms-bad', 'adaptation-failed',
        'binding-changed', 'evidence-ambiguous', 'user-correction',
        'deployed-workflow-failure', 'other'
    )),
    detail_redacted TEXT CHECK (detail_redacted IS NULL OR length(detail_redacted) BETWEEN 1 AND 8192),
    counterexample_evidence_id TEXT REFERENCES evidence(id),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    transitioned_at_unix_micros INTEGER NOT NULL CHECK (transitioned_at_unix_micros >= 0),
    UNIQUE (revision_id, idempotency_key),
    CHECK (
        (from_state = 'candidate' AND to_state IN ('validated', 'preferred-for-experiment', 'quarantined', 'invalidated', 'retired'))
        OR (from_state = 'validated' AND to_state IN ('preferred-for-experiment', 'quarantined', 'invalidated', 'retired'))
        OR (from_state = 'preferred-for-experiment' AND to_state IN ('validated', 'quarantined', 'invalidated', 'retired'))
        OR (from_state = 'quarantined' AND to_state IN ('invalidated', 'retired'))
    ),
    CHECK (
        (to_state IN ('quarantined', 'invalidated', 'retired') AND reason_kind IS NOT NULL AND detail_redacted IS NOT NULL)
        OR (to_state NOT IN ('quarantined', 'invalidated', 'retired'))
    )
) STRICT;

CREATE INDEX memory_artifact_maturity_transitions_by_revision
    ON memory_artifact_maturity_transitions(revision_id, transitioned_at_unix_micros);

CREATE TRIGGER memory_artifact_maturity_transitions_current_state_matches
BEFORE INSERT ON memory_artifact_maturity_transitions
WHEN NOT EXISTS (
    SELECT 1 FROM memory_artifact_revisions WHERE id = NEW.revision_id AND maturity = NEW.from_state
)
BEGIN SELECT RAISE(ABORT, 'memory artifact maturity transition does not match the current recorded maturity'); END;

-- Model self-report can never authorize Validated or PreferredForExperiment
-- (M21-011); this trigger is the storage-layer defense-in-depth mirror of
-- domain.AuthorizeMemoryArtifactMaturityGrant.
CREATE TRIGGER memory_artifact_maturity_transitions_require_corroborated_evidence
BEFORE INSERT ON memory_artifact_maturity_transitions
WHEN NEW.to_state IN ('validated', 'preferred-for-experiment')
 AND NOT EXISTS (
     SELECT 1 FROM memory_artifact_supporting_evidence
     WHERE revision_id = NEW.revision_id AND source != 'agent-self-report' AND strength = 'corroborated'
 )
BEGIN SELECT RAISE(ABORT, 'memory artifact maturity grant requires corroborated non-self-report evidence'); END;

-- Reproduced defect: without this trigger, a transition row for a
-- Project-A revision accepted a counterexample_evidence_id pointing at
-- Project-B's evidence with no error, unlike every other table that
-- reaches a memory artifact (see e.g.
-- memory_artifact_supporting_evidence_project_boundary above), which
-- enforces its project boundary with a trigger, per AGENTS.md "Add
-- explicit project-boundary predicates to memory, graph, vector, and
-- retrieval queries." This is the storage-layer defense-in-depth mirror
-- of the Go-side memoryArtifactRevisionProjectID/memoryEvidenceProjectID
-- check in internal/storage/memory_maturity_repository.go.
CREATE TRIGGER memory_artifact_maturity_transitions_counterexample_project_boundary
BEFORE INSERT ON memory_artifact_maturity_transitions
WHEN NEW.counterexample_evidence_id IS NOT NULL
 AND (
     SELECT thread.project_id FROM evidence
     JOIN tasks AS task ON task.id = evidence.task_id
     JOIN threads AS thread ON thread.id = task.thread_id
     WHERE evidence.id = NEW.counterexample_evidence_id
 ) != (
     SELECT memory_artifacts.project_id
     FROM memory_artifact_revisions
     JOIN memory_artifacts ON memory_artifacts.id = memory_artifact_revisions.artifact_id
     WHERE memory_artifact_revisions.id = NEW.revision_id
 )
BEGIN SELECT RAISE(ABORT, 'memory artifact maturity transition counterexample evidence crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_maturity_transitions_immutable_update
BEFORE UPDATE ON memory_artifact_maturity_transitions
BEGIN SELECT RAISE(ABORT, 'memory artifact maturity transitions are immutable'); END;
CREATE TRIGGER memory_artifact_maturity_transitions_immutable_delete
BEFORE DELETE ON memory_artifact_maturity_transitions
BEGIN SELECT RAISE(ABORT, 'memory artifact maturity transitions are immutable'); END;

-- Declared only now that memory_artifact_maturity_transitions exists: a
-- maturity change on a revision is accepted only when a matching transition
-- row was already logged in the same transaction.
CREATE TRIGGER memory_artifact_revisions_maturity_requires_logged_transition
BEFORE UPDATE ON memory_artifact_revisions
WHEN NEW.maturity != OLD.maturity
 AND NOT EXISTS (
     SELECT 1 FROM memory_artifact_maturity_transitions
     WHERE revision_id = NEW.id AND from_state = OLD.maturity AND to_state = NEW.maturity
 )
BEGIN SELECT RAISE(ABORT, 'memory artifact maturity change has no matching logged transition'); END;

-- derived_from (M21-019) is semantic dependency: the descendant's claim
-- mechanically depends on the ancestor's claim, so a failure in the
-- ancestor automatically quarantines direct dependents (§31 "Descendant
-- Contamination"). influenced_by (M21-020) is contextual exposure only: an
-- ancestor entered the agent's context, joining its evidence lineage,
-- without the descendant mechanically depending on it. The two relations
-- carry different consequences (automatic quarantine vs. flag-only) and
-- are kept as separate tables rather than one table with a flag so a
-- future cascading-quarantine trigger can target derived_from alone
-- without an extra predicate.
CREATE TABLE memory_artifact_derived_from (
    artifact_id TEXT NOT NULL REFERENCES memory_artifacts(id),
    ancestor_artifact_id TEXT NOT NULL REFERENCES memory_artifacts(id),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    PRIMARY KEY (artifact_id, ancestor_artifact_id),
    CHECK (artifact_id != ancestor_artifact_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE memory_artifact_influenced_by (
    artifact_id TEXT NOT NULL REFERENCES memory_artifacts(id),
    ancestor_artifact_id TEXT NOT NULL REFERENCES memory_artifacts(id),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    PRIMARY KEY (artifact_id, ancestor_artifact_id),
    CHECK (artifact_id != ancestor_artifact_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX memory_artifact_derived_from_by_ancestor
    ON memory_artifact_derived_from(ancestor_artifact_id);
CREATE INDEX memory_artifact_influenced_by_by_ancestor
    ON memory_artifact_influenced_by(ancestor_artifact_id);

CREATE TRIGGER memory_artifact_derived_from_project_boundary
BEFORE INSERT ON memory_artifact_derived_from
WHEN (SELECT project_id FROM memory_artifacts WHERE id = NEW.artifact_id)
     != (SELECT project_id FROM memory_artifacts WHERE id = NEW.ancestor_artifact_id)
BEGIN SELECT RAISE(ABORT, 'derived_from lineage crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_derived_from_not_also_influenced_by
BEFORE INSERT ON memory_artifact_derived_from
WHEN EXISTS (
    SELECT 1 FROM memory_artifact_influenced_by
    WHERE artifact_id = NEW.artifact_id AND ancestor_artifact_id = NEW.ancestor_artifact_id
)
BEGIN SELECT RAISE(ABORT, 'memory artifact cannot be both derived_from and influenced_by the same ancestor'); END;

CREATE TRIGGER memory_artifact_influenced_by_project_boundary
BEFORE INSERT ON memory_artifact_influenced_by
WHEN (SELECT project_id FROM memory_artifacts WHERE id = NEW.artifact_id)
     != (SELECT project_id FROM memory_artifacts WHERE id = NEW.ancestor_artifact_id)
BEGIN SELECT RAISE(ABORT, 'influenced_by lineage crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_influenced_by_not_also_derived_from
BEFORE INSERT ON memory_artifact_influenced_by
WHEN EXISTS (
    SELECT 1 FROM memory_artifact_derived_from
    WHERE artifact_id = NEW.artifact_id AND ancestor_artifact_id = NEW.ancestor_artifact_id
)
BEGIN SELECT RAISE(ABORT, 'memory artifact cannot be both influenced_by and derived_from the same ancestor'); END;

CREATE TRIGGER memory_artifact_derived_from_immutable_update
BEFORE UPDATE ON memory_artifact_derived_from
BEGIN SELECT RAISE(ABORT, 'derived_from lineage is immutable'); END;
CREATE TRIGGER memory_artifact_derived_from_immutable_delete
BEFORE DELETE ON memory_artifact_derived_from
BEGIN SELECT RAISE(ABORT, 'derived_from lineage is immutable'); END;
CREATE TRIGGER memory_artifact_influenced_by_immutable_update
BEFORE UPDATE ON memory_artifact_influenced_by
BEGIN SELECT RAISE(ABORT, 'influenced_by lineage is immutable'); END;
CREATE TRIGGER memory_artifact_influenced_by_immutable_delete
BEFORE DELETE ON memory_artifact_influenced_by
BEGIN SELECT RAISE(ABORT, 'influenced_by lineage is immutable'); END;

-- task_fingerprint_schema_versions (M21-023) is a registry of known
-- fingerprint-schema version numbers so applicability predicates and
-- retrieval logs can bind to a specific version, per docs/plan.md §31
-- "Every predicate declares its expected fingerprint-schema version...
-- Incompatible schema versions never match silently." The actual
-- fingerprint field layout is defined by the M21-051..063 lane; this table
-- only tracks which version numbers exist.
CREATE TABLE task_fingerprint_schema_versions (
    version INTEGER PRIMARY KEY CHECK (version >= 1),
    description TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 2048),
    introduced_at_unix_micros INTEGER NOT NULL CHECK (introduced_at_unix_micros >= 0)
) STRICT;

INSERT INTO task_fingerprint_schema_versions (version, description, introduced_at_unix_micros)
VALUES (1, 'Reserved for the M21-051 versioned task fingerprint schema (schema version 1).', 0);

-- memory_artifact_applicability_predicates (M21-022) stores structured,
-- machine-checkable applicability predicates distinct from the free-text
-- ApplicabilityStatement carried inside execution-recipe content. Schema
-- only: deterministic predicate evaluation is the M21-064..077 retrieval
-- gate's concern, not this lane's.
CREATE TABLE memory_artifact_applicability_predicates (
    id TEXT PRIMARY KEY,
    revision_id TEXT NOT NULL REFERENCES memory_artifact_revisions(id),
    fingerprint_schema_version INTEGER NOT NULL REFERENCES task_fingerprint_schema_versions(version),
    predicate_kind TEXT NOT NULL CHECK (predicate_kind IN (
        'repository-match', 'dependency-binding', 'capability-requirement',
        'effect-requirement', 'path-scope', 'custom'
    )),
    predicate_json TEXT NOT NULL CHECK (json_valid(predicate_json)),
    required_fields_json TEXT NOT NULL CHECK (json_valid(required_fields_json)),
    unknown_field_behavior TEXT NOT NULL CHECK (unknown_field_behavior IN ('reject', 'skip', 'warn')),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0)
) STRICT;

CREATE INDEX memory_artifact_applicability_predicates_by_revision
    ON memory_artifact_applicability_predicates(revision_id);

CREATE TRIGGER memory_artifact_applicability_predicates_immutable_update
BEFORE UPDATE ON memory_artifact_applicability_predicates
BEGIN SELECT RAISE(ABORT, 'memory artifact applicability predicates are immutable'); END;
CREATE TRIGGER memory_artifact_applicability_predicates_immutable_delete
BEFORE DELETE ON memory_artifact_applicability_predicates
BEGIN SELECT RAISE(ABORT, 'memory artifact applicability predicates are immutable'); END;

-- Vector storage (M21-024, M21-025). Schema and metadata only: no
-- embedding generation, similarity search, ranking, or vector runtime
-- (that is M21-078..089, gated behind docs/plan.md §0 and not started
-- here). Field shapes follow docs/plan.md §23 "Vector Storage": provider,
-- model, and model version; dimensions, numeric encoding, and
-- normalization; source-content hash; creation time and validity state;
-- project and security scope.
CREATE TABLE memory_embedding_models (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    provider TEXT NOT NULL CHECK (length(provider) BETWEEN 1 AND 255),
    model_name TEXT NOT NULL CHECK (length(model_name) BETWEEN 1 AND 255),
    model_version TEXT NOT NULL CHECK (length(model_version) BETWEEN 1 AND 255),
    dimensions INTEGER NOT NULL CHECK (dimensions > 0),
    numeric_encoding TEXT NOT NULL CHECK (numeric_encoding IN ('float32', 'float16', 'int8')),
    normalization TEXT NOT NULL CHECK (normalization IN ('none', 'l2')),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (provider, model_name, model_version)
) STRICT;

CREATE TABLE memory_embedding_spaces (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    embedding_model_id TEXT NOT NULL REFERENCES memory_embedding_models(id),
    input_schema_version INTEGER NOT NULL CHECK (input_schema_version >= 1),
    project_id TEXT NOT NULL REFERENCES projects(id),
    security_scope TEXT NOT NULL CHECK (security_scope IN ('project-local', 'cross-project-reviewed')),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (embedding_model_id, input_schema_version, project_id)
) STRICT;

CREATE TABLE memory_artifact_embeddings (
    id TEXT PRIMARY KEY,
    revision_id TEXT NOT NULL REFERENCES memory_artifact_revisions(id),
    embedding_space_id TEXT NOT NULL REFERENCES memory_embedding_spaces(id),
    source_content_sha256 TEXT NOT NULL CHECK (length(source_content_sha256) = 64 AND source_content_sha256 NOT GLOB '*[^0-9a-f]*'),
    vector BLOB NOT NULL CHECK (length(vector) > 0),
    valid INTEGER NOT NULL DEFAULT 1 CHECK (valid IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    invalidated_at_unix_micros INTEGER CHECK (invalidated_at_unix_micros >= created_at_unix_micros),
    UNIQUE (revision_id, embedding_space_id),
    CHECK ((valid = 1) = (invalidated_at_unix_micros IS NULL))
) STRICT;

CREATE INDEX memory_artifact_embeddings_by_space
    ON memory_artifact_embeddings(embedding_space_id, valid);

CREATE TRIGGER memory_artifact_embeddings_project_boundary
BEFORE INSERT ON memory_artifact_embeddings
WHEN (SELECT project_id FROM memory_embedding_spaces WHERE id = NEW.embedding_space_id)
     != (SELECT project_id FROM memory_artifacts
          WHERE id = (SELECT artifact_id FROM memory_artifact_revisions WHERE id = NEW.revision_id))
BEGIN SELECT RAISE(ABORT, 'memory artifact embedding crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_embeddings_immutable_content
BEFORE UPDATE ON memory_artifact_embeddings
WHEN NEW.revision_id != OLD.revision_id
  OR NEW.embedding_space_id != OLD.embedding_space_id
  OR NEW.source_content_sha256 != OLD.source_content_sha256
  OR NEW.vector != OLD.vector
  OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'memory artifact embedding content is immutable'); END;
CREATE TRIGGER memory_artifact_embeddings_immutable_delete
BEFORE DELETE ON memory_artifact_embeddings
BEGIN SELECT RAISE(ABORT, 'memory artifact embeddings are logically invalidated, never deleted'); END;

-- embedding_index_state (per docs/plan.md §23) tracks whether an
-- approximate-nearest-neighbor index is rebuildable/ready for one
-- embedding space. No index runtime is implemented here.
CREATE TABLE memory_embedding_index_state (
    embedding_space_id TEXT PRIMARY KEY REFERENCES memory_embedding_spaces(id),
    index_state TEXT NOT NULL CHECK (index_state IN ('absent', 'building', 'ready', 'stale')),
    built_from_revision_count INTEGER NOT NULL DEFAULT 0 CHECK (built_from_revision_count >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

-- Retrieval-candidate and retrieval-decision logs (M21-026): schema only,
-- for the M21-064..077 retrieval gate to write into later. Vector
-- similarity never establishes eligibility by itself (AGENTS.md); it is
-- recorded only as one possible candidate_source alongside exact-match and
-- applicability-pass sources.
CREATE TABLE memory_retrieval_queries (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    task_id TEXT REFERENCES tasks(id),
    fingerprint_schema_version INTEGER NOT NULL REFERENCES task_fingerprint_schema_versions(version),
    query_kind TEXT NOT NULL CHECK (query_kind IN ('exact-identity', 'applicability-filter', 'vector-similarity')),
    requested_at_unix_micros INTEGER NOT NULL CHECK (requested_at_unix_micros >= 0)
) STRICT;

CREATE TABLE memory_retrieval_candidates (
    id TEXT PRIMARY KEY,
    query_id TEXT NOT NULL REFERENCES memory_retrieval_queries(id),
    revision_id TEXT NOT NULL REFERENCES memory_artifact_revisions(id),
    rank INTEGER NOT NULL CHECK (rank >= 1),
    candidate_source TEXT NOT NULL CHECK (candidate_source IN ('exact-match', 'applicability-pass', 'vector-similarity')),
    similarity_score REAL CHECK (similarity_score IS NULL OR (similarity_score >= -1.0 AND similarity_score <= 1.0)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (query_id, revision_id),
    CHECK ((candidate_source = 'vector-similarity') = (similarity_score IS NOT NULL))
) STRICT;

CREATE INDEX memory_retrieval_candidates_by_query
    ON memory_retrieval_candidates(query_id, rank);
CREATE INDEX memory_retrieval_candidates_by_revision
    ON memory_retrieval_candidates(revision_id);

CREATE TRIGGER memory_retrieval_candidates_project_boundary
BEFORE INSERT ON memory_retrieval_candidates
WHEN (SELECT project_id FROM memory_retrieval_queries WHERE id = NEW.query_id)
     != (SELECT project_id FROM memory_artifacts
          WHERE id = (SELECT artifact_id FROM memory_artifact_revisions WHERE id = NEW.revision_id))
BEGIN SELECT RAISE(ABORT, 'memory retrieval candidate crosses the query project boundary'); END;

CREATE TABLE memory_retrieval_decisions (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL UNIQUE REFERENCES memory_retrieval_candidates(id),
    decision TEXT NOT NULL CHECK (decision IN ('accepted', 'rejected')),
    reason_kind TEXT NOT NULL CHECK (reason_kind IN (
        'project-boundary-mismatch', 'toolchain-mismatch', 'dependency-mismatch',
        'invalidated-evidence', 'assurance-below-requirement',
        'applicability-predicate-failed', 'eligible-and-used',
        'eligible-and-adapted', 'eligible-and-rejected-by-agent', 'no-eligible-item'
    )),
    reason_detail_redacted TEXT CHECK (reason_detail_redacted IS NULL OR length(reason_detail_redacted) BETWEEN 1 AND 2048),
    decided_at_unix_micros INTEGER NOT NULL CHECK (decided_at_unix_micros >= 0),
    CHECK (
        (decision = 'accepted' AND reason_kind IN ('eligible-and-used', 'eligible-and-adapted'))
        OR (decision = 'rejected' AND reason_kind NOT IN ('eligible-and-used', 'eligible-and-adapted'))
    )
) STRICT;

CREATE TRIGGER memory_retrieval_queries_immutable_update BEFORE UPDATE ON memory_retrieval_queries
BEGIN SELECT RAISE(ABORT, 'memory retrieval queries are immutable'); END;
CREATE TRIGGER memory_retrieval_queries_immutable_delete BEFORE DELETE ON memory_retrieval_queries
BEGIN SELECT RAISE(ABORT, 'memory retrieval queries are immutable'); END;
CREATE TRIGGER memory_retrieval_candidates_immutable_update BEFORE UPDATE ON memory_retrieval_candidates
BEGIN SELECT RAISE(ABORT, 'memory retrieval candidates are immutable'); END;
CREATE TRIGGER memory_retrieval_candidates_immutable_delete BEFORE DELETE ON memory_retrieval_candidates
BEGIN SELECT RAISE(ABORT, 'memory retrieval candidates are immutable'); END;
CREATE TRIGGER memory_retrieval_decisions_immutable_update BEFORE UPDATE ON memory_retrieval_decisions
BEGIN SELECT RAISE(ABORT, 'memory retrieval decisions are immutable'); END;
CREATE TRIGGER memory_retrieval_decisions_immutable_delete BEFORE DELETE ON memory_retrieval_decisions
BEGIN SELECT RAISE(ABORT, 'memory retrieval decisions are immutable'); END;

-- Cascading logical deletion (M21-028): memory_artifacts.deleted_at_unix_
-- micros is the only deletion mechanism. There is no hard DELETE path for
-- memory_artifacts, so a deleted artifact's revisions, lineage, evidence
-- links, and embeddings remain in place as history; application code
-- (Repositories.DeleteMemoryArtifact) walks derived_from edges to cascade
-- the logical deletion to direct semantic dependents, matching
-- docs/plan.md §31 "Direct semantic dependents are quarantined
-- automatically." Because every lineage edge is project-boundary checked
-- above, this cascade can never cross a project.
CREATE TRIGGER memory_artifacts_immutable_identity
BEFORE UPDATE ON memory_artifacts
WHEN NEW.id != OLD.id OR NEW.kind != OLD.kind OR NEW.project_id != OLD.project_id
  OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'memory artifact identity is immutable'); END;
CREATE TRIGGER memory_artifacts_reject_delete
BEFORE DELETE ON memory_artifacts
BEGIN SELECT RAISE(ABORT, 'memory artifacts are logically deleted, never removed'); END;

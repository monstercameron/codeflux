-- Revision-bound, redacted, explainable context selections. Repository source
-- remains authoritative; these rows record exactly what was admitted to one
-- provider-context assembly and why.

CREATE TABLE context_manifests (
    id TEXT PRIMARY KEY CHECK (
        length(id) = 64
        AND id NOT GLOB '*[^0-9a-f]*'
    ),
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    repository_revision TEXT NOT NULL CHECK (
        length(repository_revision) IN (40, 64)
        AND repository_revision NOT GLOB '*[^0-9a-f]*'
    ),
    map_revision TEXT NOT NULL CHECK (
        length(map_revision) = 64
        AND map_revision NOT GLOB '*[^0-9a-f]*'
    ),
    requirement_sha256 TEXT NOT NULL CHECK (
        length(requirement_sha256) = 64
        AND requirement_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    selection_policy_version INTEGER NOT NULL CHECK (
        selection_policy_version > 0
    ),
    max_files INTEGER NOT NULL CHECK (max_files BETWEEN 1 AND 200),
    max_bytes INTEGER NOT NULL CHECK (max_bytes BETWEEN 1024 AND 8388608),
    max_estimated_tokens INTEGER NOT NULL CHECK (
        max_estimated_tokens BETWEEN 256 AND 2000000
    ),
    used_files INTEGER NOT NULL CHECK (
        used_files BETWEEN 0 AND max_files
    ),
    used_bytes INTEGER NOT NULL CHECK (
        used_bytes BETWEEN 0 AND max_bytes
    ),
    used_estimated_tokens INTEGER NOT NULL CHECK (
        used_estimated_tokens BETWEEN 0 AND max_estimated_tokens
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    )
) STRICT;

CREATE INDEX context_manifests_by_repository_revision
    ON context_manifests(
        repository_id,
        repository_revision,
        map_revision,
        created_at_unix_micros DESC
    );

CREATE TABLE context_manifest_items (
    manifest_id TEXT NOT NULL REFERENCES context_manifests(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    repository_relative_path TEXT NOT NULL CHECK (
        length(repository_relative_path) BETWEEN 1 AND 4096
    ),
    kind TEXT NOT NULL CHECK (
        kind IN (
            'source', 'test', 'module', 'instruction', 'configuration',
            'history'
        )
    ),
    start_line INTEGER NOT NULL CHECK (start_line >= 0),
    end_line INTEGER NOT NULL CHECK (end_line >= start_line),
    content_redacted TEXT NOT NULL,
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    reasons_json TEXT NOT NULL CHECK (
        json_valid(reasons_json)
        AND json_type(reasons_json) = 'array'
    ),
    trust TEXT NOT NULL CHECK (trust = 'untrusted-repository-data'),
    generated INTEGER NOT NULL CHECK (generated IN (0, 1)),
    binary INTEGER NOT NULL CHECK (binary IN (0, 1)),
    minified INTEGER NOT NULL CHECK (minified IN (0, 1)),
    vendor INTEGER NOT NULL CHECK (vendor IN (0, 1)),
    dependency INTEGER NOT NULL CHECK (dependency IN (0, 1)),
    estimated_tokens INTEGER NOT NULL CHECK (estimated_tokens >= 0),
    PRIMARY KEY (manifest_id, ordinal)
) STRICT, WITHOUT ROWID;

CREATE TABLE context_manifest_exclusions (
    manifest_id TEXT NOT NULL REFERENCES context_manifests(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    repository_relative_path TEXT NOT NULL CHECK (
        length(repository_relative_path) BETWEEN 1 AND 4096
    ),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 512),
    PRIMARY KEY (manifest_id, ordinal)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER context_manifests_immutable_update
BEFORE UPDATE ON context_manifests
BEGIN
    SELECT RAISE(ABORT, 'context manifests are immutable');
END;

CREATE TRIGGER context_manifest_items_immutable_update
BEFORE UPDATE ON context_manifest_items
BEGIN
    SELECT RAISE(ABORT, 'context manifest items are immutable');
END;

CREATE TRIGGER context_manifest_exclusions_immutable_update
BEFORE UPDATE ON context_manifest_exclusions
BEGIN
    SELECT RAISE(ABORT, 'context manifest exclusions are immutable');
END;

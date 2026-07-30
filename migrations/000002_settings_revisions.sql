-- Versioned non-secret settings and task/run bindings. Secret-shaped fields are
-- additionally rejected at the storage boundary before these rows are written.

CREATE TABLE settings_revisions (
    revision INTEGER PRIMARY KEY CHECK (revision >= 0),
    scope TEXT NOT NULL CHECK (scope IN ('default', 'user', 'repository')),
    repository_id TEXT REFERENCES repositories(id),
    configuration_json TEXT NOT NULL CHECK (
        json_valid(configuration_json) AND length(configuration_json) <= 65536
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    approved INTEGER NOT NULL CHECK (approved IN (0, 1)),
    approval_reference TEXT,
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    CHECK (
        (scope = 'default' AND repository_id IS NULL AND approved = 1
            AND approval_reference IS NULL)
        OR
        (scope = 'user' AND repository_id IS NULL AND approved = 1
            AND approval_reference IS NULL)
        OR
        (scope = 'repository' AND repository_id IS NOT NULL AND approved = 1
            AND length(approval_reference) BETWEEN 1 AND 255)
    )
) STRICT;

INSERT INTO settings_revisions (
    revision, scope, configuration_json, content_sha256, approved,
    idempotency_key, created_at_unix_micros
) VALUES (
    0, 'default', '{}',
    '0000000000000000000000000000000000000000000000000000000000000000',
    1, 'bootstrap-default', 0
);

CREATE UNIQUE INDEX settings_revisions_idempotency
    ON settings_revisions(
        scope,
        ifnull(repository_id, ''),
        idempotency_key
    );

CREATE TABLE task_settings_bindings (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    settings_revision INTEGER NOT NULL REFERENCES settings_revisions(revision),
    bound_at_unix_micros INTEGER NOT NULL CHECK (bound_at_unix_micros >= 0)
) STRICT;

INSERT INTO task_settings_bindings (
    task_id, settings_revision, bound_at_unix_micros
)
SELECT id, 0, created_at_unix_micros
FROM tasks;

CREATE TABLE run_configurations (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    settings_revision INTEGER NOT NULL REFERENCES settings_revisions(revision),
    effective_configuration_json TEXT NOT NULL CHECK (
        json_valid(effective_configuration_json)
        AND length(effective_configuration_json) <= 65536
    ),
    sources_json TEXT NOT NULL CHECK (
        json_valid(sources_json) AND length(sources_json) <= 8192
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    )
) STRICT;

CREATE TRIGGER run_configurations_require_task_binding
BEFORE INSERT ON run_configurations
WHEN NOT EXISTS (
    SELECT 1
    FROM runs
    JOIN task_settings_bindings
      ON task_settings_bindings.task_id = runs.task_id
    WHERE runs.id = NEW.run_id
      AND task_settings_bindings.settings_revision = NEW.settings_revision
)
BEGIN
    SELECT RAISE(ABORT, 'run configuration must match task settings revision');
END;

CREATE INDEX settings_revisions_repository_history
    ON settings_revisions(repository_id, revision DESC);

CREATE INDEX run_configurations_by_revision
    ON run_configurations(settings_revision, run_id);

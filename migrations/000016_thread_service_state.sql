-- Durable state required by the registered ThreadService. Existing threads
-- remain readable through legacy repository operations but are not exposed
-- through a workspace-scoped service until they have a workspace binding.

ALTER TABLE threads ADD COLUMN workspace_id TEXT REFERENCES workspaces(id);
ALTER TABLE threads ADD COLUMN archived_at_unix_micros INTEGER
    CHECK (archived_at_unix_micros IS NULL OR archived_at_unix_micros >= created_at_unix_micros);
ALTER TABLE threads ADD COLUMN create_idempotency_key TEXT
    CHECK (create_idempotency_key IS NULL OR length(create_idempotency_key) BETWEEN 1 AND 255);

UPDATE threads
SET workspace_id = (
    SELECT workspaces.id
    FROM workspaces
    WHERE workspaces.repository_id = threads.repository_id
    ORDER BY workspaces.created_at_unix_micros, workspaces.id
    LIMIT 1
)
WHERE workspace_id IS NULL
  AND EXISTS (
    SELECT 1 FROM workspaces WHERE workspaces.repository_id = threads.repository_id
  );

CREATE UNIQUE INDEX threads_create_idempotency
    ON threads(workspace_id, create_idempotency_key)
    WHERE workspace_id IS NOT NULL AND create_idempotency_key IS NOT NULL;

CREATE INDEX threads_by_workspace
    ON threads(workspace_id, archived_at_unix_micros, updated_at_unix_micros DESC, id DESC)
    WHERE deleted_at_unix_micros IS NULL;

CREATE TABLE thread_mutations (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    thread_id TEXT NOT NULL REFERENCES threads(id),
    operation TEXT NOT NULL CHECK (operation IN ('rename', 'archive')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    request_fingerprint TEXT NOT NULL CHECK (length(request_fingerprint) = 64),
    result_title TEXT NOT NULL CHECK (length(result_title) BETWEEN 1 AND 512),
    result_archived INTEGER NOT NULL CHECK (result_archived IN (0, 1)),
    result_revision INTEGER NOT NULL CHECK (result_revision >= 1),
    result_updated_at_unix_micros INTEGER NOT NULL CHECK (result_updated_at_unix_micros >= 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    PRIMARY KEY (thread_id, operation, idempotency_key)
) STRICT;

CREATE TABLE message_attachments (
    message_id TEXT NOT NULL REFERENCES messages(id),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    artifact_id TEXT NOT NULL REFERENCES artifacts(id),
    PRIMARY KEY (message_id, ordinal),
    UNIQUE (message_id, artifact_id)
) STRICT;

CREATE INDEX message_attachments_by_artifact
    ON message_attachments(artifact_id, message_id);

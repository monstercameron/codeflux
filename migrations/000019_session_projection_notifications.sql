CREATE TABLE session_projection_notifications (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    session_id TEXT NOT NULL REFERENCES sessions(id),
    session_sequence INTEGER NOT NULL CHECK (session_sequence >= 1),
    entity TEXT NOT NULL CHECK (length(entity) BETWEEN 1 AND 64),
    entity_revision INTEGER NOT NULL CHECK (entity_revision >= 0),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    PRIMARY KEY (task_id, idempotency_key),
    UNIQUE (task_id, entity, entity_revision),
    UNIQUE (session_id, session_sequence),
    FOREIGN KEY (session_id, session_sequence)
        REFERENCES session_events(session_id, sequence)
) STRICT;

CREATE INDEX session_projection_notifications_by_entity
    ON session_projection_notifications(task_id, entity, entity_revision);

CREATE TRIGGER session_projection_notifications_immutable_update
BEFORE UPDATE ON session_projection_notifications
BEGIN
    SELECT RAISE(ABORT, 'session projection notifications are immutable');
END;

CREATE TRIGGER session_projection_notifications_immutable_delete
BEFORE DELETE ON session_projection_notifications
BEGIN
    SELECT RAISE(ABORT, 'session projection notifications are immutable');
END;

-- Durable ordered session streams. Sequence allocation advances the session
-- row in the same immediate transaction that inserts the immutable event.

CREATE TABLE sessions (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 40 AND 64),
    thread_id TEXT NOT NULL UNIQUE REFERENCES threads(id) ON DELETE CASCADE,
    current_sequence INTEGER NOT NULL DEFAULT 0 CHECK (current_sequence >= 0),
    compacted_through_sequence INTEGER NOT NULL DEFAULT 0 CHECK (
        compacted_through_sequence >= 0
        AND compacted_through_sequence <= current_sequence
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    updated_at_unix_micros INTEGER NOT NULL CHECK (
        updated_at_unix_micros >= created_at_unix_micros
    )
) STRICT;

CREATE TABLE session_events (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    timestamp_unix_micros INTEGER NOT NULL CHECK (
        timestamp_unix_micros >= 0
    ),
    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
    entity_revision INTEGER NOT NULL CHECK (entity_revision >= 0),
    causation_id TEXT,
    correlation_id TEXT,
    payload_version INTEGER NOT NULL CHECK (payload_version > 0),
    payload_json TEXT NOT NULL CHECK (
        json_valid(payload_json)
        AND json_type(payload_json) = 'object'
    ),
    delivery_class TEXT NOT NULL CHECK (
        delivery_class IN ('ephemeral-coalescible', 'material')
    ),
    correctness_bearing INTEGER NOT NULL CHECK (
        correctness_bearing IN (0, 1)
    ),
    PRIMARY KEY (session_id, sequence)
) STRICT, WITHOUT ROWID;

CREATE INDEX session_events_by_thread
    ON session_events(thread_id, session_id, sequence);

CREATE INDEX session_events_by_task
    ON session_events(task_id, session_id, sequence)
    WHERE task_id IS NOT NULL;

CREATE TABLE session_snapshots (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    through_sequence INTEGER NOT NULL CHECK (through_sequence >= 0),
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    task_state TEXT,
    task_revision INTEGER NOT NULL CHECK (task_revision >= 0),
    snapshot_version INTEGER NOT NULL CHECK (snapshot_version > 0),
    state_json TEXT NOT NULL CHECK (
        json_valid(state_json)
        AND json_type(state_json) = 'object'
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (session_id, through_sequence),
    CHECK (
        (task_id IS NULL AND task_state IS NULL AND task_revision = 0)
        OR
        (task_id IS NOT NULL AND task_state IS NOT NULL)
    )
) STRICT, WITHOUT ROWID;

CREATE INDEX session_snapshots_latest
    ON session_snapshots(session_id, through_sequence DESC);

CREATE TABLE session_commands (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    request_sha256 TEXT NOT NULL CHECK (
        length(request_sha256) = 64
        AND request_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    result_json TEXT NOT NULL CHECK (
        json_valid(result_json)
        AND json_type(result_json) = 'object'
    ),
    final_sequence INTEGER NOT NULL CHECK (final_sequence >= 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (session_id, idempotency_key)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER session_events_immutable_update
BEFORE UPDATE ON session_events
BEGIN
    SELECT RAISE(ABORT, 'session events are immutable');
END;

CREATE TRIGGER session_events_sequence_guard
BEFORE INSERT ON session_events
WHEN NEW.sequence != (
    SELECT current_sequence FROM sessions WHERE id = NEW.session_id
)
BEGIN
    SELECT RAISE(ABORT, 'session event sequence was not allocated by its session');
END;

CREATE TRIGGER session_events_thread_guard
BEFORE INSERT ON session_events
WHEN NEW.thread_id != (
    SELECT thread_id FROM sessions WHERE id = NEW.session_id
)
BEGIN
    SELECT RAISE(ABORT, 'session event thread differs from its session');
END;

CREATE TRIGGER session_events_task_thread_guard
BEFORE INSERT ON session_events
WHEN NEW.task_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM tasks
    WHERE tasks.id = NEW.task_id
      AND tasks.thread_id = NEW.thread_id
)
BEGIN
    SELECT RAISE(ABORT, 'session event task differs from its thread');
END;

CREATE TRIGGER sessions_sequence_monotonic
BEFORE UPDATE OF current_sequence ON sessions
WHEN NEW.current_sequence != OLD.current_sequence + 1
BEGIN
    SELECT RAISE(ABORT, 'session sequence must advance by exactly one');
END;

CREATE TRIGGER sessions_thread_immutable
BEFORE UPDATE OF thread_id ON sessions
BEGIN
    SELECT RAISE(ABORT, 'session thread is immutable');
END;

CREATE TRIGGER session_snapshots_immutable_update
BEFORE UPDATE ON session_snapshots
BEGIN
    SELECT RAISE(ABORT, 'session snapshots are immutable');
END;

CREATE TRIGGER session_snapshots_sequence_guard
BEFORE INSERT ON session_snapshots
WHEN NEW.through_sequence > (
    SELECT current_sequence FROM sessions WHERE id = NEW.session_id
)
BEGIN
    SELECT RAISE(ABORT, 'session snapshot exceeds committed history');
END;

CREATE TRIGGER session_snapshots_thread_guard
BEFORE INSERT ON session_snapshots
WHEN NEW.thread_id != (
    SELECT thread_id FROM sessions WHERE id = NEW.session_id
)
BEGIN
    SELECT RAISE(ABORT, 'session snapshot thread differs from its session');
END;

CREATE TRIGGER session_snapshots_task_thread_guard
BEFORE INSERT ON session_snapshots
WHEN NEW.task_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM tasks
    WHERE tasks.id = NEW.task_id
      AND tasks.thread_id = NEW.thread_id
)
BEGIN
    SELECT RAISE(ABORT, 'session snapshot task differs from its thread');
END;

CREATE TRIGGER session_commands_sequence_guard
BEFORE INSERT ON session_commands
WHEN NEW.final_sequence > (
    SELECT current_sequence FROM sessions WHERE id = NEW.session_id
)
BEGIN
    SELECT RAISE(ABORT, 'session command result exceeds committed history');
END;

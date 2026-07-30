-- Durable coordinator-owned worker leases and visible task scheduling.

CREATE TABLE worker_leases (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    state TEXT NOT NULL CHECK (state IN (
        'starting', 'running', 'paused', 'stopping', 'exited', 'crashed',
        'expired'
    )),
    process_id INTEGER CHECK (process_id IS NULL OR process_id > 0),
    protocol_version INTEGER NOT NULL CHECK (protocol_version > 0),
    tool_schema_version INTEGER NOT NULL CHECK (tool_schema_version > 0),
    policy_revision INTEGER NOT NULL CHECK (policy_revision >= 0),
    worktree_path TEXT NOT NULL CHECK (length(worktree_path) BETWEEN 1 AND 4096),
    endpoint TEXT NOT NULL CHECK (length(endpoint) BETWEEN 1 AND 2048),
    session_token_sha256 TEXT NOT NULL CHECK (
        length(session_token_sha256) = 64
        AND session_token_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    last_sequence INTEGER NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    last_heartbeat_at_unix_micros INTEGER CHECK (
        last_heartbeat_at_unix_micros IS NULL
        OR last_heartbeat_at_unix_micros >= started_at_unix_micros
    ),
    last_checkpoint_id TEXT CHECK (
        last_checkpoint_id IS NULL OR length(last_checkpoint_id) BETWEEN 1 AND 255
    ),
    exit_code INTEGER,
    started_at_unix_micros INTEGER NOT NULL CHECK (
        started_at_unix_micros >= 0
    ),
    ended_at_unix_micros INTEGER CHECK (
        ended_at_unix_micros IS NULL
        OR ended_at_unix_micros >= started_at_unix_micros
    ),
    updated_at_unix_micros INTEGER NOT NULL CHECK (
        updated_at_unix_micros >= started_at_unix_micros
    ),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
) STRICT;

CREATE UNIQUE INDEX worker_leases_one_active_run
    ON worker_leases(run_id)
    WHERE state IN ('starting', 'running', 'paused', 'stopping');

CREATE INDEX worker_leases_heartbeat
    ON worker_leases(state, last_heartbeat_at_unix_micros);

CREATE TABLE worker_reports (
    lease_id TEXT NOT NULL REFERENCES worker_leases(id),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    kind TEXT NOT NULL CHECK (kind IN ('status', 'tool-event')),
    payload_json TEXT NOT NULL CHECK (
        length(payload_json) BETWEEN 2 AND 65536
        AND json_valid(payload_json)
    ),
    occurred_at_unix_micros INTEGER NOT NULL CHECK (
        occurred_at_unix_micros >= 0
    ),
    PRIMARY KEY (lease_id, sequence)
) STRICT;

CREATE INDEX worker_reports_by_run
    ON worker_reports(run_id, sequence);

CREATE TABLE task_queue_entries (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    provider_key TEXT NOT NULL CHECK (length(provider_key) BETWEEN 1 AND 255),
    state TEXT NOT NULL CHECK (state IN ('queued', 'dispatched', 'cancelled')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    priority INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 1000),
    resuming INTEGER NOT NULL CHECK (resuming IN (0, 1)),
    enqueue_sequence INTEGER NOT NULL CHECK (enqueue_sequence >= 1),
    enqueued_at_unix_micros INTEGER NOT NULL CHECK (
        enqueued_at_unix_micros >= 0
    ),
    dispatched_at_unix_micros INTEGER CHECK (
        dispatched_at_unix_micros IS NULL
        OR dispatched_at_unix_micros >= enqueued_at_unix_micros
    ),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
) STRICT;

CREATE UNIQUE INDEX task_queue_one_live_entry
    ON task_queue_entries(task_id)
    WHERE state = 'queued';

CREATE INDEX task_queue_dispatch_order
    ON task_queue_entries(state, resuming DESC, priority DESC, enqueue_sequence);

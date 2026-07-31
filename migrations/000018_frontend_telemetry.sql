-- Content-free, bounded local UX telemetry. Free-form payloads are excluded by
-- schema so prompts, source, tool output, hidden content, and keystrokes cannot
-- be stored through this table.
CREATE TABLE frontend_telemetry_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL CHECK (kind IN (
        'first-run-step', 'time-to-thread', 'time-to-message', 'time-to-plan',
        'time-to-diff', 'plan-decision', 'approval-decision', 'task-control',
        'review-decision', 'graph-interaction', 'memory-interaction', 'reconnect',
        'recovery-action', 'frontend-error', 'long-task', 'slow-render'
    )),
    occurred_at_unix_micros INTEGER NOT NULL CHECK (occurred_at_unix_micros >= 0),
    duration_micros INTEGER NOT NULL CHECK (duration_micros >= 0),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'succeeded', 'failed', 'cancelled', 'approved', 'denied', 'expired',
        'revision-requested', 'paused', 'stopped', 'opened', 'navigated',
        'inspected', 'corrected', 'accepted', 'repair-requested', 'rejected',
        'rolled-back', 'reconnected', 'safe-resumed', 'reconciled',
        'patch-preserved', 'abandoned'
    )),
    component TEXT NOT NULL CHECK (component IN (
        'first-run', 'thread', 'composer', 'plan', 'diff', 'approval', 'top-bar',
        'review', 'graph', 'memory', 'session', 'recovery', 'timeline'
    )),
    graph_mode TEXT CHECK (graph_mode IS NULL OR graph_mode IN ('program', 'execution', 'evidence')),
    failure_class TEXT NOT NULL CHECK (failure_class IN (
        'none', 'input', 'configuration', 'database', 'network', 'authorization',
        'incompatible', 'projection', 'render', 'timeout', 'unknown'
    )),
    task_id TEXT CHECK (task_id IS NULL OR (length(task_id) = 40 AND task_id GLOB 'tsk_*')),
    thread_id TEXT CHECK (thread_id IS NULL OR (length(thread_id) = 40 AND thread_id GLOB 'thr_*')),
    session_id TEXT CHECK (session_id IS NULL OR (length(session_id) = 40 AND session_id GLOB 'ses_*')),
    event_sequence INTEGER NOT NULL CHECK (event_sequence >= 0),
    entity_revision INTEGER NOT NULL CHECK (entity_revision >= 0)
) STRICT;

CREATE INDEX frontend_telemetry_events_by_time
    ON frontend_telemetry_events(occurred_at_unix_micros DESC, id DESC);

CREATE INDEX frontend_telemetry_events_by_kind
    ON frontend_telemetry_events(kind, id DESC);

CREATE TRIGGER frontend_telemetry_events_immutable
BEFORE UPDATE ON frontend_telemetry_events
BEGIN
    SELECT RAISE(ABORT, 'frontend telemetry is immutable');
END;

CREATE TABLE validation_run_intents (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    profile_name TEXT NOT NULL CHECK (length(profile_name) BETWEEN 1 AND 255),
    profile_version TEXT NOT NULL CHECK (length(profile_version) BETWEEN 1 AND 255),
    profile_digest TEXT NOT NULL CHECK (
        length(profile_digest) = 64
        AND profile_digest NOT GLOB '*[^0-9a-f]*'
    ),
    check_id TEXT NOT NULL CHECK (length(check_id) BETWEEN 1 AND 255),
    check_class TEXT NOT NULL CHECK (check_class IN (
        'formatter', 'targeted-test', 'broad-test', 'build', 'static-analysis'
    )),
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    worktree_revision TEXT NOT NULL CHECK (length(worktree_revision) BETWEEN 40 AND 255),
    diff_identity TEXT NOT NULL CHECK (
        length(diff_identity) = 64
        AND diff_identity NOT GLOB '*[^0-9a-f]*'
    ),
    command_definition_json TEXT NOT NULL CHECK (
        json_valid(command_definition_json)
        AND json_type(command_definition_json) = 'object'
        AND length(command_definition_json) BETWEEN 2 AND 16384
    ),
    command_fingerprint TEXT NOT NULL CHECK (
        length(command_fingerprint) = 64
        AND command_fingerprint NOT GLOB '*[^0-9a-f]*'
    ),
    executable_path TEXT NOT NULL CHECK (length(executable_path) BETWEEN 1 AND 4096),
    executable_sha256 TEXT NOT NULL CHECK (
        length(executable_sha256) = 64
        AND executable_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    timeout_millis INTEGER NOT NULL CHECK (timeout_millis BETWEEN 1000 AND 1800000),
    intent_digest TEXT NOT NULL UNIQUE CHECK (
        length(intent_digest) = 64
        AND intent_digest NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (run_id, idempotency_key)
) STRICT;

CREATE TRIGGER validation_run_intents_task_consistency
BEFORE INSERT ON validation_run_intents
WHEN NOT EXISTS (
    SELECT 1 FROM runs
    WHERE runs.id = NEW.run_id
      AND runs.task_id = NEW.task_id
)
BEGIN
    SELECT RAISE(ABORT, 'validation run intent task attribution is inconsistent');
END;

CREATE TRIGGER validation_run_intents_immutable_update
BEFORE UPDATE ON validation_run_intents
BEGIN
    SELECT RAISE(ABORT, 'validation run intents are immutable');
END;

CREATE TRIGGER validation_run_intents_immutable_delete
BEFORE DELETE ON validation_run_intents
BEGIN
    SELECT RAISE(ABORT, 'validation run intents are immutable');
END;

CREATE TABLE validation_run_results (
    validation_run_id TEXT PRIMARY KEY REFERENCES validation_run_intents(id),
    state TEXT NOT NULL CHECK (state IN ('passed', 'failed', 'cancelled')),
    exit_code INTEGER NOT NULL,
    duration_millis INTEGER NOT NULL CHECK (duration_millis >= 0),
    timed_out INTEGER NOT NULL CHECK (timed_out IN (0, 1)),
    cancelled INTEGER NOT NULL CHECK (cancelled IN (0, 1)),
    stdout_redacted TEXT NOT NULL CHECK (length(stdout_redacted) <= 65536),
    stderr_redacted TEXT NOT NULL CHECK (length(stderr_redacted) <= 65536),
    stdout_truncated INTEGER NOT NULL CHECK (stdout_truncated IN (0, 1)),
    stderr_truncated INTEGER NOT NULL CHECK (stderr_truncated IN (0, 1)),
    parser_name TEXT NOT NULL CHECK (length(parser_name) BETWEEN 1 AND 64),
    parse_succeeded INTEGER NOT NULL CHECK (parse_succeeded IN (0, 1)),
    parsed_result_json TEXT NOT NULL CHECK (
        json_valid(parsed_result_json)
        AND json_type(parsed_result_json) = 'object'
        AND length(parsed_result_json) BETWEEN 2 AND 65536
    ),
    raw_redacted_summary TEXT NOT NULL CHECK (length(raw_redacted_summary) BETWEEN 1 AND 65536),
    observed_diff_identity TEXT NOT NULL CHECK (
        length(observed_diff_identity) = 64
        AND observed_diff_identity NOT GLOB '*[^0-9a-f]*'
    ),
    result_digest TEXT NOT NULL UNIQUE CHECK (
        length(result_digest) = 64
        AND result_digest NOT GLOB '*[^0-9a-f]*'
    ),
    completed_at_unix_micros INTEGER NOT NULL CHECK (completed_at_unix_micros >= 0),
    CHECK ((state = 'passed' AND exit_code = 0 AND timed_out = 0 AND cancelled = 0)
        OR state != 'passed')
) STRICT;

CREATE TRIGGER validation_run_results_immutable_update
BEFORE UPDATE ON validation_run_results
BEGIN
    SELECT RAISE(ABORT, 'validation run results are immutable');
END;

CREATE TRIGGER validation_run_results_immutable_delete
BEFORE DELETE ON validation_run_results
BEGIN
    SELECT RAISE(ABORT, 'validation run results are immutable');
END;

CREATE TABLE validation_run_invalidations (
    id INTEGER PRIMARY KEY,
    validation_run_id TEXT NOT NULL REFERENCES validation_run_intents(id),
    previous_diff_identity TEXT NOT NULL CHECK (
        length(previous_diff_identity) = 64
        AND previous_diff_identity NOT GLOB '*[^0-9a-f]*'
    ),
    current_diff_identity TEXT NOT NULL CHECK (
        length(current_diff_identity) = 64
        AND current_diff_identity NOT GLOB '*[^0-9a-f]*'
        AND current_diff_identity <> previous_diff_identity
    ),
    reason TEXT NOT NULL CHECK (reason = 'underlying-diff-changed'),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (validation_run_id, current_diff_identity)
) STRICT;

CREATE TRIGGER validation_run_invalidations_consistency
BEFORE INSERT ON validation_run_invalidations
WHEN NOT EXISTS (
    SELECT 1 FROM validation_run_intents AS intent
    JOIN validation_run_results AS result
      ON result.validation_run_id = intent.id
    WHERE intent.id = NEW.validation_run_id
      AND intent.diff_identity = NEW.previous_diff_identity
)
BEGIN
    SELECT RAISE(ABORT, 'validation invalidation does not match a completed run');
END;

CREATE TRIGGER validation_run_invalidations_immutable_update
BEFORE UPDATE ON validation_run_invalidations
BEGIN
    SELECT RAISE(ABORT, 'validation run invalidations are immutable');
END;

CREATE TRIGGER validation_run_invalidations_immutable_delete
BEFORE DELETE ON validation_run_invalidations
BEGIN
    SELECT RAISE(ABORT, 'validation run invalidations are immutable');
END;

CREATE INDEX validation_run_intents_by_run_check_diff
    ON validation_run_intents(run_id, check_id, diff_identity, created_at_unix_micros);

CREATE INDEX validation_run_invalidations_by_run
    ON validation_run_invalidations(validation_run_id, created_at_unix_micros);

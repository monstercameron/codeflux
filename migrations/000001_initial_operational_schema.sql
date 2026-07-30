-- Codeflux Phase 1 operational schema.
-- All timestamps are UTC Unix microseconds and all monetary amounts are exact
-- integer minor units. Provider credentials are intentionally absent.

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    deleted_at_unix_micros INTEGER CHECK (deleted_at_unix_micros >= created_at_unix_micros)
) STRICT;

CREATE TABLE repositories (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    canonical_path TEXT NOT NULL CHECK (length(canonical_path) BETWEEN 1 AND 4096),
    git_identity TEXT NOT NULL CHECK (length(git_identity) BETWEEN 1 AND 512),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    deleted_at_unix_micros INTEGER CHECK (deleted_at_unix_micros >= created_at_unix_micros),
    UNIQUE (project_id, canonical_path)
) STRICT;

CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    canonical_path TEXT NOT NULL CHECK (length(canonical_path) BETWEEN 1 AND 4096),
    state TEXT NOT NULL CHECK (state IN ('active', 'closed', 'recovery-required')),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (repository_id, canonical_path)
) STRICT;

CREATE TABLE worktree_bindings (
    workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    base_revision TEXT NOT NULL CHECK (length(base_revision) BETWEEN 1 AND 255),
    branch_name TEXT NOT NULL CHECK (length(branch_name) BETWEEN 1 AND 255),
    worktree_path TEXT NOT NULL CHECK (length(worktree_path) BETWEEN 1 AND 4096),
    state TEXT NOT NULL CHECK (state IN ('active', 'released', 'recovery-required')),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (repository_id, worktree_path)
) STRICT;

CREATE TABLE threads (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 512),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    deleted_at_unix_micros INTEGER CHECK (deleted_at_unix_micros >= created_at_unix_micros)
) STRICT;

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL REFERENCES threads(id),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
    body_redacted TEXT NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (thread_id, sequence),
    UNIQUE (thread_id, idempotency_key)
) STRICT;

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL REFERENCES threads(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    request_message_id TEXT REFERENCES messages(id),
    state TEXT NOT NULL CHECK (state IN (
        'draft', 'forecasting', 'awaiting-plan-approval', 'ready', 'running',
        'paused', 'awaiting-authority', 'validating', 'awaiting-review',
        'completed', 'failed', 'cancelled', 'rolled-back', 'recovery-required'
    )),
    policy_preset TEXT NOT NULL CHECK (policy_preset IN ('correctness', 'balanced', 'fast', 'economical')),
    reasoning_effort TEXT NOT NULL CHECK (reasoning_effort IN ('minimal', 'standard', 'extended', 'maximum')),
    risk_level TEXT NOT NULL CHECK (risk_level IN ('routine', 'elevated', 'protected')),
    required_assurance TEXT NOT NULL CHECK (required_assurance IN (
        'fully-evaluated', 'model-verified', 'contract-checked', 'runtime-only'
    )),
    pause_reason TEXT,
    failure_reason TEXT,
    cancellation_reason TEXT,
    rollback_reason TEXT,
    invalidation_reason TEXT,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (thread_id, idempotency_key)
) STRICT;

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    state TEXT NOT NULL CHECK (state IN (
        'pending', 'starting', 'running', 'pausing', 'paused', 'validating',
        'completed', 'failed', 'cancelled', 'recovery-required'
    )),
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    task_revision INTEGER NOT NULL CHECK (task_revision >= 0),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (task_id, attempt),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TABLE task_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (length(event_type) BETWEEN 1 AND 255),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (task_id, sequence),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TABLE approvals (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    state TEXT NOT NULL CHECK (state IN ('pending', 'granted', 'denied', 'expired', 'cancelled')),
    scope TEXT NOT NULL CHECK (length(scope) BETWEEN 1 AND 1024),
    request_reason TEXT NOT NULL CHECK (length(request_reason) BETWEEN 1 AND 2048),
    resolution_reason TEXT,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    requested_at_unix_micros INTEGER NOT NULL CHECK (requested_at_unix_micros >= 0),
    decided_at_unix_micros INTEGER CHECK (decided_at_unix_micros >= requested_at_unix_micros),
    expires_at_unix_micros INTEGER CHECK (expires_at_unix_micros >= requested_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TABLE permission_decisions (
    id TEXT PRIMARY KEY,
    approval_id TEXT REFERENCES approvals(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    decision TEXT NOT NULL CHECK (decision IN ('granted', 'denied')),
    capability TEXT NOT NULL CHECK (length(capability) BETWEEN 1 AND 255),
    resource_scope TEXT NOT NULL CHECK (length(resource_scope) BETWEEN 1 AND 2048),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0)
) STRICT;

CREATE TABLE providers (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 255),
    provider_type TEXT NOT NULL CHECK (length(provider_type) BETWEEN 1 AND 255),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (provider_type, display_name)
) STRICT;

CREATE TABLE model_catalog (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES providers(id),
    model_name TEXT NOT NULL CHECK (length(model_name) BETWEEN 1 AND 255),
    model_revision TEXT NOT NULL CHECK (length(model_revision) BETWEEN 1 AND 255),
    context_tokens INTEGER NOT NULL CHECK (context_tokens > 0),
    maximum_output_tokens INTEGER NOT NULL CHECK (
        maximum_output_tokens > 0 AND maximum_output_tokens <= context_tokens
    ),
    tool_calling INTEGER NOT NULL CHECK (tool_calling IN (0, 1)),
    structured_output INTEGER NOT NULL CHECK (structured_output IN (0, 1)),
    streaming INTEGER NOT NULL CHECK (streaming IN (0, 1)),
    image_input INTEGER NOT NULL CHECK (image_input IN (0, 1)),
    prompt_caching INTEGER NOT NULL CHECK (prompt_caching IN (0, 1)),
    reasoning INTEGER NOT NULL CHECK (reasoning IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (provider_id, model_name, model_revision)
) STRICT;

CREATE TABLE pricing_snapshots (
    id TEXT PRIMARY KEY,
    model_id TEXT NOT NULL REFERENCES model_catalog(id),
    currency TEXT NOT NULL CHECK (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    ),
    input_per_million_minor INTEGER CHECK (input_per_million_minor >= 0),
    cached_input_per_million_minor INTEGER CHECK (cached_input_per_million_minor >= 0),
    cache_write_per_million_minor INTEGER CHECK (cache_write_per_million_minor >= 0),
    output_per_million_minor INTEGER CHECK (output_per_million_minor >= 0),
    reasoning_per_million_minor INTEGER CHECK (reasoning_per_million_minor >= 0),
    effective_at_unix_micros INTEGER NOT NULL CHECK (effective_at_unix_micros >= 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (model_id, currency, effective_at_unix_micros)
) STRICT;

CREATE TABLE model_requests (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    model_id TEXT NOT NULL REFERENCES model_catalog(id),
    pricing_snapshot_id TEXT REFERENCES pricing_snapshots(id),
    state TEXT NOT NULL CHECK (state IN (
        'planned', 'started', 'succeeded', 'failed', 'cancelled', 'outcome-unknown'
    )),
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    cost_known INTEGER NOT NULL CHECK (cost_known IN (0, 1)),
    actual_cost_minor INTEGER CHECK (actual_cost_minor >= 0),
    currency TEXT CHECK (currency IS NULL OR (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    )),
    started_at_unix_micros INTEGER CHECK (started_at_unix_micros >= 0),
    completed_at_unix_micros INTEGER CHECK (
        completed_at_unix_micros IS NULL OR completed_at_unix_micros >= started_at_unix_micros
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    CHECK (
        (cost_known = 0 AND actual_cost_minor IS NULL AND currency IS NULL)
        OR
        (cost_known = 1 AND actual_cost_minor IS NOT NULL AND currency IS NOT NULL)
    ),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TABLE forecasts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    model_id TEXT REFERENCES model_catalog(id),
    latency_known INTEGER NOT NULL CHECK (latency_known IN (0, 1)),
    latency_p50_millis INTEGER,
    latency_p90_millis INTEGER,
    tokens_known INTEGER NOT NULL CHECK (tokens_known IN (0, 1)),
    tokens_p50 INTEGER,
    tokens_p90 INTEGER,
    cost_known INTEGER NOT NULL CHECK (cost_known IN (0, 1)),
    cost_p50_minor INTEGER,
    cost_p90_minor INTEGER,
    currency TEXT CHECK (currency IS NULL OR (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    )),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    immutable_revision INTEGER NOT NULL CHECK (immutable_revision >= 1),
    CHECK (
        (latency_known = 0 AND latency_p50_millis IS NULL AND latency_p90_millis IS NULL)
        OR
        (latency_known = 1 AND latency_p50_millis >= 0 AND latency_p90_millis >= latency_p50_millis)
    ),
    CHECK (
        (tokens_known = 0 AND tokens_p50 IS NULL AND tokens_p90 IS NULL)
        OR
        (tokens_known = 1 AND tokens_p50 >= 0 AND tokens_p90 >= tokens_p50)
    ),
    CHECK (
        (cost_known = 0 AND cost_p50_minor IS NULL AND cost_p90_minor IS NULL AND currency IS NULL)
        OR
        (cost_known = 1 AND cost_p50_minor >= 0 AND cost_p90_minor >= cost_p50_minor AND currency IS NOT NULL)
    ),
    UNIQUE (task_id, immutable_revision)
) STRICT;

CREATE TABLE budgets (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
    currency TEXT NOT NULL CHECK (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    ),
    warning_cost_minor INTEGER NOT NULL CHECK (warning_cost_minor >= 0),
    hard_stop_cost_minor INTEGER NOT NULL CHECK (hard_stop_cost_minor >= warning_cost_minor),
    warning_tokens INTEGER NOT NULL CHECK (warning_tokens >= 0),
    hard_stop_tokens INTEGER NOT NULL CHECK (hard_stop_tokens >= warning_tokens),
    warning_wall_clock_millis INTEGER NOT NULL CHECK (warning_wall_clock_millis >= 0),
    hard_stop_wall_clock_millis INTEGER NOT NULL CHECK (
        hard_stop_wall_clock_millis >= warning_wall_clock_millis
    ),
    maximum_provider_calls INTEGER NOT NULL CHECK (maximum_provider_calls >= 0),
    maximum_repair_rounds INTEGER NOT NULL CHECK (maximum_repair_rounds >= 0),
    maximum_tool_executions INTEGER NOT NULL CHECK (maximum_tool_executions >= 0),
    reserved_cost_minor INTEGER NOT NULL DEFAULT 0 CHECK (reserved_cost_minor >= 0),
    actual_cost_minor INTEGER NOT NULL DEFAULT 0 CHECK (actual_cost_minor >= 0),
    actual_tokens INTEGER NOT NULL DEFAULT 0 CHECK (actual_tokens >= 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    CHECK (reserved_cost_minor + actual_cost_minor <= hard_stop_cost_minor)
) STRICT;

CREATE TABLE usage_records (
    id TEXT PRIMARY KEY,
    model_request_id TEXT NOT NULL REFERENCES model_requests(id),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    usage_known INTEGER NOT NULL CHECK (usage_known IN (0, 1)),
    input_tokens INTEGER,
    cached_input_tokens INTEGER,
    cache_write_tokens INTEGER,
    output_tokens INTEGER,
    reasoning_tokens INTEGER,
    provider_specific_json TEXT CHECK (
        provider_specific_json IS NULL OR json_valid(provider_specific_json)
    ),
    cost_known INTEGER NOT NULL CHECK (cost_known IN (0, 1)),
    cost_minor INTEGER,
    currency TEXT CHECK (currency IS NULL OR (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    )),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    CHECK (
        (usage_known = 0 AND input_tokens IS NULL AND cached_input_tokens IS NULL
            AND cache_write_tokens IS NULL AND output_tokens IS NULL
            AND reasoning_tokens IS NULL AND provider_specific_json IS NULL)
        OR
        (usage_known = 1 AND input_tokens >= 0 AND cached_input_tokens >= 0
            AND cache_write_tokens >= 0 AND output_tokens >= 0
            AND reasoning_tokens >= 0)
    ),
    CHECK (
        (cost_known = 0 AND cost_minor IS NULL AND currency IS NULL)
        OR
        (cost_known = 1 AND cost_minor >= 0 AND currency IS NOT NULL)
    ),
    UNIQUE (model_request_id, sequence)
) STRICT;

CREATE TABLE command_executions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    approval_id TEXT REFERENCES approvals(id),
    state TEXT NOT NULL CHECK (state IN (
        'pending', 'awaiting-authority', 'authorized', 'running', 'succeeded',
        'failed', 'cancelled', 'outcome-unknown'
    )),
    command_name TEXT NOT NULL CHECK (length(command_name) BETWEEN 1 AND 255),
    arguments_redacted_json TEXT NOT NULL CHECK (json_valid(arguments_redacted_json)),
    working_directory_scope TEXT NOT NULL CHECK (length(working_directory_scope) BETWEEN 1 AND 4096),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    exit_code INTEGER,
    started_at_unix_micros INTEGER CHECK (started_at_unix_micros >= 0),
    completed_at_unix_micros INTEGER CHECK (
        completed_at_unix_micros IS NULL OR completed_at_unix_micros >= started_at_unix_micros
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (run_id, idempotency_key)
) STRICT;

CREATE TABLE redacted_outputs (
    id TEXT PRIMARY KEY,
    command_execution_id TEXT NOT NULL REFERENCES command_executions(id),
    stream TEXT NOT NULL CHECK (stream IN ('stdout', 'stderr')),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    content_redacted TEXT NOT NULL,
    byte_count INTEGER NOT NULL CHECK (byte_count >= 0),
    truncated INTEGER NOT NULL CHECK (truncated IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (command_execution_id, stream, sequence)
) STRICT;

CREATE TABLE checkpoints (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    state TEXT NOT NULL CHECK (state IN ('creating', 'ready', 'failed', 'invalidated')),
    repository_revision TEXT NOT NULL CHECK (length(repository_revision) BETWEEN 1 AND 255),
    worktree_diff_hash TEXT NOT NULL CHECK (length(worktree_diff_hash) = 64),
    event_sequence INTEGER NOT NULL CHECK (event_sequence >= 0),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TABLE recovery_attempts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    checkpoint_id TEXT REFERENCES checkpoints(id),
    state TEXT NOT NULL CHECK (state IN ('planned', 'running', 'succeeded', 'failed', 'cancelled')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    started_at_unix_micros INTEGER CHECK (started_at_unix_micros >= 0),
    completed_at_unix_micros INTEGER CHECK (
        completed_at_unix_micros IS NULL OR completed_at_unix_micros >= started_at_unix_micros
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
) STRICT;

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    repository_id TEXT REFERENCES repositories(id),
    task_id TEXT REFERENCES tasks(id),
    artifact_type TEXT NOT NULL CHECK (length(artifact_type) BETWEEN 1 AND 255),
    immutable_version INTEGER NOT NULL CHECK (immutable_version >= 1),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    media_type TEXT NOT NULL CHECK (length(media_type) BETWEEN 1 AND 255),
    storage_class TEXT NOT NULL CHECK (storage_class IN (
        'permanent-semantic', 'permanent-evidence', 'bounded-failure',
        'temporary-scratch', 'compacted-history'
    )),
    sanitized_content BLOB,
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    deleted_at_unix_micros INTEGER CHECK (deleted_at_unix_micros >= created_at_unix_micros),
    UNIQUE (project_id, content_hash, immutable_version)
) STRICT;

CREATE TABLE diff_summaries (
    id TEXT PRIMARY KEY,
    artifact_id TEXT NOT NULL REFERENCES artifacts(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    base_revision TEXT NOT NULL CHECK (length(base_revision) BETWEEN 1 AND 255),
    head_revision TEXT NOT NULL CHECK (length(head_revision) BETWEEN 1 AND 255),
    files_changed INTEGER NOT NULL CHECK (files_changed >= 0),
    insertions INTEGER NOT NULL CHECK (insertions >= 0),
    deletions INTEGER NOT NULL CHECK (deletions >= 0),
    summary_redacted TEXT NOT NULL,
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0)
) STRICT;

CREATE TABLE validations (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    artifact_id TEXT REFERENCES artifacts(id),
    command_execution_id TEXT REFERENCES command_executions(id),
    state TEXT NOT NULL CHECK (state IN (
        'pending', 'running', 'passed', 'failed', 'waived', 'skipped',
        'cancelled', 'invalidated'
    )),
    severity TEXT NOT NULL CHECK (severity IN ('advisory', 'warning', 'error', 'blocking')),
    profile_name TEXT NOT NULL CHECK (length(profile_name) BETWEEN 1 AND 255),
    summary_redacted TEXT,
    started_at_unix_micros INTEGER CHECK (started_at_unix_micros >= 0),
    completed_at_unix_micros INTEGER CHECK (
        completed_at_unix_micros IS NULL OR completed_at_unix_micros >= started_at_unix_micros
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= created_at_unix_micros),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
) STRICT;

CREATE TABLE evidence (
    id TEXT PRIMARY KEY,
    validation_id TEXT NOT NULL REFERENCES validations(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    artifact_id TEXT REFERENCES artifacts(id),
    assurance_level TEXT NOT NULL CHECK (assurance_level IN (
        'fully-evaluated', 'model-verified', 'contract-checked',
        'runtime-only', 'invalidated'
    )),
    evidence_type TEXT NOT NULL CHECK (length(evidence_type) BETWEEN 1 AND 255),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    summary_redacted TEXT NOT NULL,
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    invalidated_at_unix_micros INTEGER CHECK (invalidated_at_unix_micros >= created_at_unix_micros),
    invalidation_reason TEXT,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
) STRICT;

CREATE INDEX repositories_by_project
    ON repositories(project_id, updated_at_unix_micros DESC, id);
CREATE INDEX worktree_bindings_active
    ON worktree_bindings(repository_id, state, updated_at_unix_micros DESC);
CREATE INDEX threads_for_pagination
    ON threads(repository_id, updated_at_unix_micros DESC, id DESC);
CREATE INDEX messages_for_pagination
    ON messages(thread_id, sequence DESC);
CREATE INDEX tasks_active
    ON tasks(repository_id, state, updated_at_unix_micros DESC)
    WHERE state NOT IN ('completed', 'cancelled', 'rolled-back');
CREATE INDEX runs_by_task
    ON runs(task_id, attempt DESC);
CREATE INDEX task_events_for_replay
    ON task_events(task_id, sequence);
CREATE INDEX approvals_pending
    ON approvals(task_id, state, expires_at_unix_micros);
CREATE INDEX model_requests_by_task
    ON model_requests(task_id, created_at_unix_micros)
    WHERE state IN ('planned', 'started', 'outcome-unknown');
CREATE INDEX model_requests_for_cost
    ON model_requests(task_id, cost_known, actual_cost_minor);
CREATE INDEX usage_by_request
    ON usage_records(model_request_id, sequence);
CREATE INDEX commands_by_run
    ON command_executions(run_id, state, created_at_unix_micros);
CREATE INDEX checkpoints_by_task
    ON checkpoints(task_id, created_at_unix_micros DESC);
CREATE INDEX validations_by_task
    ON validations(task_id, state, severity);
CREATE INDEX evidence_by_task
    ON evidence(task_id, assurance_level, created_at_unix_micros DESC);
CREATE INDEX artifacts_by_project
    ON artifacts(project_id, storage_class, created_at_unix_micros DESC);

-- Append-only correctness-bearing rows reject in-place rewriting. Deletion is
-- governed separately by explicit lifecycle operations so user deletion can
-- remain possible without weakening update immutability.
CREATE TRIGGER messages_reject_update
BEFORE UPDATE ON messages
BEGIN
    SELECT RAISE(ABORT, 'messages are immutable');
END;

CREATE TRIGGER task_events_reject_update
BEFORE UPDATE ON task_events
BEGIN
    SELECT RAISE(ABORT, 'task events are immutable');
END;

CREATE TRIGGER permission_decisions_reject_update
BEFORE UPDATE ON permission_decisions
BEGIN
    SELECT RAISE(ABORT, 'permission decisions are immutable');
END;

CREATE TRIGGER pricing_snapshots_reject_update
BEFORE UPDATE ON pricing_snapshots
BEGIN
    SELECT RAISE(ABORT, 'pricing snapshots are immutable');
END;

CREATE TRIGGER forecasts_reject_update
BEFORE UPDATE ON forecasts
BEGIN
    SELECT RAISE(ABORT, 'forecasts are immutable');
END;

CREATE TRIGGER usage_records_reject_update
BEFORE UPDATE ON usage_records
BEGIN
    SELECT RAISE(ABORT, 'usage records are immutable');
END;

CREATE TRIGGER redacted_outputs_reject_update
BEFORE UPDATE ON redacted_outputs
BEGIN
    SELECT RAISE(ABORT, 'redacted outputs are immutable');
END;

CREATE TRIGGER diff_summaries_reject_update
BEFORE UPDATE ON diff_summaries
BEGIN
    SELECT RAISE(ABORT, 'diff summaries are immutable');
END;

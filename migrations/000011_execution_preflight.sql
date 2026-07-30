-- Immutable fixed-policy, forecast, execution-preflight, and outcome bindings.

CREATE TABLE execution_policy_revisions (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 255),
    selection_source TEXT NOT NULL CHECK (
        selection_source IN ('fixed-baseline', 'manual-override')
    ),
    canonical_json TEXT NOT NULL CHECK (
        length(canonical_json) BETWEEN 2 AND 262144
        AND json_valid(canonical_json)
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (task_id, revision),
    UNIQUE (task_id, content_sha256),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TRIGGER execution_policy_revisions_immutable_update
BEFORE UPDATE ON execution_policy_revisions
BEGIN
    SELECT RAISE(ABORT, 'execution policy revisions are immutable');
END;

CREATE TRIGGER execution_policy_revisions_immutable_delete
BEFORE DELETE ON execution_policy_revisions
BEGIN
    SELECT RAISE(ABORT, 'execution policy revisions are immutable');
END;

CREATE TABLE effort_forecast_revisions (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    policy_revision INTEGER NOT NULL CHECK (policy_revision >= 1),
    algorithm_version TEXT NOT NULL CHECK (
        length(algorithm_version) BETWEEN 1 AND 255
    ),
    canonical_json TEXT NOT NULL CHECK (
        length(canonical_json) BETWEEN 2 AND 524288
        AND json_valid(canonical_json)
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    features_json TEXT NOT NULL CHECK (
        length(features_json) BETWEEN 2 AND 262144
        AND json_valid(features_json)
    ),
    features_sha256 TEXT NOT NULL CHECK (
        length(features_sha256) = 64
        AND features_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    counterfactual_eligible INTEGER NOT NULL CHECK (
        counterfactual_eligible IN (0, 1)
    ),
    eligibility_json TEXT NOT NULL CHECK (
        length(eligibility_json) BETWEEN 2 AND 65536
        AND json_valid(eligibility_json)
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (task_id, revision),
    UNIQUE (task_id, content_sha256),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (task_id, policy_revision)
        REFERENCES execution_policy_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER effort_forecast_revisions_immutable_update
BEFORE UPDATE ON effort_forecast_revisions
BEGIN
    SELECT RAISE(ABORT, 'effort forecast revisions are immutable');
END;

CREATE TRIGGER effort_forecast_revisions_immutable_delete
BEFORE DELETE ON effort_forecast_revisions
BEGIN
    SELECT RAISE(ABORT, 'effort forecast revisions are immutable');
END;

CREATE TABLE task_execution_preflights (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    expected_task_revision INTEGER NOT NULL CHECK (expected_task_revision >= 0),
    policy_revision INTEGER NOT NULL CHECK (policy_revision >= 1),
    forecast_revision INTEGER NOT NULL CHECK (forecast_revision >= 1),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    budget_limit_revision INTEGER NOT NULL CHECK (budget_limit_revision >= 0),
    presentation_json TEXT NOT NULL CHECK (
        length(presentation_json) BETWEEN 2 AND 524288
        AND json_valid(presentation_json)
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (task_id, revision),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (task_id, policy_revision)
        REFERENCES execution_policy_revisions(task_id, revision),
    FOREIGN KEY (task_id, forecast_revision)
        REFERENCES effort_forecast_revisions(task_id, revision),
    FOREIGN KEY (budget_id, budget_limit_revision)
        REFERENCES budget_limit_revisions(budget_id, revision)
) STRICT;

CREATE TRIGGER task_execution_preflights_consistency
BEFORE INSERT ON task_execution_preflights
WHEN NOT EXISTS (
        SELECT 1
        FROM effort_forecast_revisions AS forecast
        JOIN budgets AS budget ON budget.id = NEW.budget_id
        WHERE forecast.task_id = NEW.task_id
          AND forecast.revision = NEW.forecast_revision
          AND forecast.policy_revision = NEW.policy_revision
          AND budget.task_id = NEW.task_id
    )
BEGIN
    SELECT RAISE(ABORT, 'execution preflight bindings are inconsistent');
END;

CREATE TRIGGER task_execution_preflights_immutable_update
BEFORE UPDATE ON task_execution_preflights
BEGIN
    SELECT RAISE(ABORT, 'task execution preflights are immutable');
END;

CREATE TRIGGER task_execution_preflights_immutable_delete
BEFORE DELETE ON task_execution_preflights
BEGIN
    SELECT RAISE(ABORT, 'task execution preflights are immutable');
END;

CREATE TABLE run_execution_bindings (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    preflight_revision INTEGER NOT NULL CHECK (preflight_revision >= 1),
    policy_revision INTEGER NOT NULL CHECK (policy_revision >= 1),
    forecast_revision INTEGER NOT NULL CHECK (forecast_revision >= 1),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    budget_limit_revision INTEGER NOT NULL CHECK (budget_limit_revision >= 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    FOREIGN KEY (task_id, preflight_revision)
        REFERENCES task_execution_preflights(task_id, revision),
    FOREIGN KEY (task_id, policy_revision)
        REFERENCES execution_policy_revisions(task_id, revision),
    FOREIGN KEY (task_id, forecast_revision)
        REFERENCES effort_forecast_revisions(task_id, revision),
    FOREIGN KEY (budget_id, budget_limit_revision)
        REFERENCES budget_limit_revisions(budget_id, revision)
) STRICT;

CREATE UNIQUE INDEX run_execution_bindings_task_preflight
    ON run_execution_bindings(task_id, preflight_revision, run_id);

CREATE TRIGGER run_execution_bindings_consistency
BEFORE INSERT ON run_execution_bindings
WHEN NOT EXISTS (
        SELECT 1
        FROM runs AS run
        JOIN task_execution_preflights AS preflight
          ON preflight.task_id = NEW.task_id
         AND preflight.revision = NEW.preflight_revision
        WHERE run.id = NEW.run_id
          AND run.task_id = NEW.task_id
          AND preflight.policy_revision = NEW.policy_revision
          AND preflight.forecast_revision = NEW.forecast_revision
          AND preflight.budget_id = NEW.budget_id
          AND preflight.budget_limit_revision = NEW.budget_limit_revision
    )
BEGIN
    SELECT RAISE(ABORT, 'run execution bindings are inconsistent');
END;

CREATE TRIGGER run_execution_bindings_immutable_update
BEFORE UPDATE ON run_execution_bindings
BEGIN
    SELECT RAISE(ABORT, 'run execution bindings are immutable');
END;

CREATE TRIGGER run_execution_bindings_immutable_delete
BEFORE DELETE ON run_execution_bindings
BEGIN
    SELECT RAISE(ABORT, 'run execution bindings are immutable');
END;

CREATE TABLE forecast_outcomes (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    forecast_revision INTEGER NOT NULL CHECK (forecast_revision >= 1),
    outcome_json TEXT NOT NULL CHECK (
        length(outcome_json) BETWEEN 2 AND 262144
        AND json_valid(outcome_json)
    ),
    outcome_sha256 TEXT NOT NULL CHECK (
        length(outcome_sha256) = 64
        AND outcome_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    comparison_json TEXT NOT NULL CHECK (
        length(comparison_json) BETWEEN 2 AND 262144
        AND json_valid(comparison_json)
    ),
    comparison_sha256 TEXT NOT NULL CHECK (
        length(comparison_sha256) = 64
        AND comparison_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (task_id, forecast_revision)
        REFERENCES effort_forecast_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER forecast_outcomes_consistency
BEFORE INSERT ON forecast_outcomes
WHEN NOT EXISTS (
        SELECT 1
        FROM run_execution_bindings AS binding
        WHERE binding.run_id = NEW.run_id
          AND binding.task_id = NEW.task_id
          AND binding.forecast_revision = NEW.forecast_revision
    )
BEGIN
    SELECT RAISE(ABORT, 'forecast outcome binding is inconsistent');
END;

CREATE TRIGGER forecast_outcomes_immutable_update
BEFORE UPDATE ON forecast_outcomes
BEGIN
    SELECT RAISE(ABORT, 'forecast outcomes are immutable');
END;

CREATE TRIGGER forecast_outcomes_immutable_delete
BEFORE DELETE ON forecast_outcomes
BEGIN
    SELECT RAISE(ABORT, 'forecast outcomes are immutable');
END;

CREATE TABLE calibration_report_schemas (
    algorithm_version TEXT NOT NULL CHECK (
        length(algorithm_version) BETWEEN 1 AND 255
    ),
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
    schema_json TEXT NOT NULL CHECK (
        length(schema_json) BETWEEN 2 AND 65536
        AND json_valid(schema_json)
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (algorithm_version, schema_version),
    UNIQUE (content_sha256)
) STRICT;

CREATE TRIGGER calibration_report_schemas_immutable_update
BEFORE UPDATE ON calibration_report_schemas
BEGIN
    SELECT RAISE(ABORT, 'calibration report schemas are immutable');
END;

CREATE TRIGGER calibration_report_schemas_immutable_delete
BEFORE DELETE ON calibration_report_schemas
BEGIN
    SELECT RAISE(ABORT, 'calibration report schemas are immutable');
END;

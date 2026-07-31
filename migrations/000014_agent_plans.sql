-- Durable requirement intake, immutable structured plans, and attributable
-- execution, repair, completion, and review records.

CREATE TABLE task_requirement_revisions (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    message_id TEXT NOT NULL REFERENCES messages(id),
    original_body_sha256 TEXT NOT NULL CHECK (
        length(original_body_sha256) = 64
        AND original_body_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    task_type TEXT NOT NULL CHECK (task_type IN (
        'bug-fix', 'feature', 'refactor', 'documentation', 'test',
        'investigation', 'maintenance'
    )),
    risk_level TEXT NOT NULL CHECK (
        risk_level IN ('routine', 'elevated', 'protected')
    ),
    validation_profile TEXT NOT NULL CHECK (
        validation_profile IN ('routine-v1', 'elevated-v1', 'protected-v1')
    ),
    requires_clarification INTEGER NOT NULL CHECK (
        requires_clarification IN (0, 1)
    ),
    clarification_question TEXT CHECK (
        clarification_question IS NULL
        OR length(clarification_question) BETWEEN 1 AND 2048
    ),
    analysis_json TEXT NOT NULL CHECK (
        length(analysis_json) BETWEEN 2 AND 262144
        AND json_valid(analysis_json)
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
    UNIQUE (task_id, message_id),
    UNIQUE (task_id, idempotency_key),
    CHECK (
        (requires_clarification = 1 AND clarification_question IS NOT NULL)
        OR
        (requires_clarification = 0 AND clarification_question IS NULL)
    )
) STRICT;

CREATE TRIGGER task_requirement_revisions_consistency
BEFORE INSERT ON task_requirement_revisions
WHEN NOT EXISTS (
    SELECT 1
    FROM tasks AS task
    JOIN messages AS message
      ON message.id = NEW.message_id
     AND message.thread_id = task.thread_id
     AND message.role = 'user'
    WHERE task.id = NEW.task_id
      AND task.request_message_id = NEW.message_id
      AND task.risk_level = NEW.risk_level
)
BEGIN
    SELECT RAISE(ABORT, 'task requirement must bind the original user message');
END;

CREATE TRIGGER task_requirement_revisions_immutable_update
BEFORE UPDATE ON task_requirement_revisions
BEGIN
    SELECT RAISE(ABORT, 'task requirement revisions are immutable');
END;

CREATE TRIGGER task_requirement_revisions_immutable_delete
BEFORE DELETE ON task_requirement_revisions
BEGIN
    SELECT RAISE(ABORT, 'task requirement revisions are immutable');
END;

CREATE TABLE agent_plan_revisions (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    requirement_revision INTEGER NOT NULL CHECK (requirement_revision >= 1),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    repository_revision TEXT NOT NULL CHECK (
        length(repository_revision) IN (40, 64)
        AND repository_revision NOT GLOB '*[^0-9a-f]*'
    ),
    context_manifest_id TEXT NOT NULL REFERENCES context_manifests(id),
    policy_revision INTEGER NOT NULL CHECK (policy_revision >= 1),
    forecast_revision INTEGER NOT NULL CHECK (forecast_revision >= 1),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    budget_limit_revision INTEGER NOT NULL CHECK (budget_limit_revision >= 0),
    budget_snapshot_revision INTEGER NOT NULL CHECK (
        budget_snapshot_revision >= 0
    ),
    risk_level TEXT NOT NULL CHECK (
        risk_level IN ('routine', 'elevated', 'protected')
    ),
    validation_profile TEXT NOT NULL CHECK (
        validation_profile IN ('routine-v1', 'elevated-v1', 'protected-v1')
    ),
    approval_required INTEGER NOT NULL CHECK (
        approval_required IN (0, 1)
    ),
    supersedes_revision INTEGER CHECK (
        supersedes_revision IS NULL OR supersedes_revision >= 1
    ),
    redirect_message_id TEXT REFERENCES messages(id),
    canonical_json TEXT NOT NULL CHECK (
        length(canonical_json) BETWEEN 2 AND 524288
        AND json_valid(canonical_json)
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    user_summary TEXT NOT NULL CHECK (
        length(user_summary) BETWEEN 1 AND 1048576
    ),
    presentation_json TEXT NOT NULL CHECK (
        length(presentation_json) BETWEEN 2 AND 1048576
        AND json_valid(presentation_json)
    ),
    presentation_sha256 TEXT NOT NULL CHECK (
        length(presentation_sha256) = 64
        AND presentation_sha256 NOT GLOB '*[^0-9a-f]*'
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
    FOREIGN KEY (task_id, requirement_revision)
        REFERENCES task_requirement_revisions(task_id, revision),
    FOREIGN KEY (task_id, policy_revision)
        REFERENCES execution_policy_revisions(task_id, revision),
    FOREIGN KEY (task_id, forecast_revision)
        REFERENCES effort_forecast_revisions(task_id, revision),
    FOREIGN KEY (budget_id, budget_limit_revision)
        REFERENCES budget_limit_revisions(budget_id, revision),
    FOREIGN KEY (budget_id, budget_snapshot_revision)
        REFERENCES budget_snapshots(budget_id, revision),
    FOREIGN KEY (task_id, supersedes_revision)
        REFERENCES agent_plan_revisions(task_id, revision),
    CHECK (
        (revision = 1 AND supersedes_revision IS NULL
            AND redirect_message_id IS NULL)
        OR
        (revision > 1 AND supersedes_revision = revision - 1
            AND redirect_message_id IS NOT NULL)
    ),
    CHECK (
        (risk_level = 'routine' AND approval_required = 0)
        OR
        (risk_level IN ('elevated', 'protected') AND approval_required = 1)
    )
) STRICT;

CREATE TRIGGER agent_plan_revisions_consistency
BEFORE INSERT ON agent_plan_revisions
WHEN NOT EXISTS (
    SELECT 1
    FROM tasks AS task
    JOIN task_requirement_revisions AS requirement
      ON requirement.task_id = task.id
     AND requirement.revision = NEW.requirement_revision
    JOIN context_manifests AS context
      ON context.id = NEW.context_manifest_id
     AND context.repository_id = NEW.repository_id
     AND context.repository_revision = NEW.repository_revision
     AND context.requirement_sha256 = requirement.original_body_sha256
    JOIN effort_forecast_revisions AS forecast
      ON forecast.task_id = task.id
     AND forecast.revision = NEW.forecast_revision
     AND forecast.policy_revision = NEW.policy_revision
    JOIN budgets AS budget
      ON budget.id = NEW.budget_id
     AND budget.task_id = task.id
     AND budget.revision = NEW.budget_snapshot_revision
    JOIN budget_snapshots AS snapshot
      ON snapshot.budget_id = budget.id
     AND snapshot.revision = NEW.budget_snapshot_revision
     AND snapshot.limit_revision = NEW.budget_limit_revision
    WHERE task.id = NEW.task_id
      AND task.repository_id = NEW.repository_id
      AND requirement.risk_level = NEW.risk_level
      AND requirement.validation_profile = NEW.validation_profile
      AND requirement.requires_clarification = 0
      AND json_extract(NEW.canonical_json, '$.task_type') =
          json_extract(requirement.analysis_json, '$.task_type')
      AND json(json_extract(
          NEW.canonical_json, '$.explicit_validation_commands'
      )) = json(json_extract(
          requirement.analysis_json, '$.explicit_commands'
      ))
      AND NOT EXISTS (
          SELECT 1
          FROM json_each(
              requirement.analysis_json, '$.explicit_files'
          ) AS explicit_file
          WHERE NOT EXISTS (
              SELECT 1
              FROM json_each(
                  NEW.canonical_json, '$.expected_files'
              ) AS expected_file
              WHERE expected_file.value = explicit_file.value
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'agent plan bindings are inconsistent or unresolved');
END;

CREATE TRIGGER agent_plan_revisions_redirect_consistency
BEFORE INSERT ON agent_plan_revisions
WHEN NEW.redirect_message_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM tasks AS task
    JOIN messages AS message
      ON message.id = NEW.redirect_message_id
     AND message.thread_id = task.thread_id
     AND message.role = 'user'
    WHERE task.id = NEW.task_id
)
BEGIN
    SELECT RAISE(ABORT, 'plan redirect must bind a user message in the task thread');
END;

CREATE TRIGGER agent_plan_revisions_immutable_update
BEFORE UPDATE ON agent_plan_revisions
BEGIN
    SELECT RAISE(ABORT, 'agent plan revisions are immutable');
END;

CREATE TRIGGER agent_plan_revisions_immutable_delete
BEFORE DELETE ON agent_plan_revisions
BEGIN
    SELECT RAISE(ABORT, 'agent plan revisions are immutable');
END;

CREATE TABLE agent_plan_steps (
    task_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL,
    step_id TEXT NOT NULL CHECK (length(step_id) BETWEEN 1 AND 64),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    step_kind TEXT NOT NULL CHECK (step_kind IN (
        'edit', 'read-file', 'list-directory', 'search-text',
        'search-symbol', 'inspect-diff', 'git-status', 'git-history',
        'test', 'build', 'static-analysis'
    )),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 512),
    detail_redacted TEXT NOT NULL CHECK (
        length(detail_redacted) BETWEEN 1 AND 4096
    ),
    expected_files_json TEXT NOT NULL CHECK (
        json_valid(expected_files_json)
        AND json_type(expected_files_json) = 'array'
    ),
    validation_commands_json TEXT NOT NULL CHECK (
        json_valid(validation_commands_json)
        AND json_type(validation_commands_json) = 'array'
    ),
    completion_tools_json TEXT NOT NULL CHECK (
        json_valid(completion_tools_json)
        AND json_type(completion_tools_json) = 'array'
        AND json_array_length(completion_tools_json) BETWEEN 1 AND 64
    ),
    authority_needs_json TEXT NOT NULL CHECK (
        json_valid(authority_needs_json)
        AND json_type(authority_needs_json) = 'array'
    ),
    PRIMARY KEY (task_id, plan_revision, step_id),
    UNIQUE (task_id, plan_revision, ordinal),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER agent_plan_steps_immutable_update
BEFORE UPDATE ON agent_plan_steps
BEGIN
    SELECT RAISE(ABORT, 'agent plan steps are immutable');
END;

CREATE TRIGGER agent_plan_steps_immutable_delete
BEFORE DELETE ON agent_plan_steps
BEGIN
    SELECT RAISE(ABORT, 'agent plan steps are immutable');
END;

CREATE TABLE agent_plan_step_graph_nodes (
    task_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL,
    step_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    PRIMARY KEY (task_id, plan_revision, step_id, node_id),
    UNIQUE (task_id, plan_revision, step_id, ordinal),
    FOREIGN KEY (task_id, plan_revision, step_id)
        REFERENCES agent_plan_steps(task_id, plan_revision, step_id)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER agent_plan_step_graph_nodes_immutable_update
BEFORE UPDATE ON agent_plan_step_graph_nodes
BEGIN
    SELECT RAISE(ABORT, 'plan step graph links are immutable');
END;

CREATE TRIGGER agent_plan_step_graph_nodes_immutable_delete
BEFORE DELETE ON agent_plan_step_graph_nodes
BEGIN
    SELECT RAISE(ABORT, 'plan step graph links are immutable');
END;

CREATE TABLE agent_plan_approval_bindings (
    task_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL,
    approval_id TEXT NOT NULL UNIQUE REFERENCES approvals(id),
    plan_sha256 TEXT NOT NULL CHECK (
        length(plan_sha256) = 64
        AND plan_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (task_id, plan_revision),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER agent_plan_approval_bindings_consistency
BEFORE INSERT ON agent_plan_approval_bindings
WHEN NOT EXISTS (
    SELECT 1
    FROM agent_plan_revisions AS plan
    JOIN approvals AS approval
      ON approval.id = NEW.approval_id
     AND approval.task_id = plan.task_id
     AND approval.state = 'granted'
    WHERE plan.task_id = NEW.task_id
      AND plan.revision = NEW.plan_revision
      AND plan.content_sha256 = NEW.plan_sha256
      AND approval.scope = (
          'plan:' || NEW.task_id || ':' || NEW.plan_revision || ':'
          || NEW.plan_sha256
      )
)
BEGIN
    SELECT RAISE(ABORT, 'plan approval binding is inconsistent');
END;

CREATE TRIGGER agent_plan_approval_bindings_immutable_update
BEFORE UPDATE ON agent_plan_approval_bindings
BEGIN
    SELECT RAISE(ABORT, 'plan approval bindings are immutable');
END;

CREATE TRIGGER agent_plan_approval_bindings_immutable_delete
BEFORE DELETE ON agent_plan_approval_bindings
BEGIN
    SELECT RAISE(ABORT, 'plan approval bindings are immutable');
END;

CREATE TABLE run_plan_bindings (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    plan_sha256 TEXT NOT NULL CHECK (
        length(plan_sha256) = 64
        AND plan_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    policy_revision INTEGER NOT NULL CHECK (policy_revision >= 1),
    forecast_revision INTEGER NOT NULL CHECK (forecast_revision >= 1),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    budget_limit_revision INTEGER NOT NULL CHECK (budget_limit_revision >= 0),
    budget_snapshot_revision INTEGER NOT NULL CHECK (
        budget_snapshot_revision >= 0
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER run_plan_bindings_current_consistency
BEFORE INSERT ON run_plan_bindings
WHEN NOT EXISTS (
    SELECT 1
    FROM runs AS run
    JOIN agent_plan_revisions AS plan
      ON plan.task_id = NEW.task_id
     AND plan.revision = NEW.plan_revision
    JOIN run_execution_bindings AS execution
      ON execution.run_id = run.id
     AND execution.task_id = NEW.task_id
     AND execution.policy_revision = NEW.policy_revision
     AND execution.forecast_revision = NEW.forecast_revision
     AND execution.budget_id = NEW.budget_id
     AND execution.budget_limit_revision = NEW.budget_limit_revision
     AND execution.budget_snapshot_revision = NEW.budget_snapshot_revision
    WHERE run.id = NEW.run_id
      AND plan.revision = (
          SELECT MAX(current.revision)
          FROM agent_plan_revisions AS current
          WHERE current.task_id = NEW.task_id
      )
      AND plan.content_sha256 = NEW.plan_sha256
      AND plan.policy_revision = NEW.policy_revision
      AND plan.forecast_revision = NEW.forecast_revision
      AND plan.budget_id = NEW.budget_id
      AND plan.budget_limit_revision = NEW.budget_limit_revision
      AND plan.budget_snapshot_revision = NEW.budget_snapshot_revision
      AND (
          plan.approval_required = 0
          OR EXISTS (
              SELECT 1
              FROM agent_plan_approval_bindings AS approved
              WHERE approved.task_id = plan.task_id
                AND approved.plan_revision = plan.revision
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'run must bind the current approved exact plan');
END;

CREATE TRIGGER run_plan_bindings_immutable_update
BEFORE UPDATE ON run_plan_bindings
BEGIN
    SELECT RAISE(ABORT, 'run plan bindings are immutable');
END;

CREATE TRIGGER run_plan_bindings_immutable_delete
BEFORE DELETE ON run_plan_bindings
BEGIN
    SELECT RAISE(ABORT, 'run plan bindings are immutable');
END;

CREATE TABLE agent_tool_requests (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES run_plan_bindings(run_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    plan_step_id TEXT NOT NULL,
    model_request_id TEXT NOT NULL REFERENCES provider_logical_requests(id),
    tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 255),
    tool_schema_version INTEGER NOT NULL CHECK (tool_schema_version >= 1),
    arguments_redacted_json TEXT NOT NULL CHECK (
        json_valid(arguments_redacted_json)
    ),
    arguments_sha256 TEXT NOT NULL CHECK (
        length(arguments_sha256) = 64
        AND arguments_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    permission_decision_id TEXT REFERENCES permission_decisions(id),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (run_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision, plan_step_id)
        REFERENCES agent_plan_steps(task_id, plan_revision, step_id)
) STRICT;

CREATE TRIGGER agent_tool_requests_consistency
BEFORE INSERT ON agent_tool_requests
WHEN NOT EXISTS (
    SELECT 1
    FROM run_plan_bindings AS binding
    JOIN provider_logical_requests AS request
      ON request.id = NEW.model_request_id
     AND request.task_id = binding.task_id
     AND request.run_id = binding.run_id
    WHERE binding.run_id = NEW.run_id
      AND binding.task_id = NEW.task_id
      AND binding.plan_revision = NEW.plan_revision
)
BEGIN
    SELECT RAISE(ABORT, 'agent tool request attribution is inconsistent');
END;

CREATE TRIGGER agent_tool_requests_immutable_update
BEFORE UPDATE ON agent_tool_requests
BEGIN
    SELECT RAISE(ABORT, 'agent tool requests are immutable');
END;

CREATE TRIGGER agent_tool_requests_immutable_delete
BEFORE DELETE ON agent_tool_requests
BEGIN
    SELECT RAISE(ABORT, 'agent tool requests are immutable');
END;

CREATE TABLE agent_tool_results (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    tool_request_id TEXT NOT NULL UNIQUE REFERENCES agent_tool_requests(id),
    state TEXT NOT NULL CHECK (state IN (
        'succeeded', 'failed', 'cancelled', 'outcome-unknown'
    )),
    result_redacted_json TEXT NOT NULL CHECK (
        json_valid(result_redacted_json)
    ),
    result_sha256 TEXT NOT NULL CHECK (
        length(result_sha256) = 64
        AND result_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    command_execution_id TEXT REFERENCES command_executions(id),
    idempotency_key TEXT NOT NULL UNIQUE CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    )
) STRICT;

CREATE TRIGGER agent_tool_results_consistency
BEFORE INSERT ON agent_tool_results
WHEN NEW.command_execution_id IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM agent_tool_requests AS request
    JOIN command_executions AS command
      ON command.id = NEW.command_execution_id
     AND command.task_id = request.task_id
     AND command.run_id = request.run_id
    WHERE request.id = NEW.tool_request_id
)
BEGIN
    SELECT RAISE(ABORT, 'agent tool result command attribution is inconsistent');
END;

CREATE TRIGGER agent_tool_results_immutable_update
BEFORE UPDATE ON agent_tool_results
BEGIN
    SELECT RAISE(ABORT, 'agent tool results are immutable');
END;

CREATE TRIGGER agent_tool_results_immutable_delete
BEFORE DELETE ON agent_tool_results
BEGIN
    SELECT RAISE(ABORT, 'agent tool results are immutable');
END;

CREATE TABLE agent_plan_step_transitions (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES run_plan_bindings(run_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    plan_step_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    from_state TEXT NOT NULL CHECK (from_state IN (
        'pending', 'in-progress', 'implemented', 'validated', 'failed',
        'skipped'
    )),
    to_state TEXT NOT NULL CHECK (to_state IN (
        'pending', 'in-progress', 'implemented', 'validated', 'failed',
        'skipped'
    )),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 2048
    ),
    model_request_id TEXT REFERENCES provider_logical_requests(id),
    validation_id TEXT REFERENCES validations(id),
    tool_request_id TEXT REFERENCES agent_tool_requests(id),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (run_id, idempotency_key),
    UNIQUE (run_id, plan_step_id, sequence),
    FOREIGN KEY (task_id, plan_revision, plan_step_id)
        REFERENCES agent_plan_steps(task_id, plan_revision, step_id),
    CHECK (from_state != to_state),
    CHECK (
        (model_request_id IS NOT NULL AND validation_id IS NULL)
        OR (model_request_id IS NULL AND validation_id IS NOT NULL)
    ),
    CHECK (
        validation_id IS NULL
        OR (from_state = 'implemented' AND to_state = 'validated'
            AND tool_request_id IS NULL)
    )
) STRICT;

CREATE TRIGGER agent_plan_step_transitions_consistency
BEFORE INSERT ON agent_plan_step_transitions
WHEN NOT EXISTS (
    SELECT 1
    FROM run_plan_bindings AS binding
    WHERE binding.run_id = NEW.run_id
      AND binding.task_id = NEW.task_id
      AND binding.plan_revision = NEW.plan_revision
      AND (
          (
              NEW.model_request_id IS NOT NULL
              AND EXISTS (
                  SELECT 1
                  FROM provider_logical_requests AS request
                  WHERE request.id = NEW.model_request_id
                    AND request.task_id = binding.task_id
                    AND request.run_id = binding.run_id
              )
          )
          OR (
              NEW.validation_id IS NOT NULL
              AND EXISTS (
                  SELECT 1
                  FROM validations AS validation
                  WHERE validation.id = NEW.validation_id
                    AND validation.task_id = binding.task_id
                    AND validation.run_id = binding.run_id
                    AND validation.state = 'passed'
              )
          )
      )
      AND (
          NEW.tool_request_id IS NULL
          OR EXISTS (
              SELECT 1
              FROM agent_tool_requests AS tool
              WHERE tool.id = NEW.tool_request_id
                AND tool.task_id = NEW.task_id
                AND tool.run_id = NEW.run_id
                AND tool.plan_revision = NEW.plan_revision
                AND tool.plan_step_id = NEW.plan_step_id
                AND tool.model_request_id = NEW.model_request_id
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'plan step transition attribution is inconsistent');
END;

CREATE TABLE run_validation_profiles (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT PRIMARY KEY REFERENCES run_plan_bindings(run_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    profile_name TEXT NOT NULL CHECK (length(profile_name) BETWEEN 1 AND 255),
    profile_version TEXT NOT NULL CHECK (
        length(profile_version) BETWEEN 1 AND 255
    ),
    profile_digest TEXT NOT NULL CHECK (
        length(profile_digest) = 64
        AND profile_digest NOT GLOB '*[^0-9a-f]*'
    ),
    commands_json TEXT NOT NULL CHECK (
        json_valid(commands_json)
        AND json_type(commands_json) = 'array'
        AND json_array_length(commands_json) BETWEEN 1 AND 64
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (run_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER run_validation_profiles_consistency
BEFORE INSERT ON run_validation_profiles
WHEN NOT EXISTS (
    SELECT 1
    FROM run_plan_bindings AS binding
    JOIN agent_plan_revisions AS plan
      ON plan.task_id = binding.task_id
     AND plan.revision = binding.plan_revision
    WHERE binding.run_id = NEW.run_id
      AND binding.task_id = NEW.task_id
      AND binding.plan_revision = NEW.plan_revision
      AND CASE NEW.profile_name
          WHEN 'routine-v1' THEN 1
          WHEN 'elevated-v1' THEN 2
          WHEN 'protected-v1' THEN 3
          ELSE 0
      END >= CASE plan.validation_profile
          WHEN 'routine-v1' THEN 1
          WHEN 'elevated-v1' THEN 2
          WHEN 'protected-v1' THEN 3
          ELSE 4
      END
      AND json_array_length(NEW.commands_json) >=
          CASE NEW.profile_name
              WHEN 'routine-v1' THEN 1
              WHEN 'elevated-v1' THEN 2
              WHEN 'protected-v1' THEN 3
              ELSE 4
          END
      AND (
          SELECT COUNT(*)
          FROM json_each(NEW.commands_json) AS required_command
          WHERE json_extract(required_command.value, '$.required') = 1
      ) >= CASE NEW.profile_name
          WHEN 'routine-v1' THEN 1
          WHEN 'elevated-v1' THEN 2
          WHEN 'protected-v1' THEN 3
          ELSE 4
      END
      AND NOT EXISTS (
          SELECT 1
          FROM json_each(NEW.commands_json) AS left_command
          JOIN json_each(NEW.commands_json) AS right_command
            ON CAST(left_command.key AS INTEGER) <
               CAST(right_command.key AS INTEGER)
           AND (
               json_extract(left_command.value, '$.plan_command') =
                   json_extract(right_command.value, '$.plan_command')
               OR json_extract(
                   left_command.value, '$.command_fingerprint'
               ) = json_extract(
                   right_command.value, '$.command_fingerprint'
               )
           )
      )
      AND json_array_length(
          json_extract(plan.canonical_json, '$.validation_commands')
      ) = json_array_length(NEW.commands_json)
      AND NOT EXISTS (
          SELECT 1
          FROM json_each(NEW.commands_json) AS selected
          WHERE json_extract(selected.value, '$.ordinal') !=
                  CAST(selected.key AS INTEGER) + 1
             OR json_extract(selected.value, '$.plan_command') !=
                  json_extract(
                      plan.canonical_json,
                      '$.validation_commands[' || selected.key || ']'
                  )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'selected validation profile differs from run plan');
END;

CREATE TRIGGER run_validation_profiles_immutable_update
BEFORE UPDATE ON run_validation_profiles
BEGIN
    SELECT RAISE(ABORT, 'selected validation profiles are immutable');
END;

CREATE TRIGGER run_validation_profiles_immutable_delete
BEFORE DELETE ON run_validation_profiles
BEGIN
    SELECT RAISE(ABORT, 'selected validation profiles are immutable');
END;

CREATE TABLE plan_validation_operations (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES run_validation_profiles(run_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    profile_digest TEXT NOT NULL CHECK (
        length(profile_digest) = 64
        AND profile_digest NOT GLOB '*[^0-9a-f]*'
    ),
    round INTEGER NOT NULL CHECK (round >= 0),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    validation_id TEXT NOT NULL UNIQUE REFERENCES validations(id),
    command_id TEXT NOT NULL CHECK (length(command_id) BETWEEN 1 AND 255),
    command_fingerprint TEXT NOT NULL CHECK (
        length(command_fingerprint) = 64
        AND command_fingerprint NOT GLOB '*[^0-9a-f]*'
    ),
    command_execution_id TEXT REFERENCES command_executions(id),
    plan_step_ids_json TEXT NOT NULL CHECK (
        json_valid(plan_step_ids_json)
        AND json_type(plan_step_ids_json) = 'array'
        AND json_array_length(plan_step_ids_json) BETWEEN 1 AND 64
    ),
    validation_passed INTEGER NOT NULL CHECK (
        validation_passed IN (0, 1)
    ),
    presentation_redacted_json TEXT NOT NULL CHECK (
        length(presentation_redacted_json) BETWEEN 2 AND 1048576
        AND json_valid(presentation_redacted_json)
    ),
    presentation_sha256 TEXT NOT NULL CHECK (
        length(presentation_sha256) = 64
        AND presentation_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    failure_present INTEGER NOT NULL CHECK (failure_present IN (0, 1)),
    failure_changed_files_json TEXT NOT NULL CHECK (
        json_valid(failure_changed_files_json)
        AND json_type(failure_changed_files_json) = 'array'
    ),
    failure_plan_step_ids_json TEXT NOT NULL CHECK (
        json_valid(failure_plan_step_ids_json)
        AND json_type(failure_plan_step_ids_json) = 'array'
    ),
    output_truncated INTEGER NOT NULL CHECK (output_truncated IN (0, 1)),
    operation_digest TEXT NOT NULL CHECK (
        length(operation_digest) = 64
        AND operation_digest NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (run_id, round, ordinal),
    UNIQUE (run_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER plan_validation_operations_consistency
BEFORE INSERT ON plan_validation_operations
WHEN NOT EXISTS (
    SELECT 1
    FROM run_validation_profiles AS profile
    JOIN validations AS validation
      ON validation.id = NEW.validation_id
     AND validation.task_id = profile.task_id
     AND validation.run_id = profile.run_id
     AND validation.profile_name = profile.profile_name
    WHERE profile.run_id = NEW.run_id
      AND profile.task_id = NEW.task_id
      AND profile.plan_revision = NEW.plan_revision
      AND profile.profile_digest = NEW.profile_digest
      AND json_extract(
          profile.commands_json,
          '$[' || (NEW.ordinal - 1) || '].ordinal'
      ) = NEW.ordinal
      AND json_extract(
          profile.commands_json,
          '$[' || (NEW.ordinal - 1) || '].command_id'
      ) = NEW.command_id
      AND json_extract(
          profile.commands_json,
          '$[' || (NEW.ordinal - 1) || '].command_fingerprint'
      ) = NEW.command_fingerprint
      AND json(
          json_extract(
              profile.commands_json,
              '$[' || (NEW.ordinal - 1) || '].plan_step_ids'
          )
      ) = json(NEW.plan_step_ids_json)
      AND (
          (NEW.validation_passed = 1 AND validation.state = 'passed')
          OR (NEW.validation_passed = 0 AND validation.state != 'passed')
      )
      AND (
          (validation.state = 'passed'
              AND NEW.failure_present = 0
              AND json_array_length(NEW.failure_changed_files_json) = 0
              AND json_array_length(NEW.failure_plan_step_ids_json) = 0
              AND NEW.output_truncated = 0)
          OR (validation.state IN ('failed', 'cancelled')
              AND NEW.failure_present = 1)
      )
      AND (
          NEW.command_execution_id IS NULL
          OR EXISTS (
              SELECT 1 FROM command_executions AS command
              WHERE command.id = NEW.command_execution_id
                AND command.task_id = NEW.task_id
                AND command.run_id = NEW.run_id
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'validation operation attribution is inconsistent');
END;

CREATE TRIGGER plan_validation_operations_immutable_update
BEFORE UPDATE ON plan_validation_operations
BEGIN
    SELECT RAISE(ABORT, 'validation operations are immutable');
END;

CREATE TRIGGER plan_validation_operations_immutable_delete
BEFORE DELETE ON plan_validation_operations
BEGIN
    SELECT RAISE(ABORT, 'validation operations are immutable');
END;

CREATE TABLE plan_validation_attributions (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES run_plan_bindings(run_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    plan_step_id TEXT NOT NULL,
    validation_id TEXT NOT NULL REFERENCES validations(id),
    command_execution_id TEXT REFERENCES command_executions(id),
    repair_attempt_revision INTEGER CHECK (
        repair_attempt_revision IS NULL OR repair_attempt_revision >= 1
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (run_id, idempotency_key),
    UNIQUE (validation_id, plan_step_id),
    FOREIGN KEY (task_id, plan_revision, plan_step_id)
        REFERENCES agent_plan_steps(task_id, plan_revision, step_id)
) STRICT;

CREATE TRIGGER plan_validation_attributions_consistency
BEFORE INSERT ON plan_validation_attributions
WHEN NOT EXISTS (
    SELECT 1
    FROM run_plan_bindings AS binding
    JOIN validations AS validation
      ON validation.id = NEW.validation_id
     AND validation.task_id = binding.task_id
     AND validation.run_id = binding.run_id
    WHERE binding.run_id = NEW.run_id
      AND binding.task_id = NEW.task_id
      AND binding.plan_revision = NEW.plan_revision
      AND (
          NEW.command_execution_id IS NULL
          OR EXISTS (
              SELECT 1
              FROM command_executions AS command
              WHERE command.id = NEW.command_execution_id
                AND command.task_id = NEW.task_id
                AND command.run_id = NEW.run_id
          )
      )
      AND (
          NEW.repair_attempt_revision IS NULL
          OR EXISTS (
              SELECT 1
              FROM repair_attempts AS repair
              WHERE repair.run_id = NEW.run_id
                AND repair.task_id = NEW.task_id
                AND repair.plan_revision = NEW.plan_revision
                AND repair.revision = NEW.repair_attempt_revision
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'plan validation attribution is inconsistent');
END;

CREATE TABLE repair_attempts (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES run_plan_bindings(run_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
    failed_validation_id TEXT NOT NULL REFERENCES validations(id),
    pre_repair_checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 2048
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (run_id, revision),
    UNIQUE (run_id, ordinal),
    UNIQUE (run_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER repair_attempts_consistency
BEFORE INSERT ON repair_attempts
WHEN NOT EXISTS (
    SELECT 1
    FROM run_plan_bindings AS binding
    JOIN validations AS validation
      ON validation.id = NEW.failed_validation_id
     AND validation.task_id = binding.task_id
     AND validation.run_id = binding.run_id
     AND validation.state = 'failed'
    JOIN checkpoints AS checkpoint
      ON checkpoint.id = NEW.pre_repair_checkpoint_id
     AND checkpoint.task_id = binding.task_id
     AND checkpoint.run_id = binding.run_id
     AND checkpoint.state = 'ready'
    JOIN budgets AS budget
      ON budget.id = binding.budget_id
     AND NEW.ordinal <= budget.maximum_repair_rounds
    WHERE binding.run_id = NEW.run_id
      AND binding.task_id = NEW.task_id
      AND binding.plan_revision = NEW.plan_revision
)
BEGIN
    SELECT RAISE(ABORT, 'repair attempt attribution or budget is inconsistent');
END;

CREATE TABLE repair_attempt_outcomes (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    repair_attempt_revision INTEGER NOT NULL CHECK (
        repair_attempt_revision >= 1
    ),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'validation-passed', 'validation-failed', 'budget-exhausted',
        'stopped'
    )),
    post_repair_validation_id TEXT REFERENCES validations(id),
    unresolved_summary_redacted TEXT NOT NULL CHECK (
        length(unresolved_summary_redacted) BETWEEN 1 AND 4096
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (run_id, repair_attempt_revision),
    UNIQUE (run_id, idempotency_key),
    FOREIGN KEY (run_id, repair_attempt_revision)
        REFERENCES repair_attempts(run_id, revision),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision),
    CHECK (
        (outcome = 'validation-passed'
            AND post_repair_validation_id IS NOT NULL)
        OR outcome != 'validation-passed'
    )
) STRICT;

CREATE TRIGGER repair_attempt_outcomes_consistency
BEFORE INSERT ON repair_attempt_outcomes
WHEN NOT EXISTS (
    SELECT 1
    FROM repair_attempts AS repair
    WHERE repair.run_id = NEW.run_id
      AND repair.revision = NEW.repair_attempt_revision
      AND repair.task_id = NEW.task_id
      AND repair.plan_revision = NEW.plan_revision
      AND (
          NEW.post_repair_validation_id IS NULL
          OR EXISTS (
              SELECT 1
              FROM validations AS validation
              WHERE validation.id = NEW.post_repair_validation_id
                AND validation.task_id = NEW.task_id
                AND validation.run_id = NEW.run_id
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'repair outcome attribution is inconsistent');
END;

CREATE TABLE completion_candidates (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES run_plan_bindings(run_id),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    expected_task_revision INTEGER NOT NULL CHECK (
        expected_task_revision >= 0
    ),
    expected_run_revision INTEGER NOT NULL CHECK (
        expected_run_revision >= 0
    ),
    event_id TEXT NOT NULL CHECK (length(event_id) BETWEEN 1 AND 255),
    event_idempotency_key TEXT NOT NULL CHECK (
        length(event_idempotency_key) BETWEEN 1 AND 255
    ),
    repository_status_json TEXT NOT NULL CHECK (
        json_valid(repository_status_json)
    ),
    diff_summary_json TEXT NOT NULL CHECK (json_valid(diff_summary_json)),
    diff_sha256 TEXT NOT NULL CHECK (
        length(diff_sha256) = 64
        AND diff_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    validation_summary_json TEXT NOT NULL CHECK (
        json_valid(validation_summary_json)
    ),
    budget_summary_json TEXT NOT NULL CHECK (
        json_valid(budget_summary_json)
    ),
    assumptions_json TEXT NOT NULL CHECK (
        json_valid(assumptions_json)
        AND json_type(assumptions_json) = 'array'
    ),
    limitations_json TEXT NOT NULL CHECK (
        json_valid(limitations_json)
        AND json_type(limitations_json) = 'array'
    ),
    implementation_complete INTEGER NOT NULL CHECK (
        implementation_complete IN (0, 1)
    ),
    validation_complete INTEGER NOT NULL CHECK (
        validation_complete IN (0, 1)
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (run_id, revision),
    UNIQUE (run_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER completion_candidates_consistency
BEFORE INSERT ON completion_candidates
WHEN NOT EXISTS (
    SELECT 1
    FROM run_plan_bindings AS binding
    WHERE binding.run_id = NEW.run_id
      AND binding.task_id = NEW.task_id
      AND binding.plan_revision = NEW.plan_revision
)
BEGIN
    SELECT RAISE(ABORT, 'completion candidate attribution is inconsistent');
END;

CREATE TABLE task_review_decisions (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    completion_revision INTEGER NOT NULL CHECK (completion_revision >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    expected_task_revision INTEGER NOT NULL CHECK (
        expected_task_revision >= 0
    ),
    expected_run_revision INTEGER NOT NULL CHECK (
        expected_run_revision >= 0
    ),
    event_id TEXT NOT NULL CHECK (length(event_id) BETWEEN 1 AND 255),
    event_idempotency_key TEXT NOT NULL CHECK (
        length(event_idempotency_key) BETWEEN 1 AND 255
    ),
    decision TEXT NOT NULL CHECK (decision IN (
        'accept', 'request-repair', 'rollback', 'abandon'
    )),
    actor_reference TEXT NOT NULL CHECK (
        length(actor_reference) BETWEEN 1 AND 512
    ),
    authority_reference TEXT NOT NULL CHECK (
        length(authority_reference) BETWEEN 1 AND 512
    ),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 2048
    ),
    message_id TEXT REFERENCES messages(id),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (task_id, revision),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (run_id, completion_revision)
        REFERENCES completion_candidates(run_id, revision),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER task_review_decisions_consistency
BEFORE INSERT ON task_review_decisions
WHEN NOT EXISTS (
    SELECT 1
    FROM completion_candidates AS completion
    WHERE completion.run_id = NEW.run_id
      AND completion.revision = NEW.completion_revision
      AND completion.task_id = NEW.task_id
      AND completion.plan_revision = NEW.plan_revision
      AND (
          NEW.message_id IS NULL
          OR EXISTS (
              SELECT 1
              FROM tasks AS task
              JOIN messages AS message
                ON message.id = NEW.message_id
               AND message.thread_id = task.thread_id
               AND message.role = 'user'
              WHERE task.id = NEW.task_id
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'review decision attribution is inconsistent');
END;

CREATE TRIGGER agent_plan_step_transitions_immutable_update
BEFORE UPDATE ON agent_plan_step_transitions
BEGIN
    SELECT RAISE(ABORT, 'plan step transitions are immutable');
END;

CREATE TRIGGER plan_validation_attributions_immutable_update
BEFORE UPDATE ON plan_validation_attributions
BEGIN
    SELECT RAISE(ABORT, 'plan validation attributions are immutable');
END;

CREATE TRIGGER repair_attempts_immutable_update
BEFORE UPDATE ON repair_attempts
BEGIN
    SELECT RAISE(ABORT, 'repair attempts are immutable');
END;

CREATE TRIGGER repair_attempt_outcomes_immutable_update
BEFORE UPDATE ON repair_attempt_outcomes
BEGIN
    SELECT RAISE(ABORT, 'repair attempt outcomes are immutable');
END;

CREATE TRIGGER completion_candidates_immutable_update
BEFORE UPDATE ON completion_candidates
BEGIN
    SELECT RAISE(ABORT, 'completion candidates are immutable');
END;

CREATE TRIGGER task_review_decisions_immutable_update
BEFORE UPDATE ON task_review_decisions
BEGIN
    SELECT RAISE(ABORT, 'task review decisions are immutable');
END;

CREATE TRIGGER agent_plan_step_transitions_immutable_delete
BEFORE DELETE ON agent_plan_step_transitions
BEGIN
    SELECT RAISE(ABORT, 'plan step transitions are immutable');
END;

CREATE TRIGGER plan_validation_attributions_immutable_delete
BEFORE DELETE ON plan_validation_attributions
BEGIN
    SELECT RAISE(ABORT, 'plan validation attributions are immutable');
END;

CREATE TRIGGER repair_attempts_immutable_delete
BEFORE DELETE ON repair_attempts
BEGIN
    SELECT RAISE(ABORT, 'repair attempts are immutable');
END;

CREATE TRIGGER repair_attempt_outcomes_immutable_delete
BEFORE DELETE ON repair_attempt_outcomes
BEGIN
    SELECT RAISE(ABORT, 'repair attempt outcomes are immutable');
END;

CREATE TRIGGER completion_candidates_immutable_delete
BEFORE DELETE ON completion_candidates
BEGIN
    SELECT RAISE(ABORT, 'completion candidates are immutable');
END;

CREATE TRIGGER task_review_decisions_immutable_delete
BEFORE DELETE ON task_review_decisions
BEGIN
    SELECT RAISE(ABORT, 'task review decisions are immutable');
END;

CREATE INDEX agent_plan_revisions_current
    ON agent_plan_revisions(task_id, revision DESC);
CREATE INDEX agent_tool_requests_by_run_step
    ON agent_tool_requests(run_id, plan_step_id, created_at_unix_micros);
CREATE INDEX agent_plan_step_transitions_by_run_step
    ON agent_plan_step_transitions(
        run_id, plan_step_id, created_at_unix_micros
    );

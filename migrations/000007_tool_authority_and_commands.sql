-- Durable tool-schema, attributable authority, custom-command, and command
-- outcome metadata. Sensitive argument values remain redacted.

CREATE TABLE run_tool_schemas (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    )
) STRICT;

ALTER TABLE permission_decisions
    ADD COLUMN requester TEXT CHECK (
        requester IS NULL OR length(requester) BETWEEN 1 AND 255
    );
ALTER TABLE permission_decisions
    ADD COLUMN tool_name TEXT CHECK (
        tool_name IS NULL OR length(tool_name) BETWEEN 1 AND 255
    );
ALTER TABLE permission_decisions
    ADD COLUMN action_sha256 TEXT CHECK (
        action_sha256 IS NULL
        OR (
            length(action_sha256) = 64
            AND action_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    );
ALTER TABLE permission_decisions
    ADD COLUMN arguments_redacted_json TEXT CHECK (
        arguments_redacted_json IS NULL
        OR (
            json_valid(arguments_redacted_json)
            AND json_type(arguments_redacted_json) = 'array'
        )
    );
ALTER TABLE permission_decisions
    ADD COLUMN side_effects_json TEXT CHECK (
        side_effects_json IS NULL
        OR (
            json_valid(side_effects_json)
            AND json_type(side_effects_json) = 'array'
        )
    );
ALTER TABLE permission_decisions
    ADD COLUMN grant_mode TEXT CHECK (
        grant_mode IS NULL OR grant_mode IN ('allow-once', 'allow-for-task')
    );

CREATE TRIGGER permission_decisions_require_tool_fields
BEFORE INSERT ON permission_decisions
WHEN NEW.requester IS NULL
  OR NEW.tool_name IS NULL
  OR NEW.action_sha256 IS NULL
  OR NEW.arguments_redacted_json IS NULL
  OR NEW.side_effects_json IS NULL
  OR (NEW.decision = 'granted' AND NEW.grant_mode IS NULL)
  OR (NEW.decision = 'denied' AND NEW.grant_mode IS NOT NULL)
BEGIN
    SELECT RAISE(ABORT, 'permission decision tool fields are required');
END;

CREATE INDEX permission_decisions_by_task_action
    ON permission_decisions(task_id, capability, action_sha256, created_at_unix_micros DESC);

CREATE TABLE permission_grant_uses (
    permission_decision_id TEXT PRIMARY KEY
        REFERENCES permission_decisions(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    action_sha256 TEXT NOT NULL CHECK (
        length(action_sha256) = 64
        AND action_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    used_at_unix_micros INTEGER NOT NULL CHECK (
        used_at_unix_micros >= 0
    )
) STRICT;

CREATE TABLE custom_commands (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    executable TEXT NOT NULL CHECK (length(executable) BETWEEN 1 AND 4096),
    arguments_template_json TEXT NOT NULL CHECK (
        json_valid(arguments_template_json)
        AND json_type(arguments_template_json) = 'array'
    ),
    placeholders_json TEXT NOT NULL CHECK (
        json_valid(placeholders_json)
        AND json_type(placeholders_json) = 'array'
    ),
    command_version TEXT NOT NULL CHECK (
        length(command_version) BETWEEN 1 AND 255
    ),
    source TEXT NOT NULL CHECK (source IN ('user', 'repository', 'plugin')),
    approval_id TEXT REFERENCES approvals(id),
    approved INTEGER NOT NULL CHECK (approved IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    CHECK (
        (source = 'repository' AND approved = 1 AND approval_id IS NOT NULL)
        OR source != 'repository'
    ),
    UNIQUE (repository_id, name, command_version)
) STRICT;

CREATE TRIGGER custom_commands_immutable_update
BEFORE UPDATE ON custom_commands
BEGIN
    SELECT RAISE(ABORT, 'custom command definitions are immutable');
END;

ALTER TABLE command_executions
    ADD COLUMN tool_schema_version INTEGER CHECK (
        tool_schema_version IS NULL OR tool_schema_version > 0
    );
ALTER TABLE command_executions
    ADD COLUMN executable_path TEXT CHECK (
        executable_path IS NULL OR length(executable_path) BETWEEN 1 AND 4096
    );
ALTER TABLE command_executions
    ADD COLUMN duration_millis INTEGER CHECK (
        duration_millis IS NULL OR duration_millis >= 0
    );
ALTER TABLE command_executions
    ADD COLUMN timed_out INTEGER CHECK (timed_out IS NULL OR timed_out IN (0, 1));
ALTER TABLE command_executions
    ADD COLUMN cancelled INTEGER CHECK (cancelled IS NULL OR cancelled IN (0, 1));
ALTER TABLE command_executions
    ADD COLUMN stdout_truncated INTEGER CHECK (
        stdout_truncated IS NULL OR stdout_truncated IN (0, 1)
    );
ALTER TABLE command_executions
    ADD COLUMN stderr_truncated INTEGER CHECK (
        stderr_truncated IS NULL OR stderr_truncated IN (0, 1)
    );

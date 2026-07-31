-- Versioned checkpoint state, atomic request aliases, Git preservation, and
-- immutable recovery facts. Existing M03 checkpoint rows remain readable as
-- legacy records; every M15 row supplies schema_version and the closed state.

ALTER TABLE checkpoints
    ADD COLUMN schema_version INTEGER CHECK (
        schema_version IS NULL OR schema_version >= 1
    );
ALTER TABLE checkpoints
    ADD COLUMN repository_id TEXT REFERENCES repositories(id);
ALTER TABLE checkpoints
    ADD COLUMN worktree_binding_revision INTEGER CHECK (
        worktree_binding_revision IS NULL OR worktree_binding_revision >= 0
    );
ALTER TABLE checkpoints
    ADD COLUMN plan_revision INTEGER CHECK (
        plan_revision IS NULL OR plan_revision >= 1
    );
ALTER TABLE checkpoints
    ADD COLUMN base_revision TEXT CHECK (
        base_revision IS NULL OR (
            length(base_revision) IN (40, 64)
            AND base_revision NOT GLOB '*[^0-9a-f]*'
        )
    );
ALTER TABLE checkpoints
    ADD COLUMN worktree_head TEXT CHECK (
        worktree_head IS NULL OR (
            length(worktree_head) IN (40, 64)
            AND worktree_head NOT GLOB '*[^0-9a-f]*'
        )
    );
ALTER TABLE checkpoints
    ADD COLUMN preserved_revision TEXT CHECK (
        preserved_revision IS NULL OR (
            length(preserved_revision) IN (40, 64)
            AND preserved_revision NOT GLOB '*[^0-9a-f]*'
        )
    );
ALTER TABLE checkpoints
    ADD COLUMN preserved_ref TEXT CHECK (
        preserved_ref IS NULL OR (
            length(preserved_ref) BETWEEN 32 AND 512
            AND preserved_ref GLOB 'refs/codeflux/checkpoints/*'
        )
    );
ALTER TABLE checkpoints
    ADD COLUMN trigger_kind TEXT CHECK (
        trigger_kind IS NULL OR trigger_kind IN (
            'plan-approved', 'material-edit-applied', 'before-risky-action',
            'validation-succeeded', 'user-paused', 'graceful-shutdown'
        )
    );
ALTER TABLE checkpoints
    ADD COLUMN attribution_json TEXT CHECK (
        attribution_json IS NULL OR json_valid(attribution_json)
    );
ALTER TABLE checkpoints
    ADD COLUMN canonical_state_json TEXT CHECK (
        canonical_state_json IS NULL OR (
            length(canonical_state_json) BETWEEN 2 AND 1048576
            AND json_valid(canonical_state_json)
        )
    );
ALTER TABLE checkpoints
    ADD COLUMN state_sha256 TEXT CHECK (
        state_sha256 IS NULL OR (
            length(state_sha256) = 64
            AND state_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    );

CREATE UNIQUE INDEX checkpoints_by_run_state
    ON checkpoints(task_id, run_id, state_sha256)
    WHERE schema_version IS NOT NULL;

CREATE UNIQUE INDEX checkpoints_by_preserved_ref
    ON checkpoints(preserved_ref)
    WHERE preserved_ref IS NOT NULL;

CREATE TABLE checkpoint_request_aliases (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    capture_request_sha256 TEXT NOT NULL CHECK (
        length(capture_request_sha256) = 64
        AND capture_request_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (task_id, idempotency_key),
    UNIQUE (task_id, capture_request_sha256)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER checkpoint_request_aliases_consistency
BEFORE INSERT ON checkpoint_request_aliases
WHEN NOT EXISTS (
    SELECT 1
    FROM checkpoints AS checkpoint
    WHERE checkpoint.id = NEW.checkpoint_id
      AND checkpoint.task_id = NEW.task_id
      AND checkpoint.schema_version IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'checkpoint request alias must bind a versioned checkpoint');
END;

CREATE TRIGGER checkpoint_request_aliases_immutable_update
BEFORE UPDATE ON checkpoint_request_aliases
BEGIN
    SELECT RAISE(ABORT, 'checkpoint request aliases are immutable');
END;

CREATE TRIGGER checkpoint_request_aliases_immutable_delete
BEFORE DELETE ON checkpoint_request_aliases
BEGIN
    SELECT RAISE(ABORT, 'checkpoint request aliases are immutable');
END;

CREATE TRIGGER checkpoints_versioned_consistency
BEFORE INSERT ON checkpoints
WHEN NEW.schema_version IS NOT NULL
 AND NOT EXISTS (
    SELECT 1
    FROM run_plan_bindings AS run_plan
    JOIN agent_plan_revisions AS plan
      ON plan.task_id = run_plan.task_id
     AND plan.revision = run_plan.plan_revision
    JOIN worktree_bindings AS worktree
      ON worktree.task_id = run_plan.task_id
     AND worktree.repository_id = NEW.repository_id
    JOIN execution_policy_revisions AS policy
      ON policy.task_id = run_plan.task_id
     AND policy.revision = run_plan.policy_revision
    JOIN run_configurations AS run_configuration
      ON run_configuration.run_id = run_plan.run_id
    JOIN run_tool_schemas AS tools
      ON tools.run_id = run_plan.run_id
    JOIN budget_snapshots AS budget
      ON budget.budget_id = run_plan.budget_id
    JOIN task_events AS event
      ON event.task_id = NEW.task_id
     AND event.run_id = NEW.run_id
     AND event.sequence = NEW.event_sequence
     AND event.event_type = 'checkpoint.created'
    WHERE run_plan.task_id = NEW.task_id
      AND run_plan.run_id = NEW.run_id
      AND run_plan.plan_revision = NEW.plan_revision
      AND worktree.revision = NEW.worktree_binding_revision
      AND worktree.base_revision = NEW.base_revision
      AND worktree.head_revision = NEW.worktree_head
      AND budget.revision = json_extract(
          NEW.canonical_state_json, '$.budget.snapshot_revision'
      )
      AND NEW.state = 'ready'
      AND NEW.schema_version = json_extract(
          NEW.canonical_state_json, '$.schema_version'
      )
      AND NEW.task_id = json_extract(
          NEW.canonical_state_json, '$.task_id'
      )
      AND NEW.run_id = json_extract(
          NEW.canonical_state_json, '$.run_id'
      )
      AND NEW.repository_id = json_extract(
          NEW.canonical_state_json, '$.repository_id'
      )
      AND NEW.worktree_binding_revision = json_extract(
          NEW.canonical_state_json, '$.worktree_binding_revision'
      )
      AND NEW.plan_revision = json_extract(
          NEW.canonical_state_json, '$.plan_revision'
      )
      AND NEW.base_revision = json_extract(
          NEW.canonical_state_json, '$.base_revision'
      )
      AND NEW.worktree_head = json_extract(
          NEW.canonical_state_json, '$.worktree_head'
      )
      AND NEW.preserved_revision = json_extract(
          NEW.canonical_state_json, '$.preserved_revision'
      )
      AND NEW.worktree_diff_hash = json_extract(
          NEW.canonical_state_json, '$.diff_sha256'
      )
      AND NEW.event_sequence = json_extract(
          NEW.canonical_state_json, '$.last_durable_event_sequence'
      ) + 1
      AND NEW.preserved_ref = (
          'refs/codeflux/checkpoints/' || NEW.id
      )
      AND json_extract(
          NEW.canonical_state_json, '$.policy.revision'
      ) = policy.revision
      AND json_extract(
          NEW.canonical_state_json, '$.policy.version'
      ) = policy.policy_version
      AND json_extract(
          NEW.canonical_state_json, '$.policy.content_sha256'
      ) = policy.content_sha256
      AND json_extract(
          NEW.canonical_state_json, '$.provider.settings_revision'
      ) = run_configuration.settings_revision
      AND json_extract(
          NEW.canonical_state_json, '$.provider.run_configuration_sha256'
      ) = run_configuration.content_sha256
      AND json_extract(
          NEW.canonical_state_json, '$.provider.adapter'
      ) = json_extract(policy.canonical_json, '$.model.Provider.Adapter')
      AND json_extract(
          NEW.canonical_state_json, '$.provider.adapter_version'
      ) = json_extract(policy.canonical_json, '$.model.Provider.AdapterVersion')
      AND json_extract(
          NEW.canonical_state_json, '$.provider.provider'
      ) = json_extract(policy.canonical_json, '$.model.Provider.Provider')
      AND json_extract(
          NEW.canonical_state_json, '$.provider.provider_version'
      ) = json_extract(policy.canonical_json, '$.model.Provider.ProviderVersion')
      AND json_extract(
          NEW.canonical_state_json, '$.provider.model'
      ) = json_extract(policy.canonical_json, '$.model.Model')
      AND json_extract(
          NEW.canonical_state_json, '$.provider.model_revision'
      ) = json_extract(policy.canonical_json, '$.model.Revision')
      AND json_extract(
          NEW.canonical_state_json, '$.tools.schema_version'
      ) = tools.schema_version
      AND json_extract(
          NEW.canonical_state_json, '$.external_outcome_ambiguous'
      ) = CASE (
          SELECT count(*)
          FROM provider_logical_requests AS provider_request
          WHERE provider_request.task_id = NEW.task_id
            AND provider_request.run_id = NEW.run_id
            AND provider_request.state IN (
                'planned', 'in-flight', 'outcome-unknown'
            )
      ) + (
          SELECT count(*)
          FROM agent_tool_requests AS tool_request
          LEFT JOIN agent_tool_results AS tool_result
            ON tool_result.tool_request_id = tool_request.id
          WHERE tool_request.task_id = NEW.task_id
            AND tool_request.run_id = NEW.run_id
            AND (
                tool_result.id IS NULL
                OR tool_result.state = 'outcome-unknown'
            )
      ) + (
          SELECT count(*)
          FROM command_executions AS command
          WHERE command.task_id = NEW.task_id
            AND command.run_id = NEW.run_id
            AND command.state IN (
                'pending', 'awaiting-authority', 'authorized', 'running',
                'outcome-unknown'
            )
      ) WHEN 0 THEN 0 ELSE 1 END
      AND COALESCE(json_array_length(json_extract(
          NEW.canonical_state_json, '$.ambiguous_external_actions'
      )), 0) = (
          SELECT count(*)
          FROM provider_logical_requests AS provider_request
          WHERE provider_request.task_id = NEW.task_id
            AND provider_request.run_id = NEW.run_id
            AND provider_request.state IN (
                'planned', 'in-flight', 'outcome-unknown'
            )
      ) + (
          SELECT count(*)
          FROM agent_tool_requests AS tool_request
          LEFT JOIN agent_tool_results AS tool_result
            ON tool_result.tool_request_id = tool_request.id
          WHERE tool_request.task_id = NEW.task_id
            AND tool_request.run_id = NEW.run_id
            AND (
                tool_result.id IS NULL
                OR tool_result.state = 'outcome-unknown'
            )
      ) + (
          SELECT count(*)
          FROM command_executions AS command
          WHERE command.task_id = NEW.task_id
            AND command.run_id = NEW.run_id
            AND command.state IN (
                'pending', 'awaiting-authority', 'authorized', 'running',
                'outcome-unknown'
            )
      )
      AND NOT EXISTS (
          SELECT 1
          FROM json_each(
              NEW.canonical_state_json,
              '$.ambiguous_external_actions'
          ) AS ambiguous
          WHERE NOT EXISTS (
              SELECT 1
              FROM provider_logical_requests AS provider_request
              WHERE provider_request.task_id = NEW.task_id
                AND provider_request.run_id = NEW.run_id
                AND provider_request.state IN (
                    'planned', 'in-flight', 'outcome-unknown'
                )
                AND provider_request.id = json_extract(
                    ambiguous.value, '$.action_id'
                )
                AND json_extract(ambiguous.value, '$.kind') =
                    'provider-request'
                AND provider_request.request_sha256 = json_extract(
                    ambiguous.value, '$.intent_sha256'
                )
                AND COALESCE(json_extract(
                    ambiguous.value, '$.tool_request_id'
                ), '') = ''
              UNION ALL
              SELECT 1
              FROM agent_tool_requests AS tool_request
              LEFT JOIN agent_tool_results AS tool_result
                ON tool_result.tool_request_id = tool_request.id
              WHERE tool_request.task_id = NEW.task_id
                AND tool_request.run_id = NEW.run_id
                AND (
                    tool_result.id IS NULL
                    OR tool_result.state = 'outcome-unknown'
                )
                AND tool_request.id = json_extract(
                    ambiguous.value, '$.action_id'
                )
                AND json_extract(ambiguous.value, '$.kind') =
                    'tool-request'
                AND tool_request.arguments_sha256 = json_extract(
                    ambiguous.value, '$.intent_sha256'
                )
                AND tool_request.id = json_extract(
                    ambiguous.value, '$.tool_request_id'
                )
              UNION ALL
              SELECT 1
              FROM command_executions AS command
              WHERE command.task_id = NEW.task_id
                AND command.run_id = NEW.run_id
                AND command.state IN (
                    'pending', 'awaiting-authority', 'authorized', 'running',
                    'outcome-unknown'
                )
                AND command.id = json_extract(
                    ambiguous.value, '$.action_id'
                )
                AND json_extract(ambiguous.value, '$.kind') =
                    'command-execution'
                AND COALESCE(json_extract(
                    ambiguous.value, '$.tool_request_id'
                ), '') = ''
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'versioned checkpoint bindings are inconsistent');
END;

CREATE TRIGGER checkpoints_versioned_immutable_update
BEFORE UPDATE ON checkpoints
WHEN OLD.schema_version IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'versioned checkpoints are immutable');
END;

CREATE TRIGGER checkpoints_versioned_immutable_delete
BEFORE DELETE ON checkpoints
WHEN OLD.schema_version IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'versioned checkpoints are immutable');
END;

CREATE TABLE checkpoint_recovery_assessments (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    checkpoint_id TEXT REFERENCES checkpoints(id),
    classification TEXT NOT NULL CHECK (classification IN (
        'safe-resume', 'reconcile-required', 'patch-preservation-only',
        'unrecoverable'
    )),
    findings_redacted_json TEXT NOT NULL CHECK (
        json_valid(findings_redacted_json)
        AND json_type(findings_redacted_json) = 'array'
    ),
    divergences_redacted_json TEXT NOT NULL CHECK (
        json_valid(divergences_redacted_json)
        AND json_type(divergences_redacted_json) = 'array'
    ),
    patch_available INTEGER NOT NULL CHECK (patch_available IN (0, 1)),
    patch_locator TEXT CHECK (
        patch_locator IS NULL OR length(patch_locator) BETWEEN 1 AND 512
    ),
    patch_path TEXT CHECK (
        patch_path IS NULL OR length(patch_path) BETWEEN 1 AND 4096
    ),
    observation_sha256 TEXT NOT NULL CHECK (
        length(observation_sha256) = 64
        AND observation_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (task_id, idempotency_key),
    CHECK (
        (patch_available = 0 AND patch_locator IS NULL AND patch_path IS NULL)
        OR (
            patch_available = 1
            AND (patch_locator IS NOT NULL OR patch_path IS NOT NULL)
        )
    )
) STRICT;

CREATE TABLE checkpoint_recovery_attempts (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    assessment_id TEXT NOT NULL
        REFERENCES checkpoint_recovery_assessments(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    checkpoint_id TEXT REFERENCES checkpoints(id),
    action TEXT NOT NULL CHECK (action IN (
        'resume', 'reconcile', 'preserve-patch', 'abandon'
    )),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'started', 'succeeded', 'failed', 'cancelled', 'outcome-unknown'
    )),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 4096
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TRIGGER checkpoint_recovery_assessments_consistency
BEFORE INSERT ON checkpoint_recovery_assessments
WHEN NOT EXISTS (
    SELECT 1
    FROM runs AS run
    WHERE run.id = NEW.run_id
      AND run.task_id = NEW.task_id
      AND (
          NEW.checkpoint_id IS NULL
          OR EXISTS (
              SELECT 1
              FROM checkpoints AS checkpoint
              WHERE checkpoint.id = NEW.checkpoint_id
                AND checkpoint.task_id = NEW.task_id
                AND checkpoint.run_id = NEW.run_id
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'recovery assessment bindings are inconsistent');
END;

CREATE TRIGGER checkpoint_recovery_attempts_consistency
BEFORE INSERT ON checkpoint_recovery_attempts
WHEN NOT EXISTS (
    SELECT 1
    FROM checkpoint_recovery_assessments AS assessment
    WHERE assessment.id = NEW.assessment_id
      AND assessment.task_id = NEW.task_id
      AND assessment.run_id = NEW.run_id
      AND (
          assessment.checkpoint_id = NEW.checkpoint_id
          OR (
              assessment.checkpoint_id IS NULL
              AND NEW.checkpoint_id IS NULL
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'recovery attempt bindings are inconsistent');
END;

CREATE TABLE checkpoint_recovery_decisions (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    assessment_id TEXT NOT NULL UNIQUE
        REFERENCES checkpoint_recovery_assessments(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    checkpoint_id TEXT REFERENCES checkpoints(id),
    actor TEXT NOT NULL CHECK (actor IN ('user', 'system')),
    action TEXT NOT NULL CHECK (action IN (
        'resume', 'reconcile', 'preserve-patch', 'abandon'
    )),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 4096
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TRIGGER checkpoint_recovery_decisions_consistency
BEFORE INSERT ON checkpoint_recovery_decisions
WHEN NOT EXISTS (
    SELECT 1
    FROM checkpoint_recovery_assessments AS assessment
    WHERE assessment.id = NEW.assessment_id
      AND assessment.task_id = NEW.task_id
      AND assessment.run_id = NEW.run_id
      AND (
          assessment.checkpoint_id = NEW.checkpoint_id
          OR (
              assessment.checkpoint_id IS NULL
              AND NEW.checkpoint_id IS NULL
          )
      )
)
BEGIN
    SELECT RAISE(ABORT, 'recovery decision bindings are inconsistent');
END;

CREATE TRIGGER checkpoint_recovery_assessments_immutable_update
BEFORE UPDATE ON checkpoint_recovery_assessments
BEGIN
    SELECT RAISE(ABORT, 'checkpoint recovery assessments are immutable');
END;
CREATE TRIGGER checkpoint_recovery_assessments_immutable_delete
BEFORE DELETE ON checkpoint_recovery_assessments
BEGIN
    SELECT RAISE(ABORT, 'checkpoint recovery assessments are immutable');
END;
CREATE TRIGGER checkpoint_recovery_attempts_immutable_update
BEFORE UPDATE ON checkpoint_recovery_attempts
BEGIN
    SELECT RAISE(ABORT, 'checkpoint recovery attempts are immutable');
END;
CREATE TRIGGER checkpoint_recovery_attempts_immutable_delete
BEFORE DELETE ON checkpoint_recovery_attempts
BEGIN
    SELECT RAISE(ABORT, 'checkpoint recovery attempts are immutable');
END;
CREATE TRIGGER checkpoint_recovery_decisions_immutable_update
BEFORE UPDATE ON checkpoint_recovery_decisions
BEGIN
    SELECT RAISE(ABORT, 'checkpoint recovery decisions are immutable');
END;
CREATE TRIGGER checkpoint_recovery_decisions_immutable_delete
BEFORE DELETE ON checkpoint_recovery_decisions
BEGIN
    SELECT RAISE(ABORT, 'checkpoint recovery decisions are immutable');
END;

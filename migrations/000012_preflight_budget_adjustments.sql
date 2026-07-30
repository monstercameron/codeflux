-- Durable pre-approval budget revisions and exact budget-snapshot preflights.

CREATE TABLE preapproval_budget_adjustments (
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    previous_limit_revision INTEGER NOT NULL CHECK (
        previous_limit_revision >= 0
    ),
    adjusted_limit_revision INTEGER NOT NULL CHECK (
        adjusted_limit_revision = previous_limit_revision + 1
    ),
    previous_budget_json TEXT NOT NULL CHECK (
        length(previous_budget_json) BETWEEN 2 AND 65536
        AND json_valid(previous_budget_json)
    ),
    adjusted_budget_json TEXT NOT NULL CHECK (
        length(adjusted_budget_json) BETWEEN 2 AND 65536
        AND json_valid(adjusted_budget_json)
    ),
    actor_reference TEXT NOT NULL CHECK (
        length(actor_reference) BETWEEN 1 AND 512
    ),
    authority_reference TEXT NOT NULL CHECK (
        length(authority_reference) BETWEEN 1 AND 512
    ),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 512
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (budget_id, revision),
    UNIQUE (budget_id, idempotency_key),
    FOREIGN KEY (budget_id, previous_limit_revision)
        REFERENCES budget_limit_revisions(budget_id, revision),
    FOREIGN KEY (budget_id, adjusted_limit_revision)
        REFERENCES budget_limit_revisions(budget_id, revision)
) STRICT;

CREATE TRIGGER preapproval_budget_adjustments_immutable_update
BEFORE UPDATE ON preapproval_budget_adjustments
BEGIN
    SELECT RAISE(ABORT, 'pre-approval budget adjustments are immutable');
END;

CREATE TRIGGER preapproval_budget_adjustments_immutable_delete
BEFORE DELETE ON preapproval_budget_adjustments
BEGIN
    SELECT RAISE(ABORT, 'pre-approval budget adjustments are immutable');
END;

ALTER TABLE task_execution_preflights
    ADD COLUMN budget_snapshot_revision INTEGER NOT NULL DEFAULT 0 CHECK (
        budget_snapshot_revision >= 0
    );

ALTER TABLE run_execution_bindings
    ADD COLUMN budget_snapshot_revision INTEGER NOT NULL DEFAULT 0 CHECK (
        budget_snapshot_revision >= 0
    );

CREATE TRIGGER task_execution_preflights_budget_snapshot_consistency
BEFORE INSERT ON task_execution_preflights
WHEN NOT EXISTS (
    SELECT 1
    FROM budget_snapshots AS snapshot
    WHERE snapshot.budget_id = NEW.budget_id
      AND snapshot.revision = NEW.budget_snapshot_revision
      AND snapshot.limit_revision = NEW.budget_limit_revision
      AND snapshot.task_id = NEW.task_id
)
BEGIN
    SELECT RAISE(ABORT, 'execution preflight budget snapshot is inconsistent');
END;

CREATE TRIGGER run_execution_bindings_budget_snapshot_consistency
BEFORE INSERT ON run_execution_bindings
WHEN NOT EXISTS (
    SELECT 1
    FROM task_execution_preflights AS preflight
    WHERE preflight.task_id = NEW.task_id
      AND preflight.revision = NEW.preflight_revision
      AND preflight.budget_id = NEW.budget_id
      AND preflight.budget_limit_revision = NEW.budget_limit_revision
      AND preflight.budget_snapshot_revision = NEW.budget_snapshot_revision
)
BEGIN
    SELECT RAISE(ABORT, 'run budget snapshot binding is inconsistent');
END;

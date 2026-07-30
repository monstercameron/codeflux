-- Exact task-budget limits, reservations, settlement, warnings, and snapshots.

CREATE TABLE budget_limit_revisions (
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    revision INTEGER NOT NULL CHECK (revision >= 0),
    warning_cost_minor_numerator INTEGER NOT NULL CHECK (
        warning_cost_minor_numerator >= 0
    ),
    warning_cost_minor_denominator INTEGER NOT NULL CHECK (
        warning_cost_minor_denominator > 0
    ),
    hard_cost_minor_numerator INTEGER NOT NULL CHECK (
        hard_cost_minor_numerator >= 0
    ),
    hard_cost_minor_denominator INTEGER NOT NULL CHECK (
        hard_cost_minor_denominator > 0
    ),
    currency TEXT NOT NULL CHECK (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    ),
    warning_tokens INTEGER NOT NULL CHECK (warning_tokens >= 0),
    hard_tokens INTEGER NOT NULL CHECK (hard_tokens >= warning_tokens),
    approval_id TEXT REFERENCES approvals(id),
    authority_kind TEXT NOT NULL CHECK (authority_kind IN (
        'initial-policy', 'preapproval-user-adjustment', 'user-approval'
    )),
    actor_kind TEXT NOT NULL CHECK (actor_kind IN (
        'system', 'user', 'coordinator'
    )),
    actor_reference TEXT NOT NULL CHECK (
        length(actor_reference) BETWEEN 1 AND 255
    ),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 2048
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    provenance_json TEXT NOT NULL CHECK (
        length(provenance_json) BETWEEN 2 AND 65536
        AND json_valid(provenance_json)
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (budget_id, revision),
    UNIQUE (budget_id, idempotency_key),
    CHECK (
        (authority_kind IN (
            'initial-policy', 'preapproval-user-adjustment'
        ) AND approval_id IS NULL)
        OR
        (authority_kind = 'user-approval' AND approval_id IS NOT NULL)
    )
) STRICT;

CREATE UNIQUE INDEX budget_limit_revisions_approval_once
    ON budget_limit_revisions(approval_id)
    WHERE approval_id IS NOT NULL;

CREATE TRIGGER budget_limit_revisions_immutable_update
BEFORE UPDATE ON budget_limit_revisions
BEGIN
    SELECT RAISE(ABORT, 'budget limit revisions are immutable');
END;

CREATE TRIGGER budget_limit_revisions_immutable_delete
BEFORE DELETE ON budget_limit_revisions
BEGIN
    SELECT RAISE(ABORT, 'budget limit revisions are immutable');
END;

CREATE TABLE budget_reservations (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    operation_id TEXT NOT NULL CHECK (
        length(operation_id) BETWEEN 1 AND 255
    ),
    attempt_id TEXT CHECK (
        attempt_id IS NULL OR length(attempt_id) BETWEEN 1 AND 255
    ),
    retry_ordinal INTEGER NOT NULL CHECK (retry_ordinal >= 0),
    category TEXT NOT NULL CHECK (category IN (
        'model', 'tool', 'infrastructure'
    )),
    provider_call_slots INTEGER NOT NULL CHECK (
        provider_call_slots >= 0
    ),
    state TEXT NOT NULL CHECK (state IN (
        'active', 'committed', 'released'
    )),
    cost_bound_minor_numerator INTEGER NOT NULL CHECK (
        cost_bound_minor_numerator >= 0
    ),
    cost_bound_minor_denominator INTEGER NOT NULL CHECK (
        cost_bound_minor_denominator > 0
    ),
    currency TEXT NOT NULL CHECK (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    ),
    token_bound_known INTEGER NOT NULL CHECK (
        token_bound_known IN (0, 1)
    ),
    token_bound INTEGER CHECK (token_bound >= 0),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    provenance_json TEXT NOT NULL CHECK (
        length(provenance_json) BETWEEN 2 AND 65536
        AND json_valid(provenance_json)
    ),
    settlement_reason_redacted TEXT CHECK (
        settlement_reason_redacted IS NULL
        OR length(settlement_reason_redacted) BETWEEN 1 AND 2048
    ),
    settlement_provenance_json TEXT CHECK (
        settlement_provenance_json IS NULL
        OR (
            length(settlement_provenance_json) BETWEEN 2 AND 65536
            AND json_valid(settlement_provenance_json)
        )
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    settled_at_unix_micros INTEGER CHECK (
        settled_at_unix_micros IS NULL
        OR settled_at_unix_micros >= created_at_unix_micros
    ),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (budget_id, idempotency_key),
    UNIQUE (budget_id, operation_id, retry_ordinal, category),
    CHECK (
        (token_bound_known = 0 AND token_bound IS NULL)
        OR
        (token_bound_known = 1 AND token_bound IS NOT NULL)
    ),
    CHECK (
        (category = 'model' AND provider_call_slots >= 1)
        OR
        (category != 'model' AND provider_call_slots = 0)
    ),
    CHECK (
        (state = 'active' AND settled_at_unix_micros IS NULL)
        OR
        (state != 'active' AND settled_at_unix_micros IS NOT NULL)
    )
) STRICT;

CREATE TRIGGER budget_reservations_identity_immutable
BEFORE UPDATE ON budget_reservations
WHEN NEW.id != OLD.id
    OR NEW.budget_id != OLD.budget_id
    OR NEW.task_id != OLD.task_id
    OR NEW.operation_id != OLD.operation_id
    OR NEW.attempt_id IS NOT OLD.attempt_id
    OR NEW.retry_ordinal != OLD.retry_ordinal
    OR NEW.category != OLD.category
    OR NEW.provider_call_slots != OLD.provider_call_slots
    OR NEW.cost_bound_minor_numerator
        != OLD.cost_bound_minor_numerator
    OR NEW.cost_bound_minor_denominator
        != OLD.cost_bound_minor_denominator
    OR NEW.currency != OLD.currency
    OR NEW.token_bound_known != OLD.token_bound_known
    OR NEW.token_bound IS NOT OLD.token_bound
    OR NEW.idempotency_key != OLD.idempotency_key
    OR NEW.provenance_json != OLD.provenance_json
    OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN
    SELECT RAISE(ABORT, 'budget reservation identity is immutable');
END;

CREATE TRIGGER budget_reservations_transition_guard
BEFORE UPDATE ON budget_reservations
WHEN OLD.state != 'active'
    OR NEW.state NOT IN ('committed', 'released')
    OR NEW.settlement_reason_redacted IS NULL
    OR NEW.settlement_provenance_json IS NULL
    OR NEW.settled_at_unix_micros IS NULL
    OR NEW.revision != OLD.revision + 1
BEGIN
    SELECT RAISE(ABORT, 'budget reservation transition is invalid');
END;

CREATE TRIGGER budget_reservations_reject_delete
BEFORE DELETE ON budget_reservations
BEGIN
    SELECT RAISE(ABORT, 'budget reservations cannot be deleted');
END;

CREATE INDEX budget_reservations_active
    ON budget_reservations(budget_id, state, created_at_unix_micros, id);

CREATE INDEX budget_reservations_by_operation
    ON budget_reservations(operation_id, retry_ordinal, category);

CREATE TABLE budget_usage_postings (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    reservation_id TEXT NOT NULL UNIQUE REFERENCES budget_reservations(id),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    category TEXT NOT NULL CHECK (category IN (
        'model', 'tool', 'infrastructure'
    )),
    cost_known INTEGER NOT NULL CHECK (cost_known IN (0, 1)),
    cost_minor_numerator INTEGER,
    cost_minor_denominator INTEGER,
    charged_cost_minor_numerator INTEGER NOT NULL CHECK (
        charged_cost_minor_numerator >= 0
    ),
    charged_cost_minor_denominator INTEGER NOT NULL CHECK (
        charged_cost_minor_denominator > 0
    ),
    currency TEXT NOT NULL CHECK (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    ),
    tokens_known INTEGER NOT NULL CHECK (tokens_known IN (0, 1)),
    actual_tokens INTEGER CHECK (actual_tokens >= 0),
    charged_tokens INTEGER CHECK (charged_tokens >= 0),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    provenance_json TEXT NOT NULL CHECK (
        length(provenance_json) BETWEEN 2 AND 65536
        AND json_valid(provenance_json)
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (budget_id, idempotency_key),
    CHECK (
        (cost_known = 0 AND cost_minor_numerator IS NULL
            AND cost_minor_denominator IS NULL)
        OR
        (cost_known = 1 AND cost_minor_numerator >= 0
            AND cost_minor_denominator > 0)
    ),
    CHECK (
        (tokens_known = 0 AND actual_tokens IS NULL)
        OR
        (tokens_known = 1 AND actual_tokens IS NOT NULL)
    )
) STRICT;

CREATE TRIGGER budget_usage_postings_immutable_update
BEFORE UPDATE ON budget_usage_postings
BEGIN
    SELECT RAISE(ABORT, 'budget usage postings are immutable');
END;

CREATE TRIGGER budget_usage_postings_immutable_delete
BEFORE DELETE ON budget_usage_postings
BEGIN
    SELECT RAISE(ABORT, 'budget usage postings are immutable');
END;

CREATE INDEX budget_usage_postings_by_budget
    ON budget_usage_postings(budget_id, created_at_unix_micros, id);

CREATE TABLE budget_reconciliation_intents (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    reservation_id TEXT NOT NULL UNIQUE REFERENCES budget_reservations(id),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    cost_known INTEGER NOT NULL CHECK (cost_known IN (0, 1)),
    cost_minor_numerator INTEGER,
    cost_minor_denominator INTEGER,
    currency TEXT NOT NULL CHECK (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    ),
    tokens_known INTEGER NOT NULL CHECK (tokens_known IN (0, 1)),
    actual_tokens INTEGER CHECK (actual_tokens >= 0),
    reason_redacted TEXT NOT NULL CHECK (
        length(reason_redacted) BETWEEN 1 AND 2048
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    provenance_json TEXT NOT NULL CHECK (
        length(provenance_json) BETWEEN 2 AND 65536
        AND json_valid(provenance_json)
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (budget_id, idempotency_key),
    CHECK (
        (cost_known = 0 AND cost_minor_numerator IS NULL
            AND cost_minor_denominator IS NULL)
        OR
        (cost_known = 1 AND cost_minor_numerator >= 0
            AND cost_minor_denominator > 0)
    ),
    CHECK (
        (tokens_known = 0 AND actual_tokens IS NULL)
        OR
        (tokens_known = 1 AND actual_tokens IS NOT NULL)
    )
) STRICT;

CREATE TRIGGER budget_reconciliation_intents_immutable_update
BEFORE UPDATE ON budget_reconciliation_intents
BEGIN
    SELECT RAISE(ABORT, 'budget reconciliation intents are immutable');
END;

CREATE TRIGGER budget_reconciliation_intents_immutable_delete
BEFORE DELETE ON budget_reconciliation_intents
BEGIN
    SELECT RAISE(ABORT, 'budget reconciliation intents are immutable');
END;

CREATE INDEX budget_reconciliation_intents_pending
    ON budget_reconciliation_intents(budget_id, created_at_unix_micros, id);

CREATE TABLE budget_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    limit_revision INTEGER NOT NULL CHECK (limit_revision >= 0),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'warning-cost', 'warning-tokens', 'hard-cap-cost',
        'hard-cap-tokens', 'accounting-unknown',
        'reconciliation-pending', 'limit-raised'
    )),
    payload_json TEXT NOT NULL CHECK (
        length(payload_json) BETWEEN 2 AND 65536
        AND json_valid(payload_json)
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (budget_id, limit_revision, event_type),
    UNIQUE (budget_id, idempotency_key)
) STRICT;

CREATE TRIGGER budget_events_immutable_update
BEFORE UPDATE ON budget_events
BEGIN
    SELECT RAISE(ABORT, 'budget events are immutable');
END;

CREATE TRIGGER budget_events_immutable_delete
BEFORE DELETE ON budget_events
BEGIN
    SELECT RAISE(ABORT, 'budget events are immutable');
END;

CREATE TABLE budget_snapshots (
    budget_id TEXT NOT NULL REFERENCES budgets(id),
    revision INTEGER NOT NULL CHECK (revision >= 0),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    limit_revision INTEGER NOT NULL CHECK (limit_revision >= 0),
    currency TEXT NOT NULL CHECK (
        length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'
    ),
    reserved_cost_minor_numerator INTEGER NOT NULL CHECK (
        reserved_cost_minor_numerator >= 0
    ),
    reserved_cost_minor_denominator INTEGER NOT NULL CHECK (
        reserved_cost_minor_denominator > 0
    ),
    charged_cost_minor_numerator INTEGER NOT NULL CHECK (
        charged_cost_minor_numerator >= 0
    ),
    charged_cost_minor_denominator INTEGER NOT NULL CHECK (
        charged_cost_minor_denominator > 0
    ),
    actual_cost_known INTEGER NOT NULL CHECK (
        actual_cost_known IN (0, 1)
    ),
    actual_cost_minor_numerator INTEGER NOT NULL CHECK (
        actual_cost_minor_numerator >= 0
    ),
    actual_cost_minor_denominator INTEGER NOT NULL CHECK (
        actual_cost_minor_denominator > 0
    ),
    cost_accounting_unknown INTEGER NOT NULL CHECK (
        cost_accounting_unknown IN (0, 1)
    ),
    reserved_tokens INTEGER NOT NULL CHECK (reserved_tokens >= 0),
    charged_tokens INTEGER NOT NULL CHECK (charged_tokens >= 0),
    actual_tokens_known INTEGER NOT NULL CHECK (
        actual_tokens_known IN (0, 1)
    ),
    actual_tokens INTEGER NOT NULL CHECK (actual_tokens >= 0),
    token_accounting_unknown INTEGER NOT NULL CHECK (
        token_accounting_unknown IN (0, 1)
    ),
    model_reservations INTEGER NOT NULL CHECK (model_reservations >= 0),
    tool_reservations INTEGER NOT NULL CHECK (tool_reservations >= 0),
    infrastructure_reservations INTEGER NOT NULL CHECK (
        infrastructure_reservations >= 0
    ),
    provider_call_slots INTEGER NOT NULL CHECK (
        provider_call_slots >= 0
    ),
    reconciliation_pending INTEGER NOT NULL CHECK (
        reconciliation_pending IN (0, 1)
    ),
    warning_reached INTEGER NOT NULL CHECK (warning_reached IN (0, 1)),
    hard_cap_reached INTEGER NOT NULL CHECK (hard_cap_reached IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (budget_id, revision)
) STRICT;

CREATE TRIGGER budget_snapshots_immutable_update
BEFORE UPDATE ON budget_snapshots
BEGIN
    SELECT RAISE(ABORT, 'budget snapshots are immutable');
END;

CREATE TRIGGER budget_snapshots_immutable_delete
BEFORE DELETE ON budget_snapshots
BEGIN
    SELECT RAISE(ABORT, 'budget snapshots are immutable');
END;

INSERT INTO budget_limit_revisions (
    budget_id, revision,
    warning_cost_minor_numerator, warning_cost_minor_denominator,
    hard_cost_minor_numerator, hard_cost_minor_denominator,
    currency, warning_tokens, hard_tokens, approval_id,
    authority_kind, actor_kind, actor_reference, reason_redacted,
    idempotency_key, provenance_json, created_at_unix_micros
)
SELECT id, 0, warning_cost_minor, 1, hard_stop_cost_minor, 1,
       currency, warning_tokens, hard_stop_tokens, NULL,
       'initial-policy', 'system', 'migration-000010',
       'initial exact budget limit',
       'initial-budget-limit', '{"schema_version":1,"source":"migration"}',
       created_at_unix_micros
FROM budgets;

INSERT INTO budget_snapshots (
    budget_id, revision, task_id, limit_revision, currency,
    reserved_cost_minor_numerator, reserved_cost_minor_denominator,
    charged_cost_minor_numerator, charged_cost_minor_denominator,
    actual_cost_known, actual_cost_minor_numerator,
    actual_cost_minor_denominator, cost_accounting_unknown,
    reserved_tokens, charged_tokens, actual_tokens_known, actual_tokens,
    token_accounting_unknown, model_reservations, tool_reservations,
    infrastructure_reservations, provider_call_slots,
    reconciliation_pending,
    warning_reached, hard_cap_reached,
    created_at_unix_micros
)
SELECT id, revision, task_id, 0, currency,
       reserved_cost_minor, 1, actual_cost_minor, 1,
       1, actual_cost_minor, 1, 0,
       0, actual_tokens, 1, actual_tokens, 0,
       0, 0, 0, 0, 0,
       CASE WHEN reserved_cost_minor + actual_cost_minor >= warning_cost_minor
                  OR actual_tokens >= warning_tokens THEN 1 ELSE 0 END,
       CASE WHEN reserved_cost_minor + actual_cost_minor >= hard_stop_cost_minor
                  OR actual_tokens >= hard_stop_tokens THEN 1 ELSE 0 END,
       updated_at_unix_micros
FROM budgets;

CREATE TRIGGER budgets_initialize_exact_ledger
AFTER INSERT ON budgets
BEGIN
    INSERT INTO budget_limit_revisions (
        budget_id, revision,
        warning_cost_minor_numerator, warning_cost_minor_denominator,
        hard_cost_minor_numerator, hard_cost_minor_denominator,
        currency, warning_tokens, hard_tokens, approval_id,
        authority_kind, actor_kind, actor_reference, reason_redacted,
        idempotency_key, provenance_json, created_at_unix_micros
    ) VALUES (
        NEW.id, 0, NEW.warning_cost_minor, 1, NEW.hard_stop_cost_minor, 1,
        NEW.currency, NEW.warning_tokens, NEW.hard_stop_tokens, NULL,
        'initial-policy', 'system', 'fixed-policy',
        'initial exact budget limit',
        'initial-budget-limit', '{"schema_version":1,"source":"fixed-policy"}',
        NEW.created_at_unix_micros
    );
    INSERT INTO budget_snapshots (
        budget_id, revision, task_id, limit_revision, currency,
        reserved_cost_minor_numerator, reserved_cost_minor_denominator,
        charged_cost_minor_numerator, charged_cost_minor_denominator,
        actual_cost_known, actual_cost_minor_numerator,
        actual_cost_minor_denominator, cost_accounting_unknown,
        reserved_tokens, charged_tokens, actual_tokens_known, actual_tokens,
        token_accounting_unknown, model_reservations, tool_reservations,
        infrastructure_reservations, provider_call_slots,
        reconciliation_pending,
        warning_reached, hard_cap_reached,
        created_at_unix_micros
    ) VALUES (
        NEW.id, NEW.revision, NEW.task_id, 0, NEW.currency,
        NEW.reserved_cost_minor, 1, NEW.actual_cost_minor, 1,
        1, NEW.actual_cost_minor, 1, 0,
        0, NEW.actual_tokens, 1, NEW.actual_tokens, 0,
        0, 0, 0, 0, 0,
        CASE WHEN NEW.reserved_cost_minor + NEW.actual_cost_minor
                       >= NEW.warning_cost_minor
                  OR NEW.actual_tokens >= NEW.warning_tokens
             THEN 1 ELSE 0 END,
        CASE WHEN NEW.reserved_cost_minor + NEW.actual_cost_minor
                       >= NEW.hard_stop_cost_minor
                  OR NEW.actual_tokens >= NEW.hard_stop_tokens
             THEN 1 ELSE 0 END,
        NEW.updated_at_unix_micros
    );
END;

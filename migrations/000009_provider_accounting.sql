-- Immutable provider configuration, request-attempt, usage, and cost evidence.

CREATE TABLE provider_configuration_revisions (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    adapter_name TEXT NOT NULL CHECK (length(adapter_name) BETWEEN 1 AND 255),
    adapter_version TEXT NOT NULL CHECK (length(adapter_version) BETWEEN 1 AND 255),
    provider_version TEXT NOT NULL CHECK (
        length(provider_version) BETWEEN 1 AND 255
    ),
    endpoint_redacted TEXT NOT NULL CHECK (length(endpoint_redacted) BETWEEN 1 AND 2048),
    capabilities_json TEXT NOT NULL CHECK (
        length(capabilities_json) BETWEEN 2 AND 65536
        AND json_valid(capabilities_json)
    ),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    approval_reference TEXT CHECK (
        approval_reference IS NULL
        OR length(approval_reference) BETWEEN 1 AND 255
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (provider_id, revision),
    UNIQUE (provider_id, idempotency_key),
    UNIQUE (id, provider_id),
    UNIQUE (
        id, provider_id, adapter_name, adapter_version, provider_version
    )
) STRICT;

CREATE TRIGGER provider_configuration_revisions_immutable_update
BEFORE UPDATE ON provider_configuration_revisions
BEGIN
    SELECT RAISE(ABORT, 'provider configuration revisions are immutable');
END;

CREATE TRIGGER provider_configuration_revisions_immutable_delete
BEFORE DELETE ON provider_configuration_revisions
BEGIN
    SELECT RAISE(ABORT, 'provider configuration revisions are immutable');
END;

CREATE TABLE provider_pricing_revisions (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    model_identifier TEXT NOT NULL CHECK (
        length(model_identifier) BETWEEN 1 AND 255
    ),
    model_version TEXT NOT NULL CHECK (
        length(model_version) BETWEEN 1 AND 255
    ),
    currency TEXT CHECK (
        currency IS NULL
        OR (length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]')
    ),
    pricing_known INTEGER NOT NULL CHECK (pricing_known IN (0, 1)),
    source_redacted TEXT CHECK (
        source_redacted IS NULL
        OR length(source_redacted) BETWEEN 1 AND 2048
    ),
    effective_at_unix_micros INTEGER NOT NULL CHECK (
        effective_at_unix_micros >= 0
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    CHECK (
        (pricing_known = 0 AND currency IS NULL)
        OR
        (pricing_known = 1 AND currency IS NOT NULL)
    ),
    UNIQUE (
        provider_id, model_identifier, model_version,
        effective_at_unix_micros
    ),
    UNIQUE (id, provider_id, model_identifier, model_version)
) STRICT;

CREATE TABLE provider_price_components (
    pricing_revision_id TEXT NOT NULL REFERENCES provider_pricing_revisions(id),
    usage_kind TEXT NOT NULL CHECK (usage_kind IN (
        'input', 'cached-input', 'cache-write', 'output', 'reasoning',
        'provider-specific'
    )),
    provider_specific_kind TEXT CHECK (
        provider_specific_kind IS NULL
        OR length(provider_specific_kind) BETWEEN 1 AND 255
    ),
    minor_numerator INTEGER NOT NULL CHECK (minor_numerator >= 0),
    token_denominator INTEGER NOT NULL CHECK (token_denominator > 0),
    CHECK (
        (usage_kind = 'provider-specific' AND provider_specific_kind IS NOT NULL)
        OR
        (usage_kind != 'provider-specific' AND provider_specific_kind IS NULL)
    ),
    UNIQUE (pricing_revision_id, usage_kind, provider_specific_kind)
) STRICT;

CREATE UNIQUE INDEX provider_price_components_standard_unique
    ON provider_price_components(pricing_revision_id, usage_kind)
    WHERE provider_specific_kind IS NULL;

CREATE UNIQUE INDEX provider_price_components_specific_unique
    ON provider_price_components(
        pricing_revision_id, usage_kind, provider_specific_kind
    )
    WHERE provider_specific_kind IS NOT NULL;

CREATE TRIGGER provider_price_components_require_known_pricing
BEFORE INSERT ON provider_price_components
WHEN NOT EXISTS (
    SELECT 1
    FROM provider_pricing_revisions
    WHERE id = NEW.pricing_revision_id AND pricing_known = 1
)
BEGIN
    SELECT RAISE(ABORT, 'price components require known pricing');
END;

CREATE TRIGGER provider_pricing_revisions_immutable_update
BEFORE UPDATE ON provider_pricing_revisions
BEGIN
    SELECT RAISE(ABORT, 'provider pricing revisions are immutable');
END;

CREATE TRIGGER provider_pricing_revisions_immutable_delete
BEFORE DELETE ON provider_pricing_revisions
BEGIN
    SELECT RAISE(ABORT, 'provider pricing revisions are immutable');
END;

CREATE TRIGGER provider_price_components_immutable_update
BEFORE UPDATE ON provider_price_components
BEGIN
    SELECT RAISE(ABORT, 'provider price components are immutable');
END;

CREATE TRIGGER provider_price_components_immutable_delete
BEFORE DELETE ON provider_price_components
BEGIN
    SELECT RAISE(ABORT, 'provider price components are immutable');
END;

CREATE TABLE provider_pricing_revision_seals (
    pricing_revision_id TEXT PRIMARY KEY
        REFERENCES provider_pricing_revisions(id),
    component_count INTEGER NOT NULL CHECK (
        component_count BETWEEN 0 AND 128
    ),
    sealed_at_unix_micros INTEGER NOT NULL CHECK (
        sealed_at_unix_micros >= 0
    )
) STRICT;

CREATE TRIGGER provider_pricing_revision_seals_validate
BEFORE INSERT ON provider_pricing_revision_seals
WHEN NOT EXISTS (
    SELECT 1
    FROM provider_pricing_revisions AS pricing
    WHERE pricing.id = NEW.pricing_revision_id
      AND NEW.component_count = (
          SELECT count(*)
          FROM provider_price_components AS component
          WHERE component.pricing_revision_id = pricing.id
      )
      AND (
          (pricing.pricing_known = 0 AND NEW.component_count = 0)
          OR
          (pricing.pricing_known = 1 AND NEW.component_count > 0)
      )
)
BEGIN
    SELECT RAISE(ABORT, 'pricing revision seal requires a complete snapshot');
END;

CREATE TRIGGER provider_pricing_revision_seals_immutable_update
BEFORE UPDATE ON provider_pricing_revision_seals
BEGIN
    SELECT RAISE(ABORT, 'provider pricing revision seals are immutable');
END;

CREATE TRIGGER provider_pricing_revision_seals_immutable_delete
BEFORE DELETE ON provider_pricing_revision_seals
BEGIN
    SELECT RAISE(ABORT, 'provider pricing revision seals are immutable');
END;

CREATE TRIGGER provider_price_components_no_insert_after_seal
BEFORE INSERT ON provider_price_components
WHEN EXISTS (
    SELECT 1 FROM provider_pricing_revision_seals
    WHERE pricing_revision_id = NEW.pricing_revision_id
)
BEGIN
    SELECT RAISE(ABORT, 'sealed provider pricing components are immutable');
END;

CREATE TABLE provider_logical_requests (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    provider_configuration_revision_id TEXT NOT NULL,
    adapter_name TEXT NOT NULL CHECK (length(adapter_name) BETWEEN 1 AND 255),
    adapter_version TEXT NOT NULL CHECK (length(adapter_version) BETWEEN 1 AND 255),
    provider_version TEXT NOT NULL CHECK (
        length(provider_version) BETWEEN 1 AND 255
    ),
    model_identifier TEXT NOT NULL CHECK (
        length(model_identifier) BETWEEN 1 AND 255
    ),
    model_version TEXT NOT NULL CHECK (
        length(model_version) BETWEEN 1 AND 255
    ),
    pricing_revision_id TEXT NOT NULL
        REFERENCES provider_pricing_revision_seals(pricing_revision_id),
    state TEXT NOT NULL CHECK (state IN (
        'planned', 'in-flight', 'succeeded', 'failed', 'cancelled',
        'outcome-unknown', 'retry-exhausted'
    )),
    request_sha256 TEXT NOT NULL CHECK (
        length(request_sha256) = 64
        AND request_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    idempotency_key TEXT NOT NULL CHECK (
        length(idempotency_key) BETWEEN 1 AND 255
    ),
    accounting_status TEXT NOT NULL CHECK (accounting_status IN (
        'unknown', 'estimated', 'provider-reported', 'reconciled',
        'discrepant'
    )),
    started_at_unix_micros INTEGER CHECK (started_at_unix_micros >= 0),
    completed_at_unix_micros INTEGER CHECK (
        completed_at_unix_micros IS NULL
        OR (
            started_at_unix_micros IS NOT NULL
            AND completed_at_unix_micros >= started_at_unix_micros
        )
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    updated_at_unix_micros INTEGER NOT NULL CHECK (
        updated_at_unix_micros >= created_at_unix_micros
    ),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    FOREIGN KEY (
        provider_configuration_revision_id, provider_id,
        adapter_name, adapter_version, provider_version
    ) REFERENCES provider_configuration_revisions(
        id, provider_id, adapter_name, adapter_version, provider_version
    ),
    FOREIGN KEY (
        pricing_revision_id, provider_id, model_identifier, model_version
    ) REFERENCES provider_pricing_revisions(
        id, provider_id, model_identifier, model_version
    ),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE INDEX provider_logical_requests_for_task
    ON provider_logical_requests(task_id, created_at_unix_micros, id);

CREATE TRIGGER provider_logical_requests_identity_immutable
BEFORE UPDATE ON provider_logical_requests
WHEN NEW.id != OLD.id
    OR NEW.task_id != OLD.task_id
    OR NEW.run_id IS NOT OLD.run_id
    OR NEW.provider_id != OLD.provider_id
    OR NEW.provider_configuration_revision_id
        != OLD.provider_configuration_revision_id
    OR NEW.adapter_name != OLD.adapter_name
    OR NEW.adapter_version != OLD.adapter_version
    OR NEW.provider_version != OLD.provider_version
    OR NEW.model_identifier != OLD.model_identifier
    OR NEW.model_version != OLD.model_version
    OR NEW.pricing_revision_id IS NOT OLD.pricing_revision_id
    OR NEW.request_sha256 != OLD.request_sha256
    OR NEW.idempotency_key != OLD.idempotency_key
    OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN
    SELECT RAISE(ABORT, 'provider logical request identity is immutable');
END;

CREATE TRIGGER provider_logical_requests_terminal_immutable
BEFORE UPDATE ON provider_logical_requests
WHEN OLD.state IN (
    'succeeded', 'failed', 'cancelled', 'outcome-unknown', 'retry-exhausted'
)
BEGIN
    SELECT RAISE(ABORT, 'terminal provider logical requests are immutable');
END;

CREATE TRIGGER provider_logical_requests_no_delete
BEFORE DELETE ON provider_logical_requests
BEGIN
    SELECT RAISE(ABORT, 'provider logical requests are immutable');
END;

CREATE TABLE provider_request_attempts (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    logical_request_id TEXT NOT NULL REFERENCES provider_logical_requests(id),
    attempt_number INTEGER NOT NULL CHECK (
        attempt_number BETWEEN 1 AND 32
    ),
    state TEXT NOT NULL CHECK (state IN (
        'prepared', 'started', 'streaming', 'succeeded', 'failed',
        'cancelled', 'outcome-unknown'
    )),
    provider_request_id_redacted TEXT CHECK (
        provider_request_id_redacted IS NULL
        OR length(provider_request_id_redacted) BETWEEN 1 AND 512
    ),
    request_idempotency_key TEXT CHECK (
        request_idempotency_key IS NULL
        OR length(request_idempotency_key) BETWEEN 1 AND 255
    ),
    effect_status TEXT NOT NULL CHECK (effect_status IN (
        'none', 'possible', 'confirmed'
    )),
    partial_stream_observed INTEGER NOT NULL DEFAULT 0 CHECK (
        partial_stream_observed IN (0, 1)
    ),
    error_class TEXT CHECK (error_class IS NULL OR error_class IN (
        'timeout', 'retryable', 'rate-limit', 'authentication',
        'invalid-request', 'safety', 'unavailable', 'cancelled', 'unknown'
    )),
    retryable INTEGER NOT NULL DEFAULT 0 CHECK (retryable IN (0, 1)),
    retry_after_millis INTEGER CHECK (
        retry_after_millis IS NULL OR retry_after_millis >= 0
    ),
    safe_metadata_json TEXT CHECK (
        safe_metadata_json IS NULL
        OR (
            length(safe_metadata_json) BETWEEN 2 AND 65536
            AND json_valid(safe_metadata_json)
        )
    ),
    started_at_unix_micros INTEGER CHECK (started_at_unix_micros >= 0),
    first_response_at_unix_micros INTEGER CHECK (
        first_response_at_unix_micros IS NULL
        OR (
            started_at_unix_micros IS NOT NULL
            AND first_response_at_unix_micros >= started_at_unix_micros
        )
    ),
    completed_at_unix_micros INTEGER CHECK (
        completed_at_unix_micros IS NULL
        OR (
            started_at_unix_micros IS NOT NULL
            AND completed_at_unix_micros >= started_at_unix_micros
        )
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    updated_at_unix_micros INTEGER NOT NULL CHECK (
        updated_at_unix_micros >= created_at_unix_micros
    ),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    UNIQUE (logical_request_id, attempt_number)
) STRICT;

CREATE INDEX provider_request_attempts_for_request
    ON provider_request_attempts(logical_request_id, attempt_number);

CREATE TRIGGER provider_request_attempts_identity_immutable
BEFORE UPDATE ON provider_request_attempts
WHEN NEW.id != OLD.id
    OR NEW.logical_request_id != OLD.logical_request_id
    OR NEW.attempt_number != OLD.attempt_number
    OR NEW.created_at_unix_micros != OLD.created_at_unix_micros
BEGIN
    SELECT RAISE(ABORT, 'provider request attempt identity is immutable');
END;

CREATE TRIGGER provider_request_attempts_terminal_immutable
BEFORE UPDATE ON provider_request_attempts
WHEN OLD.state IN (
    'succeeded', 'failed', 'cancelled', 'outcome-unknown'
)
BEGIN
    SELECT RAISE(ABORT, 'terminal provider request attempts are immutable');
END;

CREATE TRIGGER provider_request_attempts_no_delete
BEFORE DELETE ON provider_request_attempts
BEGIN
    SELECT RAISE(ABORT, 'provider request attempts are immutable');
END;

CREATE TABLE provider_attempt_accounting (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    attempt_id TEXT NOT NULL REFERENCES provider_request_attempts(id),
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 1024),
    source TEXT NOT NULL CHECK (source IN (
        'estimated', 'provider-partial', 'provider-final', 'reconciled'
    )),
    usage_known INTEGER NOT NULL CHECK (usage_known IN (0, 1)),
    input_tokens INTEGER,
    cached_input_tokens INTEGER,
    cache_write_tokens INTEGER,
    output_tokens INTEGER,
    reasoning_tokens INTEGER,
    provider_specific_json TEXT CHECK (
        provider_specific_json IS NULL
        OR (
            length(provider_specific_json) BETWEEN 2 AND 65536
            AND json_valid(provider_specific_json)
        )
    ),
    pricing_revision_id TEXT REFERENCES provider_pricing_revisions(id),
    cost_known INTEGER NOT NULL CHECK (cost_known IN (0, 1)),
    cost_minor_numerator INTEGER,
    cost_minor_denominator INTEGER,
    currency TEXT CHECK (
        currency IS NULL
        OR (length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]')
    ),
    discrepancy INTEGER NOT NULL DEFAULT 0 CHECK (discrepancy IN (0, 1)),
    discrepancy_redacted TEXT CHECK (
        discrepancy_redacted IS NULL
        OR length(discrepancy_redacted) BETWEEN 1 AND 2048
    ),
    partial INTEGER NOT NULL CHECK (partial IN (0, 1)),
    provenance_json TEXT NOT NULL CHECK (
        length(provenance_json) BETWEEN 2 AND 65536
        AND json_valid(provenance_json)
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    CHECK (
        (usage_known = 0
            AND input_tokens IS NULL
            AND cached_input_tokens IS NULL
            AND cache_write_tokens IS NULL
            AND output_tokens IS NULL
            AND reasoning_tokens IS NULL
            AND provider_specific_json IS NULL)
        OR
        (usage_known = 1
            AND input_tokens >= 0
            AND cached_input_tokens >= 0
            AND cache_write_tokens >= 0
            AND output_tokens >= 0
            AND reasoning_tokens >= 0)
    ),
    CHECK (
        (cost_known = 0
            AND cost_minor_numerator IS NULL
            AND cost_minor_denominator IS NULL
            AND currency IS NULL)
        OR
        (cost_known = 1
            AND cost_minor_numerator >= 0
            AND cost_minor_denominator > 0
            AND currency IS NOT NULL
            AND pricing_revision_id IS NOT NULL)
    ),
    CHECK (
        (discrepancy = 0 AND discrepancy_redacted IS NULL)
        OR
        (discrepancy = 1 AND discrepancy_redacted IS NOT NULL)
    ),
    UNIQUE (attempt_id, sequence)
) STRICT;

CREATE INDEX provider_attempt_accounting_for_attempt
    ON provider_attempt_accounting(attempt_id, sequence);

CREATE TRIGGER provider_attempt_accounting_pricing_binding
BEFORE INSERT ON provider_attempt_accounting
WHEN NEW.pricing_revision_id IS NOT NULL
    AND NOT EXISTS (
        SELECT 1
        FROM provider_request_attempts AS attempt
        JOIN provider_logical_requests AS request
          ON request.id = attempt.logical_request_id
        WHERE attempt.id = NEW.attempt_id
          AND request.pricing_revision_id = NEW.pricing_revision_id
    )
BEGIN
    SELECT RAISE(ABORT, 'provider accounting pricing revision is not attributable');
END;

CREATE TRIGGER provider_attempt_accounting_known_cost_pricing
BEFORE INSERT ON provider_attempt_accounting
WHEN NEW.cost_known = 1
    AND NOT EXISTS (
        SELECT 1
        FROM provider_pricing_revisions
        WHERE id = NEW.pricing_revision_id
          AND pricing_known = 1
          AND currency = NEW.currency
    )
BEGIN
    SELECT RAISE(ABORT, 'known provider cost requires matching known pricing');
END;

CREATE TRIGGER provider_attempt_accounting_immutable_update
BEFORE UPDATE ON provider_attempt_accounting
BEGIN
    SELECT RAISE(ABORT, 'provider attempt accounting is immutable');
END;

CREATE TRIGGER provider_attempt_accounting_immutable_delete
BEFORE DELETE ON provider_attempt_accounting
BEGIN
    SELECT RAISE(ABORT, 'provider attempt accounting is immutable');
END;

CREATE TABLE provider_attempt_evidence (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 255),
    attempt_id TEXT NOT NULL REFERENCES provider_request_attempts(id),
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 4096),
    kind TEXT NOT NULL CHECK (kind IN (
        'partial-output', 'partial-tool-call', 'effect-observed',
        'late-response-discarded'
    )),
    final INTEGER NOT NULL CHECK (final IN (0, 1)),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    summary_redacted TEXT NOT NULL CHECK (
        length(summary_redacted) BETWEEN 1 AND 4096
    ),
    byte_count INTEGER NOT NULL CHECK (byte_count >= 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    CHECK (
        kind NOT IN ('partial-output', 'partial-tool-call')
        OR final = 0
    ),
    UNIQUE (attempt_id, sequence)
) STRICT;

CREATE INDEX provider_attempt_evidence_for_attempt
    ON provider_attempt_evidence(attempt_id, sequence);

CREATE TRIGGER provider_attempt_evidence_immutable_update
BEFORE UPDATE ON provider_attempt_evidence
BEGIN
    SELECT RAISE(ABORT, 'provider attempt evidence is immutable');
END;

CREATE TRIGGER provider_attempt_evidence_immutable_delete
BEFORE DELETE ON provider_attempt_evidence
BEGIN
    SELECT RAISE(ABORT, 'provider attempt evidence is immutable');
END;

CREATE TABLE provider_live_smoke_fixtures (
    idempotency_sha256 TEXT PRIMARY KEY CHECK (
        length(idempotency_sha256) = 64
        AND idempotency_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    input_sha256 TEXT NOT NULL CHECK (
        length(input_sha256) = 64
        AND input_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    project_id TEXT NOT NULL REFERENCES projects(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    thread_id TEXT NOT NULL REFERENCES threads(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES runs(id),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    configuration_revision_id TEXT NOT NULL
        REFERENCES provider_configuration_revisions(id),
    pricing_revision_id TEXT NOT NULL REFERENCES provider_pricing_revisions(id),
    logical_request_id TEXT NOT NULL UNIQUE
        REFERENCES provider_logical_requests(id),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    )
) STRICT;

CREATE TRIGGER provider_live_smoke_fixtures_immutable_update
BEFORE UPDATE ON provider_live_smoke_fixtures
BEGIN
    SELECT RAISE(ABORT, 'provider live smoke fixtures are immutable');
END;

CREATE TRIGGER provider_live_smoke_fixtures_immutable_delete
BEFORE DELETE ON provider_live_smoke_fixtures
BEGIN
    SELECT RAISE(ABORT, 'provider live smoke fixtures are immutable');
END;

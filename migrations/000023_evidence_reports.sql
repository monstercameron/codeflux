CREATE TABLE final_evidence_reports (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    requirement_revision INTEGER NOT NULL CHECK (requirement_revision >= 1),
    accepted_plan_revision INTEGER NOT NULL CHECK (accepted_plan_revision >= 1),
    plan_approval_id TEXT NOT NULL REFERENCES approvals(id),
    base_revision TEXT NOT NULL CHECK (
        length(base_revision) IN (40, 64) AND base_revision NOT GLOB '*[^0-9a-f]*'
    ),
    diff_identity TEXT NOT NULL CHECK (
        length(diff_identity) = 64 AND diff_identity NOT GLOB '*[^0-9a-f]*'
    ),
    risk_classification_revision INTEGER NOT NULL CHECK (risk_classification_revision >= 1),
    risk_level TEXT NOT NULL CHECK (risk_level IN ('routine', 'elevated', 'protected')),
    risk_explanation TEXT NOT NULL CHECK (length(risk_explanation) BETWEEN 1 AND 8192),
    graph_revision_id TEXT NOT NULL REFERENCES graph_revisions(id),
    forecast_duration_known INTEGER NOT NULL CHECK (forecast_duration_known IN (0, 1)),
    forecast_p50_nanos INTEGER,
    forecast_p90_nanos INTEGER,
    forecast_duration_unknown_reason TEXT,
    forecast_tokens_known INTEGER NOT NULL CHECK (forecast_tokens_known IN (0, 1)),
    forecast_tokens_p50 INTEGER,
    forecast_tokens_p90 INTEGER,
    forecast_tokens_unknown_reason TEXT,
    forecast_cost_known INTEGER NOT NULL CHECK (forecast_cost_known IN (0, 1)),
    forecast_cost_p50_minor INTEGER,
    forecast_cost_p90_minor INTEGER,
    forecast_currency TEXT,
    forecast_cost_unknown_reason TEXT,
    actual_duration_known INTEGER NOT NULL CHECK (actual_duration_known IN (0, 1)),
    actual_duration_nanos INTEGER,
    actual_duration_unknown_reason TEXT,
    actual_tokens_known INTEGER NOT NULL CHECK (actual_tokens_known IN (0, 1)),
    actual_input_tokens INTEGER,
    actual_cached_input_tokens INTEGER,
    actual_cache_write_tokens INTEGER,
    actual_output_tokens INTEGER,
    actual_reasoning_tokens INTEGER,
    actual_tokens_unknown_reason TEXT,
    actual_cost_known INTEGER NOT NULL CHECK (actual_cost_known IN (0, 1)),
    actual_cost_minor INTEGER,
    actual_currency TEXT,
    actual_cost_unknown_reason TEXT,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (task_id, requirement_revision)
        REFERENCES task_requirement_revisions(task_id, revision),
    FOREIGN KEY (task_id, accepted_plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision),
    FOREIGN KEY (task_id, risk_classification_revision)
        REFERENCES task_risk_classifications(task_id, revision),
    CHECK ((forecast_duration_known = 0 AND forecast_p50_nanos IS NULL AND forecast_p90_nanos IS NULL
            AND forecast_duration_unknown_reason IS NOT NULL AND length(forecast_duration_unknown_reason) BETWEEN 1 AND 8192)
        OR (forecast_duration_known = 1 AND forecast_p50_nanos >= 0 AND forecast_p90_nanos >= forecast_p50_nanos
            AND forecast_duration_unknown_reason IS NULL)),
    CHECK ((forecast_tokens_known = 0 AND forecast_tokens_p50 IS NULL AND forecast_tokens_p90 IS NULL
            AND forecast_tokens_unknown_reason IS NOT NULL AND length(forecast_tokens_unknown_reason) BETWEEN 1 AND 8192)
        OR (forecast_tokens_known = 1 AND forecast_tokens_p50 >= 0 AND forecast_tokens_p90 >= forecast_tokens_p50
            AND forecast_tokens_unknown_reason IS NULL)),
    CHECK ((forecast_cost_known = 0 AND forecast_cost_p50_minor IS NULL AND forecast_cost_p90_minor IS NULL AND forecast_currency IS NULL
            AND forecast_cost_unknown_reason IS NOT NULL AND length(forecast_cost_unknown_reason) BETWEEN 1 AND 8192)
        OR (forecast_cost_known = 1 AND forecast_cost_p50_minor >= 0 AND forecast_cost_p90_minor >= forecast_cost_p50_minor
            AND length(forecast_currency) = 3 AND forecast_currency GLOB '[A-Z][A-Z][A-Z]'
            AND forecast_cost_unknown_reason IS NULL)),
    CHECK ((actual_duration_known = 0 AND actual_duration_nanos IS NULL
            AND actual_duration_unknown_reason IS NOT NULL AND length(actual_duration_unknown_reason) BETWEEN 1 AND 8192)
        OR (actual_duration_known = 1 AND actual_duration_nanos >= 0 AND actual_duration_unknown_reason IS NULL)),
    CHECK ((actual_tokens_known = 0 AND actual_input_tokens IS NULL AND actual_cached_input_tokens IS NULL
            AND actual_cache_write_tokens IS NULL AND actual_output_tokens IS NULL AND actual_reasoning_tokens IS NULL
            AND actual_tokens_unknown_reason IS NOT NULL AND length(actual_tokens_unknown_reason) BETWEEN 1 AND 8192)
        OR (actual_tokens_known = 1 AND actual_input_tokens >= 0 AND actual_cached_input_tokens >= 0
            AND actual_cache_write_tokens >= 0 AND actual_output_tokens >= 0 AND actual_reasoning_tokens >= 0
            AND actual_tokens_unknown_reason IS NULL)),
    CHECK ((actual_cost_known = 0 AND actual_cost_minor IS NULL AND actual_currency IS NULL
            AND actual_cost_unknown_reason IS NOT NULL AND length(actual_cost_unknown_reason) BETWEEN 1 AND 8192)
        OR (actual_cost_known = 1 AND actual_cost_minor >= 0
            AND length(actual_currency) = 3 AND actual_currency GLOB '[A-Z][A-Z][A-Z]'
            AND actual_cost_unknown_reason IS NULL))
) STRICT;

CREATE TRIGGER final_evidence_reports_plan_approval
BEFORE INSERT ON final_evidence_reports
WHEN NOT EXISTS (
    SELECT 1 FROM agent_plan_approval_bindings
    WHERE task_id = NEW.task_id AND plan_revision = NEW.accepted_plan_revision
      AND approval_id = NEW.plan_approval_id
)
BEGIN SELECT RAISE(ABORT, 'final evidence report plan approval is inconsistent'); END;

CREATE TRIGGER final_evidence_reports_graph_scope
BEFORE INSERT ON final_evidence_reports
WHEN NOT EXISTS (
    SELECT 1 FROM graph_task_bindings AS binding
    JOIN graph_revisions AS revision ON revision.graph_id = binding.graph_id
    JOIN graph_revision_seals AS seal ON seal.graph_revision_id = revision.id
    WHERE binding.task_id = NEW.task_id AND revision.id = NEW.graph_revision_id
)
BEGIN SELECT RAISE(ABORT, 'final evidence report graph revision is not a sealed task graph'); END;

CREATE TRIGGER final_evidence_reports_risk_snapshot
BEFORE INSERT ON final_evidence_reports
WHEN NOT EXISTS (
    SELECT 1 FROM task_risk_classifications
    WHERE task_id = NEW.task_id AND revision = NEW.risk_classification_revision
      AND selected_risk = NEW.risk_level AND explanation = NEW.risk_explanation
)
BEGIN SELECT RAISE(ABORT, 'final evidence report risk snapshot is inconsistent'); END;

CREATE TABLE final_evidence_report_token_categories (
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    category TEXT NOT NULL CHECK (length(category) BETWEEN 1 AND 64),
    token_count INTEGER NOT NULL CHECK (token_count >= 0),
    PRIMARY KEY (report_id, category)
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_changed_files (
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 4095),
    repository_relative_path TEXT NOT NULL CHECK (
        length(repository_relative_path) BETWEEN 1 AND 4096
        AND repository_relative_path NOT LIKE '/%'
        AND instr(repository_relative_path, char(0)) = 0
        AND instr(repository_relative_path, char(92)) = 0
        AND repository_relative_path != '..'
        AND repository_relative_path NOT LIKE '../%'
        AND repository_relative_path NOT LIKE '%/../%'
        AND repository_relative_path NOT LIKE '%/..'
    ),
    prior_repository_relative_path TEXT,
    status TEXT NOT NULL CHECK (status IN ('added', 'modified', 'deleted', 'renamed')),
    insertions INTEGER NOT NULL CHECK (insertions >= 0),
    deletions INTEGER NOT NULL CHECK (deletions >= 0),
    generated INTEGER NOT NULL CHECK (generated IN (0, 1)),
    PRIMARY KEY (report_id, ordinal),
    UNIQUE (report_id, repository_relative_path),
    CHECK ((status = 'renamed' AND prior_repository_relative_path IS NOT NULL)
        OR (status != 'renamed' AND prior_repository_relative_path IS NULL)),
    CHECK (prior_repository_relative_path IS NULL OR (
        length(prior_repository_relative_path) BETWEEN 1 AND 4096
        AND prior_repository_relative_path NOT LIKE '/%'
        AND instr(prior_repository_relative_path, char(0)) = 0
        AND instr(prior_repository_relative_path, char(92)) = 0
        AND prior_repository_relative_path != '..'
        AND prior_repository_relative_path NOT LIKE '../%'
        AND prior_repository_relative_path NOT LIKE '%/../%'
        AND prior_repository_relative_path NOT LIKE '%/..'
        AND prior_repository_relative_path != repository_relative_path
    ))
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_validations (
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 1023),
    check_id TEXT NOT NULL CHECK (length(check_id) BETWEEN 1 AND 255),
    validation_run_id TEXT,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN (
        'passed', 'failed', 'waived', 'skipped', 'unavailable', 'cancelled', 'invalidated'
    )),
    summary_redacted TEXT NOT NULL CHECK (length(summary_redacted) BETWEEN 1 AND 8192),
    status_reason_redacted TEXT,
    command_digest TEXT CHECK (
        command_digest IS NULL OR (length(command_digest) = 64 AND command_digest NOT GLOB '*[^0-9a-f]*')
    ),
    diff_identity TEXT NOT NULL CHECK (length(diff_identity) = 64 AND diff_identity NOT GLOB '*[^0-9a-f]*'),
    PRIMARY KEY (report_id, ordinal),
    UNIQUE (report_id, check_id),
    UNIQUE (report_id, validation_run_id),
    CHECK (status = 'passed' OR status_reason_redacted IS NOT NULL)
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_approvals (
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
    approval_id TEXT NOT NULL REFERENCES approvals(id),
    state TEXT NOT NULL CHECK (state IN ('pending', 'granted', 'denied', 'expired', 'cancelled')),
    scope TEXT NOT NULL CHECK (length(scope) BETWEEN 1 AND 8192),
    authority_used TEXT,
    PRIMARY KEY (report_id, ordinal),
    UNIQUE (report_id, approval_id),
    CHECK (authority_used IS NULL OR state = 'granted')
) STRICT, WITHOUT ROWID;

CREATE TRIGGER final_evidence_report_approvals_snapshot
BEFORE INSERT ON final_evidence_report_approvals
WHEN NOT EXISTS (
    SELECT 1 FROM approvals AS approval
    JOIN final_evidence_reports AS report ON report.id = NEW.report_id
    WHERE approval.id = NEW.approval_id AND approval.task_id = report.task_id
      AND approval.state = NEW.state AND approval.scope = NEW.scope
)
BEGIN SELECT RAISE(ABORT, 'final evidence report approval snapshot is inconsistent'); END;

CREATE TABLE final_evidence_report_versions (
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 127),
    version_kind TEXT NOT NULL CHECK (version_kind IN ('model', 'provider', 'tool', 'policy')),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 8192),
    known INTEGER NOT NULL CHECK (known IN (0, 1)),
    version TEXT,
    unknown_reason TEXT,
    PRIMARY KEY (report_id, ordinal),
    UNIQUE (report_id, version_kind, name),
    CHECK ((known = 1 AND version IS NOT NULL AND unknown_reason IS NULL)
        OR (known = 0 AND version IS NULL AND unknown_reason IS NOT NULL))
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_narratives (
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    narrative_kind TEXT NOT NULL CHECK (narrative_kind IN ('assumption', 'limitation')),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
    statement_redacted TEXT NOT NULL CHECK (length(statement_redacted) BETWEEN 1 AND 8192),
    PRIMARY KEY (report_id, narrative_kind, ordinal),
    UNIQUE (report_id, narrative_kind, statement_redacted)
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_claims (
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 1023),
    claim_id TEXT NOT NULL CHECK (length(claim_id) BETWEEN 1 AND 255),
    statement_redacted TEXT NOT NULL CHECK (length(statement_redacted) BETWEEN 1 AND 8192),
    scope_redacted TEXT NOT NULL CHECK (length(scope_redacted) BETWEEN 1 AND 8192),
    boundary TEXT NOT NULL CHECK (boundary IN ('internal', 'external-system')),
    guarantee_level TEXT NOT NULL CHECK (guarantee_level IN (
        'fully-evaluated', 'model-verified', 'contract-checked', 'runtime-only', 'invalidated'
    )),
    guarantee_reason_redacted TEXT NOT NULL CHECK (length(guarantee_reason_redacted) BETWEEN 1 AND 8192),
    PRIMARY KEY (report_id, ordinal),
    UNIQUE (report_id, claim_id),
    CHECK (boundary = 'internal' OR guarantee_level IN ('contract-checked', 'runtime-only', 'invalidated'))
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_claim_evidence (
    report_id TEXT NOT NULL,
    claim_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 127),
    evidence_id TEXT NOT NULL REFERENCES evidence(id),
    PRIMARY KEY (report_id, claim_id, ordinal),
    UNIQUE (report_id, claim_id, evidence_id),
    FOREIGN KEY (report_id, claim_id)
        REFERENCES final_evidence_report_claims(report_id, claim_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_claim_validations (
    report_id TEXT NOT NULL,
    claim_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 127),
    validation_run_id TEXT NOT NULL,
    PRIMARY KEY (report_id, claim_id, ordinal),
    UNIQUE (report_id, claim_id, validation_run_id),
    FOREIGN KEY (report_id, claim_id)
        REFERENCES final_evidence_report_claims(report_id, claim_id),
    FOREIGN KEY (report_id, validation_run_id)
        REFERENCES final_evidence_report_validations(report_id, validation_run_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_claim_graph_nodes (
    report_id TEXT NOT NULL,
    claim_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 127),
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    PRIMARY KEY (report_id, claim_id, ordinal),
    UNIQUE (report_id, claim_id, node_id),
    FOREIGN KEY (report_id, claim_id)
        REFERENCES final_evidence_report_claims(report_id, claim_id),
    FOREIGN KEY (graph_revision_id, node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE final_evidence_report_claim_narratives (
    report_id TEXT NOT NULL,
    claim_id TEXT NOT NULL,
    narrative_kind TEXT NOT NULL CHECK (narrative_kind IN ('assumption', 'limitation')),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
    statement_redacted TEXT NOT NULL CHECK (length(statement_redacted) BETWEEN 1 AND 8192),
    PRIMARY KEY (report_id, claim_id, narrative_kind, ordinal),
    UNIQUE (report_id, claim_id, narrative_kind, statement_redacted),
    FOREIGN KEY (report_id, claim_id)
        REFERENCES final_evidence_report_claims(report_id, claim_id)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER final_evidence_report_claim_evidence_task_scope
BEFORE INSERT ON final_evidence_report_claim_evidence
WHEN NOT EXISTS (
    SELECT 1 FROM final_evidence_reports AS report
    JOIN evidence AS item ON item.id = NEW.evidence_id AND item.task_id = report.task_id
    WHERE report.id = NEW.report_id
)
BEGIN SELECT RAISE(ABORT, 'claim evidence belongs to another task'); END;

CREATE TRIGGER final_evidence_report_claim_evidence_assurance
BEFORE INSERT ON final_evidence_report_claim_evidence
WHEN EXISTS (
    SELECT 1 FROM final_evidence_report_claims AS claim
    JOIN evidence AS item ON item.id = NEW.evidence_id
    WHERE claim.report_id = NEW.report_id AND claim.claim_id = NEW.claim_id
      AND CASE item.assurance_level
            WHEN 'fully-evaluated' THEN 4 WHEN 'model-verified' THEN 3
            WHEN 'contract-checked' THEN 2 WHEN 'runtime-only' THEN 1 ELSE 0
          END < CASE claim.guarantee_level
            WHEN 'fully-evaluated' THEN 4 WHEN 'model-verified' THEN 3
            WHEN 'contract-checked' THEN 2 WHEN 'runtime-only' THEN 1 ELSE 0
          END
)
BEGIN SELECT RAISE(ABORT, 'claim guarantee exceeds linked evidence assurance'); END;

CREATE TRIGGER final_evidence_report_claim_graph_scope
BEFORE INSERT ON final_evidence_report_claim_graph_nodes
WHEN NOT EXISTS (
    SELECT 1 FROM final_evidence_reports
    WHERE id = NEW.report_id AND graph_revision_id = NEW.graph_revision_id
)
BEGIN SELECT RAISE(ABORT, 'claim graph node belongs to another report revision'); END;

CREATE TABLE final_evidence_report_seals (
    report_id TEXT PRIMARY KEY REFERENCES final_evidence_reports(id),
    sealed_at_unix_micros INTEGER NOT NULL CHECK (sealed_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER final_evidence_report_seals_immutable_update BEFORE UPDATE ON final_evidence_report_seals
BEGIN SELECT RAISE(ABORT, 'final evidence report seals are immutable'); END;
CREATE TRIGGER final_evidence_report_seals_immutable_delete BEFORE DELETE ON final_evidence_report_seals
BEGIN SELECT RAISE(ABORT, 'final evidence report seals are immutable'); END;

CREATE TRIGGER final_evidence_report_token_categories_after_seal BEFORE INSERT ON final_evidence_report_token_categories
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept token categories'); END;
CREATE TRIGGER final_evidence_report_changed_files_after_seal BEFORE INSERT ON final_evidence_report_changed_files
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept changed files'); END;
CREATE TRIGGER final_evidence_report_validations_after_seal BEFORE INSERT ON final_evidence_report_validations
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept validations'); END;
CREATE TRIGGER final_evidence_report_approvals_after_seal BEFORE INSERT ON final_evidence_report_approvals
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept approvals'); END;
CREATE TRIGGER final_evidence_report_versions_after_seal BEFORE INSERT ON final_evidence_report_versions
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept versions'); END;
CREATE TRIGGER final_evidence_report_narratives_after_seal BEFORE INSERT ON final_evidence_report_narratives
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept narratives'); END;
CREATE TRIGGER final_evidence_report_claims_after_seal BEFORE INSERT ON final_evidence_report_claims
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept claims'); END;
CREATE TRIGGER final_evidence_report_claim_evidence_after_seal BEFORE INSERT ON final_evidence_report_claim_evidence
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept claim evidence links'); END;
CREATE TRIGGER final_evidence_report_claim_validations_after_seal BEFORE INSERT ON final_evidence_report_claim_validations
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept claim validation links'); END;
CREATE TRIGGER final_evidence_report_claim_graph_nodes_after_seal BEFORE INSERT ON final_evidence_report_claim_graph_nodes
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept claim graph links'); END;
CREATE TRIGGER final_evidence_report_claim_narratives_after_seal BEFORE INSERT ON final_evidence_report_claim_narratives
WHEN EXISTS (SELECT 1 FROM final_evidence_report_seals WHERE report_id = NEW.report_id)
BEGIN SELECT RAISE(ABORT, 'sealed final evidence report cannot accept claim narratives'); END;

CREATE INDEX final_evidence_reports_by_task
    ON final_evidence_reports(task_id, created_at_unix_micros DESC);

CREATE TRIGGER final_evidence_reports_immutable_update BEFORE UPDATE ON final_evidence_reports
BEGIN SELECT RAISE(ABORT, 'final evidence reports are immutable'); END;
CREATE TRIGGER final_evidence_reports_immutable_delete BEFORE DELETE ON final_evidence_reports
BEGIN SELECT RAISE(ABORT, 'final evidence reports are immutable'); END;
CREATE TRIGGER final_evidence_report_changed_files_immutable_update BEFORE UPDATE ON final_evidence_report_changed_files
BEGIN SELECT RAISE(ABORT, 'final evidence report changed files are immutable'); END;
CREATE TRIGGER final_evidence_report_changed_files_immutable_delete BEFORE DELETE ON final_evidence_report_changed_files
BEGIN SELECT RAISE(ABORT, 'final evidence report changed files are immutable'); END;
CREATE TRIGGER final_evidence_report_validations_immutable_update BEFORE UPDATE ON final_evidence_report_validations
BEGIN SELECT RAISE(ABORT, 'final evidence report validations are immutable'); END;
CREATE TRIGGER final_evidence_report_validations_immutable_delete BEFORE DELETE ON final_evidence_report_validations
BEGIN SELECT RAISE(ABORT, 'final evidence report validations are immutable'); END;
CREATE TRIGGER final_evidence_report_claims_immutable_update BEFORE UPDATE ON final_evidence_report_claims
BEGIN SELECT RAISE(ABORT, 'final evidence report claims are immutable'); END;
CREATE TRIGGER final_evidence_report_claims_immutable_delete BEFORE DELETE ON final_evidence_report_claims
BEGIN SELECT RAISE(ABORT, 'final evidence report claims are immutable'); END;
CREATE TRIGGER final_evidence_report_token_categories_immutable_update BEFORE UPDATE ON final_evidence_report_token_categories
BEGIN SELECT RAISE(ABORT, 'final evidence report token categories are immutable'); END;
CREATE TRIGGER final_evidence_report_token_categories_immutable_delete BEFORE DELETE ON final_evidence_report_token_categories
BEGIN SELECT RAISE(ABORT, 'final evidence report token categories are immutable'); END;
CREATE TRIGGER final_evidence_report_approvals_immutable_update BEFORE UPDATE ON final_evidence_report_approvals
BEGIN SELECT RAISE(ABORT, 'final evidence report approvals are immutable'); END;
CREATE TRIGGER final_evidence_report_approvals_immutable_delete BEFORE DELETE ON final_evidence_report_approvals
BEGIN SELECT RAISE(ABORT, 'final evidence report approvals are immutable'); END;
CREATE TRIGGER final_evidence_report_versions_immutable_update BEFORE UPDATE ON final_evidence_report_versions
BEGIN SELECT RAISE(ABORT, 'final evidence report versions are immutable'); END;
CREATE TRIGGER final_evidence_report_versions_immutable_delete BEFORE DELETE ON final_evidence_report_versions
BEGIN SELECT RAISE(ABORT, 'final evidence report versions are immutable'); END;
CREATE TRIGGER final_evidence_report_narratives_immutable_update BEFORE UPDATE ON final_evidence_report_narratives
BEGIN SELECT RAISE(ABORT, 'final evidence report narratives are immutable'); END;
CREATE TRIGGER final_evidence_report_narratives_immutable_delete BEFORE DELETE ON final_evidence_report_narratives
BEGIN SELECT RAISE(ABORT, 'final evidence report narratives are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_evidence_immutable_update BEFORE UPDATE ON final_evidence_report_claim_evidence
BEGIN SELECT RAISE(ABORT, 'final evidence report claim evidence links are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_evidence_immutable_delete BEFORE DELETE ON final_evidence_report_claim_evidence
BEGIN SELECT RAISE(ABORT, 'final evidence report claim evidence links are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_validations_immutable_update BEFORE UPDATE ON final_evidence_report_claim_validations
BEGIN SELECT RAISE(ABORT, 'final evidence report claim validation links are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_validations_immutable_delete BEFORE DELETE ON final_evidence_report_claim_validations
BEGIN SELECT RAISE(ABORT, 'final evidence report claim validation links are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_graph_nodes_immutable_update BEFORE UPDATE ON final_evidence_report_claim_graph_nodes
BEGIN SELECT RAISE(ABORT, 'final evidence report claim graph links are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_graph_nodes_immutable_delete BEFORE DELETE ON final_evidence_report_claim_graph_nodes
BEGIN SELECT RAISE(ABORT, 'final evidence report claim graph links are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_narratives_immutable_update BEFORE UPDATE ON final_evidence_report_claim_narratives
BEGIN SELECT RAISE(ABORT, 'final evidence report claim narratives are immutable'); END;
CREATE TRIGGER final_evidence_report_claim_narratives_immutable_delete BEFORE DELETE ON final_evidence_report_claim_narratives
BEGIN SELECT RAISE(ABORT, 'final evidence report claim narratives are immutable'); END;

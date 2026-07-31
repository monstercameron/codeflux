CREATE TABLE acceptance_reviews (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    diff_identity TEXT NOT NULL CHECK (length(diff_identity) = 64 AND diff_identity NOT GLOB '*[^0-9a-f]*'),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    opened_by TEXT NOT NULL CHECK (length(opened_by) BETWEEN 1 AND 255),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    opened_at_unix_micros INTEGER NOT NULL CHECK (opened_at_unix_micros >= 0),
    UNIQUE (task_id, revision),
    UNIQUE (task_id, idempotency_key),
    FOREIGN KEY (task_id, plan_revision) REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER acceptance_reviews_report_binding
BEFORE INSERT ON acceptance_reviews
WHEN NOT EXISTS (
    SELECT 1 FROM final_evidence_reports AS report
    JOIN final_evidence_report_seals AS seal ON seal.report_id = report.id
    WHERE report.id = NEW.report_id AND report.task_id = NEW.task_id
      AND report.diff_identity = NEW.diff_identity
      AND report.accepted_plan_revision = NEW.plan_revision
)
BEGIN SELECT RAISE(ABORT, 'acceptance review report binding is inconsistent'); END;

CREATE TABLE acceptance_review_heads (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id),
    review_id TEXT NOT NULL UNIQUE REFERENCES acceptance_reviews(id),
    review_revision INTEGER NOT NULL CHECK (review_revision >= 1),
    updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER acceptance_review_heads_insert_binding
BEFORE INSERT ON acceptance_review_heads
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_reviews
    WHERE id = NEW.review_id AND task_id = NEW.task_id AND revision = NEW.review_revision
)
BEGIN SELECT RAISE(ABORT, 'acceptance review head binding is inconsistent'); END;

CREATE TABLE acceptance_review_resolutions (
    review_id TEXT PRIMARY KEY REFERENCES acceptance_reviews(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    resolution_kind TEXT NOT NULL CHECK (resolution_kind IN ('accepted', 'repair-requested', 'rejected')),
    resolution_id TEXT NOT NULL UNIQUE CHECK (length(resolution_id) = 64 AND resolution_id NOT GLOB '*[^0-9a-f]*'),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER acceptance_review_resolutions_scope
BEFORE INSERT ON acceptance_review_resolutions
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_reviews WHERE id = NEW.review_id AND task_id = NEW.task_id
)
BEGIN SELECT RAISE(ABORT, 'acceptance review resolution scope is inconsistent'); END;

CREATE TRIGGER acceptance_review_heads_update_binding
BEFORE UPDATE ON acceptance_review_heads
WHEN NEW.task_id != OLD.task_id OR NEW.review_revision != OLD.review_revision + 1
  OR NOT EXISTS (
      SELECT 1 FROM acceptance_reviews
      WHERE id = NEW.review_id AND task_id = NEW.task_id AND revision = NEW.review_revision
  )
  OR EXISTS (
      SELECT 1 FROM acceptance_review_resolutions
      WHERE review_id = OLD.review_id AND resolution_kind IN ('accepted', 'rejected')
  )
BEGIN SELECT RAISE(ABORT, 'acceptance review renewal is inconsistent'); END;

CREATE TABLE acceptance_decisions (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    review_id TEXT NOT NULL REFERENCES acceptance_reviews(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    review_revision INTEGER NOT NULL CHECK (review_revision >= 1),
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    diff_identity TEXT NOT NULL CHECK (length(diff_identity) = 64 AND diff_identity NOT GLOB '*[^0-9a-f]*'),
    actor_reference TEXT NOT NULL CHECK (length(actor_reference) BETWEEN 1 AND 255),
    authority_reference TEXT NOT NULL CHECK (length(authority_reference) BETWEEN 1 AND 512),
    reason_redacted TEXT NOT NULL CHECK (length(reason_redacted) BETWEEN 1 AND 8192),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (task_id, idempotency_key),
    UNIQUE (review_id)
) STRICT;

CREATE TRIGGER acceptance_decisions_review_binding
BEFORE INSERT ON acceptance_decisions
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_reviews AS review
    JOIN acceptance_review_heads AS head ON head.review_id = review.id
    WHERE review.id = NEW.review_id AND review.task_id = NEW.task_id
      AND review.revision = NEW.review_revision AND review.report_id = NEW.report_id
      AND review.diff_identity = NEW.diff_identity
      AND NOT EXISTS (SELECT 1 FROM acceptance_review_resolutions WHERE review_id = review.id)
)
BEGIN SELECT RAISE(ABORT, 'acceptance decision review binding is stale'); END;

CREATE TABLE acceptance_decision_acknowledgements (
    acceptance_id TEXT NOT NULL REFERENCES acceptance_decisions(id),
    report_id TEXT NOT NULL,
    check_id TEXT NOT NULL,
    acknowledged_at_unix_micros INTEGER NOT NULL CHECK (acknowledged_at_unix_micros >= 0),
    PRIMARY KEY (acceptance_id, check_id),
    FOREIGN KEY (report_id, check_id)
        REFERENCES final_evidence_report_validations(report_id, check_id)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER acceptance_acknowledgements_exact_scope
BEFORE INSERT ON acceptance_decision_acknowledgements
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_decisions AS decision
    JOIN final_evidence_report_validations AS validation
      ON validation.report_id = decision.report_id AND validation.check_id = NEW.check_id
    WHERE decision.id = NEW.acceptance_id AND decision.report_id = NEW.report_id
      AND validation.required = 1 AND validation.status IN ('failed', 'waived')
)
BEGIN SELECT RAISE(ABORT, 'acceptance acknowledgement is not required by this report'); END;

CREATE TABLE acceptance_decision_seals (
    acceptance_id TEXT PRIMARY KEY REFERENCES acceptance_decisions(id),
    sealed_at_unix_micros INTEGER NOT NULL CHECK (sealed_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER acceptance_decision_seals_gate
BEFORE INSERT ON acceptance_decision_seals
WHEN EXISTS (
    SELECT 1 FROM acceptance_decisions AS decision
    JOIN final_evidence_report_validations AS validation ON validation.report_id = decision.report_id
    WHERE decision.id = NEW.acceptance_id AND validation.required = 1
      AND (
        validation.status IN ('skipped', 'unavailable', 'cancelled', 'invalidated')
        OR (validation.status IN ('failed', 'waived') AND NOT EXISTS (
            SELECT 1 FROM acceptance_decision_acknowledgements AS acknowledgement
            WHERE acknowledgement.acceptance_id = decision.id
              AND acknowledgement.report_id = decision.report_id
              AND acknowledgement.check_id = validation.check_id
        ))
      )
)
BEGIN SELECT RAISE(ABORT, 'required validation gate blocks acceptance'); END;

CREATE TABLE acceptance_repair_requests (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    review_id TEXT NOT NULL REFERENCES acceptance_reviews(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    review_revision INTEGER NOT NULL CHECK (review_revision >= 1),
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    diff_identity TEXT NOT NULL CHECK (length(diff_identity) = 64 AND diff_identity NOT GLOB '*[^0-9a-f]*'),
    previous_plan_revision INTEGER NOT NULL CHECK (previous_plan_revision >= 1),
    new_plan_revision INTEGER NOT NULL CHECK (new_plan_revision > previous_plan_revision),
    pre_repair_checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    feedback_redacted TEXT NOT NULL CHECK (length(feedback_redacted) BETWEEN 1 AND 8192),
    requested_by TEXT NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 255),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (task_id, idempotency_key),
    UNIQUE (review_id),
    FOREIGN KEY (task_id, previous_plan_revision) REFERENCES agent_plan_revisions(task_id, revision),
    FOREIGN KEY (task_id, new_plan_revision) REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TRIGGER acceptance_repair_requests_lineage
BEFORE INSERT ON acceptance_repair_requests
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_reviews AS review
    JOIN acceptance_review_heads AS head ON head.review_id = review.id
    JOIN checkpoints AS checkpoint ON checkpoint.id = NEW.pre_repair_checkpoint_id
    WHERE review.id = NEW.review_id AND review.task_id = NEW.task_id
      AND review.revision = NEW.review_revision AND review.report_id = NEW.report_id
      AND review.diff_identity = NEW.diff_identity
      AND review.plan_revision = NEW.previous_plan_revision
      AND checkpoint.task_id = NEW.task_id AND checkpoint.state = 'ready'
      AND NOT EXISTS (SELECT 1 FROM acceptance_review_resolutions WHERE review_id = review.id)
)
BEGIN SELECT RAISE(ABORT, 'repair request review or checkpoint lineage is inconsistent'); END;

CREATE TABLE acceptance_repair_targets (
    repair_request_id TEXT NOT NULL REFERENCES acceptance_repair_requests(id),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
    target_kind TEXT NOT NULL CHECK (target_kind IN ('validation', 'hunk')),
    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 255),
    PRIMARY KEY (repair_request_id, ordinal),
    UNIQUE (repair_request_id, target_kind, target_id)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER acceptance_repair_validation_target_scope
BEFORE INSERT ON acceptance_repair_targets
WHEN NEW.target_kind = 'validation' AND NOT EXISTS (
    SELECT 1 FROM acceptance_repair_requests AS repair
    JOIN final_evidence_report_validations AS validation
      ON validation.report_id = repair.report_id AND validation.check_id = NEW.target_id
    WHERE repair.id = NEW.repair_request_id AND validation.status != 'passed'
)
BEGIN SELECT RAISE(ABORT, 'repair validation target is not a selected non-passed check'); END;

CREATE TABLE acceptance_repair_request_seals (
    repair_request_id TEXT PRIMARY KEY REFERENCES acceptance_repair_requests(id),
    sealed_at_unix_micros INTEGER NOT NULL CHECK (sealed_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER acceptance_repair_request_seals_targets
BEFORE INSERT ON acceptance_repair_request_seals
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_repair_targets WHERE repair_request_id = NEW.repair_request_id
)
BEGIN SELECT RAISE(ABORT, 'repair request requires at least one selected target'); END;

CREATE TABLE acceptance_rejections (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    review_id TEXT NOT NULL UNIQUE REFERENCES acceptance_reviews(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    review_revision INTEGER NOT NULL CHECK (review_revision >= 1),
    report_id TEXT NOT NULL REFERENCES final_evidence_reports(id),
    diff_identity TEXT NOT NULL CHECK (length(diff_identity) = 64 AND diff_identity NOT GLOB '*[^0-9a-f]*'),
    preserve_patch INTEGER NOT NULL CHECK (preserve_patch = 1),
    reason_redacted TEXT NOT NULL CHECK (length(reason_redacted) BETWEEN 1 AND 8192),
    rejected_by TEXT NOT NULL CHECK (length(rejected_by) BETWEEN 1 AND 255),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TRIGGER acceptance_rejections_review_binding
BEFORE INSERT ON acceptance_rejections
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_reviews AS review
    JOIN acceptance_review_heads AS head ON head.review_id = review.id
    WHERE review.id = NEW.review_id AND review.task_id = NEW.task_id
      AND review.revision = NEW.review_revision AND review.report_id = NEW.report_id
      AND review.diff_identity = NEW.diff_identity
      AND NOT EXISTS (SELECT 1 FROM acceptance_review_resolutions WHERE review_id = review.id)
)
BEGIN SELECT RAISE(ABORT, 'rejection review binding is stale'); END;

CREATE TABLE acceptance_repair_rollback_intents (
    id TEXT PRIMARY KEY CHECK (length(id) = 64 AND id NOT GLOB '*[^0-9a-f]*'),
    repair_request_id TEXT NOT NULL UNIQUE REFERENCES acceptance_repair_requests(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    target_checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
    expected_repair_diff_identity TEXT NOT NULL CHECK (length(expected_repair_diff_identity) = 64 AND expected_repair_diff_identity NOT GLOB '*[^0-9a-f]*'),
    reason TEXT NOT NULL CHECK (reason IN ('user-requested', 'regression', 'validation-invalidated', 'recovery')),
    requested_by TEXT NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 255),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    UNIQUE (task_id, idempotency_key)
) STRICT;

CREATE TRIGGER acceptance_repair_rollback_intents_lineage
BEFORE INSERT ON acceptance_repair_rollback_intents
WHEN NOT EXISTS (
    SELECT 1 FROM acceptance_repair_requests AS repair
    JOIN acceptance_repair_request_seals AS seal ON seal.repair_request_id = repair.id
    WHERE repair.id = NEW.repair_request_id AND repair.task_id = NEW.task_id
      AND repair.pre_repair_checkpoint_id = NEW.target_checkpoint_id
)
BEGIN SELECT RAISE(ABORT, 'repair rollback target is not the pre-repair checkpoint'); END;

CREATE TABLE acceptance_repair_rollback_outcomes (
    rollback_intent_id TEXT PRIMARY KEY REFERENCES acceptance_repair_rollback_intents(id),
    outcome TEXT NOT NULL CHECK (outcome IN ('restored', 'failed')),
    observed_diff_identity TEXT NOT NULL CHECK (length(observed_diff_identity) = 64 AND observed_diff_identity NOT GLOB '*[^0-9a-f]*'),
    detail_redacted TEXT NOT NULL CHECK (length(detail_redacted) BETWEEN 1 AND 8192),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER acceptance_repair_rollback_outcomes_exact_restore
BEFORE INSERT ON acceptance_repair_rollback_outcomes
WHEN NEW.outcome = 'restored' AND NOT EXISTS (
    SELECT 1 FROM acceptance_repair_rollback_intents AS intent
    JOIN checkpoints AS checkpoint ON checkpoint.id = intent.target_checkpoint_id
    WHERE intent.id = NEW.rollback_intent_id AND checkpoint.worktree_diff_hash = NEW.observed_diff_identity
)
BEGIN SELECT RAISE(ABORT, 'restored rollback diff does not match the pre-repair checkpoint'); END;

CREATE TRIGGER acceptance_review_resolutions_exact_kind
BEFORE INSERT ON acceptance_review_resolutions
WHEN (NEW.resolution_kind = 'accepted' AND NOT EXISTS (
        SELECT 1 FROM acceptance_decisions AS decision
        JOIN acceptance_decision_seals AS seal ON seal.acceptance_id = decision.id
        WHERE decision.id = NEW.resolution_id AND decision.review_id = NEW.review_id AND decision.task_id = NEW.task_id
    ))
 OR (NEW.resolution_kind = 'repair-requested' AND NOT EXISTS (
        SELECT 1 FROM acceptance_repair_requests AS repair
        JOIN acceptance_repair_request_seals AS seal ON seal.repair_request_id = repair.id
        WHERE repair.id = NEW.resolution_id AND repair.review_id = NEW.review_id AND repair.task_id = NEW.task_id
    ))
 OR (NEW.resolution_kind = 'rejected' AND NOT EXISTS (
        SELECT 1 FROM acceptance_rejections AS rejection
        WHERE rejection.id = NEW.resolution_id AND rejection.review_id = NEW.review_id AND rejection.task_id = NEW.task_id
    ))
BEGIN SELECT RAISE(ABORT, 'acceptance review resolution does not identify a sealed exact decision'); END;

CREATE TRIGGER acceptance_acknowledgements_after_seal
BEFORE INSERT ON acceptance_decision_acknowledgements
WHEN EXISTS (SELECT 1 FROM acceptance_decision_seals WHERE acceptance_id = NEW.acceptance_id)
BEGIN SELECT RAISE(ABORT, 'sealed acceptance cannot accept acknowledgements'); END;

CREATE TRIGGER acceptance_repair_targets_after_seal
BEFORE INSERT ON acceptance_repair_targets
WHEN EXISTS (SELECT 1 FROM acceptance_repair_request_seals WHERE repair_request_id = NEW.repair_request_id)
BEGIN SELECT RAISE(ABORT, 'sealed repair request cannot accept targets'); END;

CREATE INDEX acceptance_reviews_by_task ON acceptance_reviews(task_id, revision DESC);
CREATE INDEX acceptance_repairs_by_task ON acceptance_repair_requests(task_id, created_at_unix_micros DESC);

CREATE TRIGGER acceptance_reviews_immutable_update BEFORE UPDATE ON acceptance_reviews
BEGIN SELECT RAISE(ABORT, 'acceptance reviews are immutable'); END;
CREATE TRIGGER acceptance_reviews_immutable_delete BEFORE DELETE ON acceptance_reviews
BEGIN SELECT RAISE(ABORT, 'acceptance reviews are immutable'); END;
CREATE TRIGGER acceptance_review_heads_immutable_delete BEFORE DELETE ON acceptance_review_heads
BEGIN SELECT RAISE(ABORT, 'acceptance review heads cannot be deleted'); END;
CREATE TRIGGER acceptance_review_resolutions_immutable_update BEFORE UPDATE ON acceptance_review_resolutions
BEGIN SELECT RAISE(ABORT, 'acceptance review resolutions are immutable'); END;
CREATE TRIGGER acceptance_review_resolutions_immutable_delete BEFORE DELETE ON acceptance_review_resolutions
BEGIN SELECT RAISE(ABORT, 'acceptance review resolutions are immutable'); END;
CREATE TRIGGER acceptance_decisions_immutable_update BEFORE UPDATE ON acceptance_decisions
BEGIN SELECT RAISE(ABORT, 'acceptance decisions are immutable'); END;
CREATE TRIGGER acceptance_decisions_immutable_delete BEFORE DELETE ON acceptance_decisions
BEGIN SELECT RAISE(ABORT, 'acceptance decisions are immutable'); END;
CREATE TRIGGER acceptance_decision_acknowledgements_immutable_update BEFORE UPDATE ON acceptance_decision_acknowledgements
BEGIN SELECT RAISE(ABORT, 'acceptance acknowledgements are immutable'); END;
CREATE TRIGGER acceptance_decision_acknowledgements_immutable_delete BEFORE DELETE ON acceptance_decision_acknowledgements
BEGIN SELECT RAISE(ABORT, 'acceptance acknowledgements are immutable'); END;
CREATE TRIGGER acceptance_decision_seals_immutable_update BEFORE UPDATE ON acceptance_decision_seals
BEGIN SELECT RAISE(ABORT, 'acceptance decision seals are immutable'); END;
CREATE TRIGGER acceptance_decision_seals_immutable_delete BEFORE DELETE ON acceptance_decision_seals
BEGIN SELECT RAISE(ABORT, 'acceptance decision seals are immutable'); END;
CREATE TRIGGER acceptance_repair_requests_immutable_update BEFORE UPDATE ON acceptance_repair_requests
BEGIN SELECT RAISE(ABORT, 'acceptance repair requests are immutable'); END;
CREATE TRIGGER acceptance_repair_requests_immutable_delete BEFORE DELETE ON acceptance_repair_requests
BEGIN SELECT RAISE(ABORT, 'acceptance repair requests are immutable'); END;
CREATE TRIGGER acceptance_repair_targets_immutable_update BEFORE UPDATE ON acceptance_repair_targets
BEGIN SELECT RAISE(ABORT, 'acceptance repair targets are immutable'); END;
CREATE TRIGGER acceptance_repair_targets_immutable_delete BEFORE DELETE ON acceptance_repair_targets
BEGIN SELECT RAISE(ABORT, 'acceptance repair targets are immutable'); END;
CREATE TRIGGER acceptance_repair_request_seals_immutable_update BEFORE UPDATE ON acceptance_repair_request_seals
BEGIN SELECT RAISE(ABORT, 'acceptance repair request seals are immutable'); END;
CREATE TRIGGER acceptance_repair_request_seals_immutable_delete BEFORE DELETE ON acceptance_repair_request_seals
BEGIN SELECT RAISE(ABORT, 'acceptance repair request seals are immutable'); END;
CREATE TRIGGER acceptance_rejections_immutable_update BEFORE UPDATE ON acceptance_rejections
BEGIN SELECT RAISE(ABORT, 'acceptance rejections are immutable'); END;
CREATE TRIGGER acceptance_rejections_immutable_delete BEFORE DELETE ON acceptance_rejections
BEGIN SELECT RAISE(ABORT, 'acceptance rejections are immutable'); END;
CREATE TRIGGER acceptance_repair_rollback_intents_immutable_update BEFORE UPDATE ON acceptance_repair_rollback_intents
BEGIN SELECT RAISE(ABORT, 'repair rollback intents are immutable'); END;
CREATE TRIGGER acceptance_repair_rollback_intents_immutable_delete BEFORE DELETE ON acceptance_repair_rollback_intents
BEGIN SELECT RAISE(ABORT, 'repair rollback intents are immutable'); END;
CREATE TRIGGER acceptance_repair_rollback_outcomes_immutable_update BEFORE UPDATE ON acceptance_repair_rollback_outcomes
BEGIN SELECT RAISE(ABORT, 'repair rollback outcomes are immutable'); END;
CREATE TRIGGER acceptance_repair_rollback_outcomes_immutable_delete BEFORE DELETE ON acceptance_repair_rollback_outcomes
BEGIN SELECT RAISE(ABORT, 'repair rollback outcomes are immutable'); END;

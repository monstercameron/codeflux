CREATE TABLE task_risk_classifications (
    task_id TEXT NOT NULL REFERENCES tasks(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    policy_version TEXT NOT NULL CHECK (policy_version = 'risk-classification-v1'),
    selected_risk TEXT NOT NULL CHECK (selected_risk IN ('routine', 'elevated', 'protected')),
    user_override TEXT CHECK (user_override IS NULL OR user_override IN ('routine', 'elevated', 'protected')),
    explanation TEXT NOT NULL CHECK (length(explanation) BETWEEN 1 AND 8192),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    PRIMARY KEY (task_id, revision)
) STRICT;

CREATE TABLE task_risk_classification_signals (
    task_id TEXT NOT NULL,
    classification_revision INTEGER NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0 AND ordinal < 32),
    signal TEXT NOT NULL CHECK (length(signal) BETWEEN 1 AND 64),
    floor TEXT NOT NULL CHECK (floor IN ('routine', 'elevated', 'protected')),
    PRIMARY KEY (task_id, classification_revision, ordinal),
    UNIQUE (task_id, classification_revision, signal),
    FOREIGN KEY (task_id, classification_revision)
        REFERENCES task_risk_classifications(task_id, revision)
) STRICT;

CREATE TABLE task_risk_classification_reasons (
    task_id TEXT NOT NULL,
    classification_revision INTEGER NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0 AND ordinal < 34),
    reason_code TEXT NOT NULL CHECK (reason_code IN (
        'signal-floor', 'conservative-default',
        'user-override-raised', 'user-override-retained-floor'
    )),
    signal TEXT,
    floor TEXT NOT NULL CHECK (floor IN ('routine', 'elevated', 'protected')),
    PRIMARY KEY (task_id, classification_revision, ordinal),
    FOREIGN KEY (task_id, classification_revision)
        REFERENCES task_risk_classifications(task_id, revision),
    CHECK (
        (reason_code = 'signal-floor' AND signal IS NOT NULL)
        OR (reason_code <> 'signal-floor' AND signal IS NULL)
    )
) STRICT;

CREATE TRIGGER task_risk_classifications_revision_sequence
BEFORE INSERT ON task_risk_classifications
WHEN NEW.revision <> COALESCE((
    SELECT MAX(revision) + 1
    FROM task_risk_classifications
    WHERE task_id = NEW.task_id
), 1)
BEGIN
    SELECT RAISE(ABORT, 'task risk classification revision must be contiguous');
END;

CREATE TRIGGER task_risk_classifications_no_demotion
BEFORE INSERT ON task_risk_classifications
WHEN EXISTS (
    SELECT 1
    FROM task_risk_classifications AS previous
    WHERE previous.task_id = NEW.task_id
      AND previous.revision = NEW.revision - 1
      AND CASE previous.selected_risk
            WHEN 'routine' THEN 1 WHEN 'elevated' THEN 2 ELSE 3
          END > CASE NEW.selected_risk
            WHEN 'routine' THEN 1 WHEN 'elevated' THEN 2 ELSE 3
          END
)
BEGIN
    SELECT RAISE(ABORT, 'task risk classification cannot be demoted');
END;

CREATE TRIGGER task_risk_classifications_immutable_update
BEFORE UPDATE ON task_risk_classifications
BEGIN
    SELECT RAISE(ABORT, 'task risk classifications are immutable');
END;

CREATE TRIGGER task_risk_classifications_immutable_delete
BEFORE DELETE ON task_risk_classifications
BEGIN
    SELECT RAISE(ABORT, 'task risk classifications are immutable');
END;

CREATE TRIGGER task_risk_classification_signals_immutable_update
BEFORE UPDATE ON task_risk_classification_signals
BEGIN
    SELECT RAISE(ABORT, 'task risk classification signals are immutable');
END;

CREATE TRIGGER task_risk_classification_signals_immutable_delete
BEFORE DELETE ON task_risk_classification_signals
BEGIN
    SELECT RAISE(ABORT, 'task risk classification signals are immutable');
END;

CREATE TRIGGER task_risk_classification_reasons_immutable_update
BEFORE UPDATE ON task_risk_classification_reasons
BEGIN
    SELECT RAISE(ABORT, 'task risk classification reasons are immutable');
END;

CREATE TRIGGER task_risk_classification_reasons_immutable_delete
BEFORE DELETE ON task_risk_classification_reasons
BEGIN
    SELECT RAISE(ABORT, 'task risk classification reasons are immutable');
END;

CREATE INDEX task_risk_classifications_latest
    ON task_risk_classifications(task_id, revision DESC);

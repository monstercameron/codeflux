-- Widen the pipeline stage ledger so the flow can gain stages.
--
-- The flow gained an atom-documentation stage, which takes it past the
-- thirty-two the original CHECK allowed. SQLite cannot alter a CHECK in
-- place, so the table is rebuilt.
--
-- The bound is raised to sixty-four rather than to thirty-three. A bound that
-- has to be migrated every time the flow gains a step is a bound that will be
-- worked around instead of raised, and the point of the constraint is to
-- refuse a number that names no stage at all -- not to encode the current
-- length of the flow, which the Go vocabulary already owns.
--
-- A stage's identity is its name, not its number. The number orders the flow
-- and shifts when a stage is inserted; the name is what recorded evidence
-- should be read by. That was left implicit before this migration and is
-- stated here because a reader of old rows needs to know which of the two to
-- trust.

CREATE TABLE pipeline_stage_records_rebuilt (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    stage_number INTEGER NOT NULL CHECK (stage_number BETWEEN 1 AND 64),
    stage_name TEXT NOT NULL CHECK (length(stage_name) BETWEEN 1 AND 64),
    state TEXT NOT NULL CHECK (state IN (
        'satisfied', 'failed', 'skipped', 'blocked', 'not-implemented'
    )),
    gate_redacted TEXT NOT NULL CHECK (
        length(gate_redacted) BETWEEN 1 AND 512
    ),
    detail_redacted TEXT NOT NULL CHECK (
        length(detail_redacted) BETWEEN 0 AND 4096
    ),
    evidence_json TEXT NOT NULL CHECK (json_valid(evidence_json)),
    started_at_unix_micros INTEGER NOT NULL CHECK (
        started_at_unix_micros >= 0
    ),
    finished_at_unix_micros INTEGER NOT NULL CHECK (
        finished_at_unix_micros >= started_at_unix_micros
    ),
    UNIQUE (task_id, attempt, stage_number)
) STRICT;

INSERT INTO pipeline_stage_records_rebuilt (
    id, task_id, run_id, attempt, stage_number, stage_name, state,
    gate_redacted, detail_redacted, evidence_json,
    started_at_unix_micros, finished_at_unix_micros
)
SELECT
    id, task_id, run_id, attempt, stage_number, stage_name, state,
    gate_redacted, detail_redacted, evidence_json,
    started_at_unix_micros, finished_at_unix_micros
FROM pipeline_stage_records;

DROP TRIGGER pipeline_stage_records_immutable_update;
DROP TRIGGER pipeline_stage_records_immutable_delete;
DROP TRIGGER pipeline_stage_records_satisfied_needs_evidence;
DROP TRIGGER pipeline_stage_records_unimplemented_claims_nothing;
DROP INDEX pipeline_stage_records_by_task;
DROP INDEX pipeline_stage_records_by_state;
DROP TABLE pipeline_stage_records;

ALTER TABLE pipeline_stage_records_rebuilt
    RENAME TO pipeline_stage_records;

CREATE INDEX pipeline_stage_records_by_task
    ON pipeline_stage_records (task_id, attempt, stage_number);

CREATE INDEX pipeline_stage_records_by_state
    ON pipeline_stage_records (state, stage_number);

CREATE TRIGGER pipeline_stage_records_immutable_update
BEFORE UPDATE ON pipeline_stage_records
BEGIN
    SELECT RAISE(ABORT, 'pipeline stage records are immutable');
END;

CREATE TRIGGER pipeline_stage_records_immutable_delete
BEFORE DELETE ON pipeline_stage_records
BEGIN
    SELECT RAISE(ABORT, 'pipeline stage records are immutable');
END;

CREATE TRIGGER pipeline_stage_records_satisfied_needs_evidence
BEFORE INSERT ON pipeline_stage_records
WHEN NEW.state = 'satisfied'
    AND json_type(NEW.evidence_json) NOT IN ('object', 'array')
BEGIN
    SELECT RAISE(
        ABORT,
        'a satisfied pipeline stage must record structured evidence'
    );
END;

CREATE TRIGGER pipeline_stage_records_unimplemented_claims_nothing
BEFORE INSERT ON pipeline_stage_records
WHEN NEW.state = 'not-implemented'
    AND json_type(NEW.evidence_json) = 'object'
    AND (SELECT count(*) FROM json_each(NEW.evidence_json)) > 0
BEGIN
    SELECT RAISE(
        ABORT,
        'a stage that is not implemented cannot record what it produced'
    );
END;

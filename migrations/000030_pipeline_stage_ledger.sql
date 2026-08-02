-- Pipeline stage ledger: one immutable row per stage of the delivery flow
-- per task attempt, recording whether that stage was satisfied, failed,
-- skipped, blocked, or is not implemented yet.
--
-- The flow this records runs from the instruction through decomposition,
-- contracts, atoms, molecules, control flow, assembly, verification depth,
-- and delivery. Most of those stages do not exist in the product yet. Their
-- absence was previously visible only by reading the code and noticing that
-- nothing called them, which meant a run that skipped two thirds of the flow
-- and a run that satisfied all of it produced identical evidence: a green
-- result and a written file.
--
-- Recording every stage, including the ones that are not implemented, makes
-- the difference queryable. "Passed" and "never ran" must never render the
-- same way, and a stage that nothing implements is a fact about this build
-- rather than an omission in the record.

CREATE TABLE pipeline_stage_records (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT REFERENCES runs(id),
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    -- The stage's position in the flow. Bounded so a typo cannot invent a
    -- stage, and ordered so the ledger reads as the pipeline it describes.
    stage_number INTEGER NOT NULL CHECK (stage_number BETWEEN 1 AND 32),
    stage_name TEXT NOT NULL CHECK (length(stage_name) BETWEEN 1 AND 64),
    -- The closed set of outcomes. "not-implemented" is deliberately distinct
    -- from "skipped": skipped means this run had no need of the stage, while
    -- not-implemented means the product cannot perform it at all. Collapsing
    -- them would hide the gap this table exists to expose.
    state TEXT NOT NULL CHECK (state IN (
        'satisfied', 'failed', 'skipped', 'blocked', 'not-implemented'
    )),
    -- The condition that had to hold for this stage to pass, in words, so the
    -- record says what was checked and not merely that something was.
    gate_redacted TEXT NOT NULL CHECK (
        length(gate_redacted) BETWEEN 1 AND 512
    ),
    detail_redacted TEXT NOT NULL CHECK (
        length(detail_redacted) BETWEEN 0 AND 4096
    ),
    -- Whatever the stage produced, as structured data: counts, identities,
    -- scores. A stage claiming success with no evidence is a claim, and this
    -- column is where the difference is kept.
    evidence_json TEXT NOT NULL CHECK (json_valid(evidence_json)),
    started_at_unix_micros INTEGER NOT NULL CHECK (
        started_at_unix_micros >= 0
    ),
    finished_at_unix_micros INTEGER NOT NULL CHECK (
        finished_at_unix_micros >= started_at_unix_micros
    ),
    -- One row per stage per attempt. A second recording of the same stage in
    -- the same attempt would mean two different answers to one question.
    UNIQUE (task_id, attempt, stage_number)
) STRICT;

CREATE INDEX pipeline_stage_records_by_task
    ON pipeline_stage_records (task_id, attempt, stage_number);

CREATE INDEX pipeline_stage_records_by_state
    ON pipeline_stage_records (state, stage_number);

-- The ledger is evidence, so it is append-only. A stage that failed and was
-- then edited to say it passed would make the whole record worthless.
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

-- A stage that reports being satisfied must say what it produced. An empty
-- evidence object is allowed for stages that legitimately produce nothing,
-- but the column may not be null and must parse, so "satisfied" always comes
-- with something a reader can inspect.
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

-- A stage nothing implements cannot also claim to have produced a result.
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

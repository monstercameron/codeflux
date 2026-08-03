-- Carry an explicit signal for whether a pipeline stage's elapsed span was
-- actually tracked (PIPE-006b).
--
-- Before this migration, a stage recorded without a tracked start wrote the
-- finish time into both timestamp columns (see the doc comment on
-- RecordPipelineStage.StartedAt in internal/storage/pipeline_stage_repository.go),
-- so an untracked stage and a real stage that took zero elapsed microseconds
-- were stored identically: started_at_unix_micros equal to
-- finished_at_unix_micros in both cases. A reader computing a span from the
-- two columns alone cannot tell "not measured" from "instant", and exposing
-- the ledger over the wire needs that distinction rather than inventing one
-- at read time from data that does not actually carry it.
--
-- elapsed_measured is written once, at insert time, by the same code that
-- decides whether to accept a caller-supplied start
-- (RecordPipelineStageResult). Historical rows cannot be asked after the fact
-- whether their start was tracked -- that bit was never durable -- so they are
-- backfilled by the best available proxy: a row whose two timestamps differ
-- was certainly measured, and a row whose timestamps are equal is treated as
-- unmeasured. That matches every row this build has produced so far, since
-- internal/coordinator/agent_pipeline_ledger.go always supplies a start, and
-- is the conservative reading for any row that did not.

ALTER TABLE pipeline_stage_records
    ADD COLUMN elapsed_measured INTEGER NOT NULL DEFAULT 0
        CHECK (elapsed_measured IN (0, 1));

UPDATE pipeline_stage_records
    SET elapsed_measured = 1
    WHERE finished_at_unix_micros > started_at_unix_micros;

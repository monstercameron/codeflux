-- MEM-001..005 "Episode Lifecycle" (docs/plan.md §31 Chronological Episodes;
-- Influence Lineage). Forward-only.
--
-- Migration 000027 gave the append-only episode record a schema with nothing
-- writing into it: OpenEpisode and CloseEpisode had zero production callers.
-- This migration is what that wiring needs from storage, in four parts.
--
-- MEM-003 "a task started twice produces two episodes and neither inherits
-- the other's evidence": migration 000027 declared `task_id TEXT NOT NULL
-- UNIQUE`, so a task could hold at most one episode ever, which is the
-- opposite of what a retried task needs -- storage.NextPipelineAttempt
-- (PIPE-003) already numbers a task's successive runs as attempt 1, 2, 3...,
-- and a second attempt at the same task must open its own episode rather
-- than be refused or silently merged into the first. `episodes` is rebuilt
-- (CREATE + copy + drop + rename) rather than altered in place because
-- SQLite has no ALTER TABLE that can drop a column-level UNIQUE constraint;
-- the rebuild is safe under `PRAGMA foreign_keys=ON` (this connection's
-- fixed setting; see internal/storage/database.go) only because every
-- table that references `episodes(id)` -- episode_fact_references,
-- episode_invalidation_overlays, memory_artifact_supporting_episode -- is
-- guaranteed empty at this point in every database that will ever apply
-- this migration: OpenEpisode has never had a production caller before this
-- milestone, so nothing has ever written a row into any of them. A rebuild
-- that had to preserve non-empty child rows across a re-parented table would
-- need the child tables rebuilt in the same pass, which is deliberately not
-- built here because the situation it would handle cannot occur.
--
-- MEM-005 "record advisory_exposure on every episode as a write-once
-- transition rather than a property asserted at open": the new
-- `advisory_exposure` column defaults every episode to 'unexposed', and
-- `episodes_advisory_exposure_write_once` is the schema-level enforcement --
-- mirroring migrations/000034_proof_obligations.sql's CHECK-based discharge
-- proof, except the rule here is a direction of travel (unexposed ->
-- exposed, never back) rather than a same-row co-presence, so it is a
-- BEFORE UPDATE trigger comparing OLD and NEW rather than a CHECK, which
-- SQLite cannot express against OLD/NEW at all.
--
-- MEM-002 "record each attempt's gate, normalised failure, rung, and
-- outcome as episode action events": `episode_attempt_transitions` is the
-- durable home for the facts a run's send-back/escalate/decompose/converge
-- decisions previously only ever existed as narrated chat strings. The
-- table is deliberately not named `episode_action_events`: that phrase
-- already denotes a different, pre-existing mechanism in this codebase --
-- (*Repositories).ListEpisodeActionEvents (migration 000027, M21-032) reads
-- the ordered task_events journal for an episode's task, and copies no
-- payload of its own. What MEM-002 asks for is a new fact this build has
-- never recorded anywhere -- the attempt-to-attempt transition itself, not
-- the events inside one attempt -- so it gets its own name rather than
-- overloading that one.
--
-- MEM-004a "record the influence action and justification for every carried
-- item at episode close": `episode_memory_influence_events` is the durable
-- link from an episode to the specific memory_retrieval_candidates row(s)
-- RunPreWorkGate found eligible for the episode's own task, plus what the
-- run actually did with each one. It deliberately does not touch
-- memory_retrieval_decisions or memory_retrieval_candidates -- both remain
-- exactly as migration 000025 declared them -- so this is purely additive.

-- ---------------------------------------------------------------------
-- MEM-003: rebuild episodes with an attempt binding.
-- ---------------------------------------------------------------------

CREATE TABLE episodes_mem003 (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    -- The pipeline ledger's own attempt number (PIPE-003,
    -- storage.NextPipelineAttempt), bound at open and immutable thereafter
    -- (see episodes_immutable_identity below). Attempt 1 is a task's first
    -- run; a task started again after that gets attempt 2, and so on.
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    fingerprint_schema_version INTEGER NOT NULL REFERENCES task_fingerprint_schema_versions(version),
    fingerprint_hash TEXT NOT NULL CHECK (length(fingerprint_hash) = 64 AND fingerprint_hash NOT GLOB '*[^0-9a-f]*'),
    starting_revision TEXT NOT NULL CHECK (length(starting_revision) BETWEEN 1 AND 255),
    ending_revision TEXT CHECK (ending_revision IS NULL OR length(ending_revision) BETWEEN 1 AND 255),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    outcome TEXT CHECK (outcome IS NULL OR outcome IN ('accepted', 'rejected', 'abandoned', 'unresolved')),
    -- MEM-005: opens unexposed; becomes exposed the first time an advisory
    -- pattern enters the run; can never be relabelled unexposed (see the
    -- write-once trigger below). This is what makes the permanent
    -- clean-room cohort §31 requires identifiable after the fact.
    advisory_exposure TEXT NOT NULL DEFAULT 'unexposed' CHECK (advisory_exposure IN ('unexposed', 'exposed')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    started_at_unix_micros INTEGER NOT NULL CHECK (started_at_unix_micros >= 0),
    closed_at_unix_micros INTEGER CHECK (closed_at_unix_micros IS NULL OR closed_at_unix_micros >= started_at_unix_micros),
    -- One task may now hold one episode PER ATTEMPT rather than one ever.
    UNIQUE (task_id, attempt),
    UNIQUE (task_id, idempotency_key),
    CHECK (
        (status = 'open' AND outcome IS NULL AND ending_revision IS NULL AND closed_at_unix_micros IS NULL)
        OR
        (status = 'closed' AND outcome IS NOT NULL AND ending_revision IS NOT NULL AND closed_at_unix_micros IS NOT NULL)
    )
) STRICT;

INSERT INTO episodes_mem003 (
    id, project_id, repository_id, task_id, attempt, fingerprint_schema_version, fingerprint_hash,
    starting_revision, ending_revision, status, outcome, advisory_exposure, idempotency_key,
    started_at_unix_micros, closed_at_unix_micros
)
SELECT
    id, project_id, repository_id, task_id, 1, fingerprint_schema_version, fingerprint_hash,
    starting_revision, ending_revision, status, outcome, 'unexposed', idempotency_key,
    started_at_unix_micros, closed_at_unix_micros
FROM episodes;

-- The three dependent tables are dropped before `episodes` itself, in this
-- order, for a reason that is not optional: SQLite's ALTER TABLE RENAME TO
-- (below) revalidates every trigger body left in the schema as part of its
-- own rename-reference fixup, and episode_fact_references_task_boundary's
-- WHEN clause names `episodes` -- a table that, for the instant the rename
-- is executing, is mid-rename. Left in place, that revalidation fails with
-- "no such table: main.episodes" even though the rename itself would have
-- succeeded a statement later. Dropping the three dependents first (safe:
-- see the top-of-file note -- all three are always empty here, and none of
-- them has anything else depending on it) removes every trigger body that
-- could be caught by that fixup scan, so the rename below has nothing left
-- to trip over. They are recreated, unchanged, further down.
DROP TABLE memory_artifact_supporting_episode;
DROP TABLE episode_invalidation_overlays;
DROP TABLE episode_fact_references;
DROP TABLE episodes;
ALTER TABLE episodes_mem003 RENAME TO episodes;

CREATE INDEX episodes_by_project ON episodes(project_id, status);
CREATE INDEX episodes_by_repository ON episodes(repository_id, status);
CREATE INDEX episodes_by_task_attempt ON episodes(task_id, attempt);

CREATE TRIGGER episodes_task_boundary
BEFORE INSERT ON episodes
WHEN NOT EXISTS (
    SELECT 1 FROM tasks
    JOIN threads ON threads.id = tasks.thread_id
    WHERE tasks.id = NEW.task_id
      AND tasks.repository_id = NEW.repository_id
      AND threads.project_id = NEW.project_id
)
BEGIN SELECT RAISE(ABORT, 'episode project/repository does not match its task'); END;

-- Identity and every fact fixed at episode start are immutable for the
-- entire life of the episode, including its one legitimate open -> closed
-- transition. `attempt` joins this set (MEM-003): it is bound at open, the
-- same as starting_revision. `advisory_exposure` deliberately does NOT join
-- it: MEM-005 requires that column to move exactly once, after open, which
-- this trigger would otherwise forbid outright.
CREATE TRIGGER episodes_immutable_identity
BEFORE UPDATE ON episodes
WHEN NEW.id != OLD.id
  OR NEW.project_id != OLD.project_id
  OR NEW.repository_id != OLD.repository_id
  OR NEW.task_id != OLD.task_id
  OR NEW.attempt != OLD.attempt
  OR NEW.fingerprint_schema_version != OLD.fingerprint_schema_version
  OR NEW.fingerprint_hash != OLD.fingerprint_hash
  OR NEW.starting_revision != OLD.starting_revision
  OR NEW.idempotency_key != OLD.idempotency_key
  OR NEW.started_at_unix_micros != OLD.started_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'episode identity and starting facts are immutable'); END;

CREATE TRIGGER episodes_immutable_after_close
BEFORE UPDATE ON episodes
WHEN OLD.status = 'closed'
BEGIN SELECT RAISE(ABORT, 'episode is frozen after its terminal decision'); END;

CREATE TRIGGER episodes_reject_delete
BEFORE DELETE ON episodes
BEGIN SELECT RAISE(ABORT, 'episodes are immutable history and are never deleted'); END;

-- MEM-005: the write-once enforcement. A CHECK constraint cannot compare
-- OLD and NEW, so the direction rule is a trigger; this is the schema doing
-- the enforcing, not application discipline, exactly as
-- migrations/000034_proof_obligations.sql's discharge CHECK does for a
-- same-row invariant.
CREATE TRIGGER episodes_advisory_exposure_write_once
BEFORE UPDATE ON episodes
WHEN OLD.advisory_exposure = 'exposed' AND NEW.advisory_exposure != 'exposed'
BEGIN SELECT RAISE(ABORT, 'advisory exposure can never be relabelled unexposed'); END;

-- ---------------------------------------------------------------------
-- Recreate the three tables just dropped above. Their own shape is
-- unchanged from migrations/000027_chronological_episodes.sql -- every
-- trigger, index, and constraint below is copied verbatim; they were
-- dropped only so the `episodes` rebuild above could run (see the comment
-- ahead of those DROP statements), and SQLite resolves a foreign key by the
-- name in the REFERENCES clause, not by a stable internal identity, so they
-- need to be declared again against the rebuilt `episodes`.
-- ---------------------------------------------------------------------

CREATE TABLE episode_fact_references (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id),
    fact_task_id TEXT NOT NULL REFERENCES tasks(id),
    fact_kind TEXT NOT NULL CHECK (fact_kind IN (
        'requirement-revision', 'plan-revision-proposed', 'plan-revision-accepted',
        'repository-context-revision', 'user-intervention', 'user-approval',
        'validation-result', 'final-decision', 'forecast', 'actual-metrics',
        'pre-repair-failure', 'repair-attempt'
    )),
    fact_revision INTEGER CHECK (fact_revision IS NULL OR fact_revision >= 1),
    fact_reference_id TEXT CHECK (fact_reference_id IS NULL OR length(fact_reference_id) BETWEEN 1 AND 255),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    UNIQUE (episode_id, fact_kind, ordinal),
    UNIQUE (episode_id, idempotency_key),
    CHECK (fact_revision IS NOT NULL OR fact_reference_id IS NOT NULL)
) STRICT;

CREATE INDEX episode_fact_references_by_episode ON episode_fact_references(episode_id, fact_kind, ordinal);

CREATE TRIGGER episode_fact_references_task_boundary
BEFORE INSERT ON episode_fact_references
WHEN NEW.fact_task_id != (SELECT task_id FROM episodes WHERE id = NEW.episode_id)
BEGIN SELECT RAISE(ABORT, 'episode fact reference task does not match its episode'); END;

CREATE TRIGGER episode_fact_references_repair_requires_prior_failure
BEFORE INSERT ON episode_fact_references
WHEN NEW.fact_kind = 'repair-attempt'
 AND NOT EXISTS (
     SELECT 1 FROM episode_fact_references
     WHERE episode_id = NEW.episode_id AND fact_kind = 'pre-repair-failure'
 )
BEGIN SELECT RAISE(ABORT, 'repair attempt fact requires a pre-repair failure fact recorded first'); END;

CREATE TRIGGER episode_fact_references_immutable_update
BEFORE UPDATE ON episode_fact_references
BEGIN SELECT RAISE(ABORT, 'episode fact references are immutable'); END;
CREATE TRIGGER episode_fact_references_immutable_delete
BEFORE DELETE ON episode_fact_references
BEGIN SELECT RAISE(ABORT, 'episode fact references are immutable'); END;

CREATE TABLE episode_invalidation_overlays (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    overlay_kind TEXT NOT NULL CHECK (overlay_kind IN (
        'outcome-invalidated', 'evidence-invalidated', 'fingerprint-superseded', 'other'
    )),
    current_assessment TEXT NOT NULL CHECK (current_assessment IN ('reliable', 'unreliable', 'superseded')),
    reason_redacted TEXT NOT NULL CHECK (length(reason_redacted) BETWEEN 1 AND 8192),
    replacement_episode_id TEXT REFERENCES episodes(id),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    UNIQUE (episode_id, sequence),
    UNIQUE (episode_id, idempotency_key),
    CHECK (replacement_episode_id IS NULL OR replacement_episode_id != episode_id)
) STRICT;

CREATE INDEX episode_invalidation_overlays_by_episode ON episode_invalidation_overlays(episode_id, sequence DESC);

CREATE TRIGGER episode_invalidation_overlays_requires_closed_episode
BEFORE INSERT ON episode_invalidation_overlays
WHEN (SELECT status FROM episodes WHERE id = NEW.episode_id) != 'closed'
BEGIN SELECT RAISE(ABORT, 'episode invalidation overlay requires a closed episode'); END;

CREATE TRIGGER episode_invalidation_overlays_sequence_contiguous
BEFORE INSERT ON episode_invalidation_overlays
WHEN NEW.sequence != COALESCE((SELECT MAX(sequence) FROM episode_invalidation_overlays WHERE episode_id = NEW.episode_id), 0) + 1
BEGIN SELECT RAISE(ABORT, 'episode invalidation overlay sequence must be contiguous'); END;

CREATE TRIGGER episode_invalidation_overlays_replacement_boundary
BEFORE INSERT ON episode_invalidation_overlays
WHEN NEW.replacement_episode_id IS NOT NULL
 AND (SELECT project_id FROM episodes WHERE id = NEW.replacement_episode_id)
     != (SELECT project_id FROM episodes WHERE id = NEW.episode_id)
BEGIN SELECT RAISE(ABORT, 'episode invalidation overlay replacement crosses the owning project boundary'); END;

CREATE TRIGGER episode_invalidation_overlays_immutable_update
BEFORE UPDATE ON episode_invalidation_overlays
BEGIN SELECT RAISE(ABORT, 'episode invalidation overlays are immutable'); END;
CREATE TRIGGER episode_invalidation_overlays_immutable_delete
BEFORE DELETE ON episode_invalidation_overlays
BEGIN SELECT RAISE(ABORT, 'episode invalidation overlays are immutable'); END;

CREATE TABLE memory_artifact_supporting_episode (
    artifact_id TEXT NOT NULL REFERENCES memory_artifacts(id),
    episode_id TEXT NOT NULL REFERENCES episodes(id),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    PRIMARY KEY (artifact_id, episode_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX memory_artifact_supporting_episode_by_episode
    ON memory_artifact_supporting_episode(episode_id);

CREATE TRIGGER memory_artifact_supporting_episode_project_boundary
BEFORE INSERT ON memory_artifact_supporting_episode
WHEN (SELECT project_id FROM memory_artifacts WHERE id = NEW.artifact_id)
     != (SELECT project_id FROM episodes WHERE id = NEW.episode_id)
BEGIN SELECT RAISE(ABORT, 'memory artifact supporting episode crosses the owning project boundary'); END;

CREATE TRIGGER memory_artifact_supporting_episode_immutable_update
BEFORE UPDATE ON memory_artifact_supporting_episode
BEGIN SELECT RAISE(ABORT, 'memory artifact supporting episode links are immutable'); END;
CREATE TRIGGER memory_artifact_supporting_episode_immutable_delete
BEFORE DELETE ON memory_artifact_supporting_episode
BEGIN SELECT RAISE(ABORT, 'memory artifact supporting episode links are immutable'); END;

-- ---------------------------------------------------------------------
-- MEM-002: episode attempt transitions. One row per attempt transition --
-- the gate that evaluated it, the normalised failure that sent it back
-- (NULL on the attempt that converges), the rung it ran on, and what
-- happened next.
-- ---------------------------------------------------------------------

CREATE TABLE episode_attempt_transitions (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id),
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    gate TEXT NOT NULL CHECK (length(gate) BETWEEN 1 AND 128),
    normalized_failure TEXT CHECK (normalized_failure IS NULL OR length(normalized_failure) BETWEEN 1 AND 2048),
    rung TEXT NOT NULL CHECK (length(rung) BETWEEN 1 AND 64),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'sent-back', 'escalated', 'decomposed', 'awaiting-approval', 'converged'
    )),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    UNIQUE (episode_id, attempt, ordinal),
    UNIQUE (episode_id, idempotency_key),
    -- A converged attempt (the run's own success) has nothing that sent it
    -- back; every other outcome exists only because something did.
    CHECK ((outcome = 'converged') = (normalized_failure IS NULL))
) STRICT;

CREATE INDEX episode_attempt_transitions_by_episode ON episode_attempt_transitions(episode_id, attempt, ordinal);

CREATE TRIGGER episode_attempt_transitions_immutable_update
BEFORE UPDATE ON episode_attempt_transitions
BEGIN SELECT RAISE(ABORT, 'episode attempt transitions are immutable'); END;
CREATE TRIGGER episode_attempt_transitions_immutable_delete
BEFORE DELETE ON episode_attempt_transitions
BEGIN SELECT RAISE(ABORT, 'episode attempt transitions are immutable'); END;

-- ---------------------------------------------------------------------
-- MEM-004a: what the run did with each memory item RunPreWorkGate found
-- eligible for the episode's own task. Purely additive over migration
-- 000025's memory_retrieval_candidates/memory_retrieval_queries -- neither
-- table is touched.
-- ---------------------------------------------------------------------

CREATE TABLE episode_memory_influence_events (
    id TEXT PRIMARY KEY,
    episode_id TEXT NOT NULL REFERENCES episodes(id),
    candidate_id TEXT NOT NULL REFERENCES memory_retrieval_candidates(id),
    action TEXT NOT NULL CHECK (action IN ('used', 'adapted', 'rejected')),
    justification_redacted TEXT NOT NULL CHECK (length(justification_redacted) BETWEEN 1 AND 2048),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    -- One recorded influence per candidate per episode: eligibility becomes
    -- influence at most once, not once per re-evaluation.
    UNIQUE (episode_id, candidate_id),
    UNIQUE (episode_id, idempotency_key)
) STRICT;

CREATE INDEX episode_memory_influence_events_by_episode ON episode_memory_influence_events(episode_id);

-- The candidate carried into an episode's close must be one the episode's
-- own task actually queried for -- never a candidate borrowed from another
-- task's retrieval pass, per AGENTS.md "Add explicit project-boundary
-- predicates to memory, graph, vector, and retrieval queries."
CREATE TRIGGER episode_memory_influence_events_candidate_boundary
BEFORE INSERT ON episode_memory_influence_events
WHEN NOT EXISTS (
    SELECT 1 FROM memory_retrieval_candidates
    JOIN memory_retrieval_queries ON memory_retrieval_queries.id = memory_retrieval_candidates.query_id
    JOIN episodes ON episodes.task_id = memory_retrieval_queries.task_id
    WHERE memory_retrieval_candidates.id = NEW.candidate_id AND episodes.id = NEW.episode_id
)
BEGIN SELECT RAISE(ABORT, 'episode memory influence candidate was not queried for this episode''s task'); END;

CREATE TRIGGER episode_memory_influence_events_immutable_update
BEFORE UPDATE ON episode_memory_influence_events
BEGIN SELECT RAISE(ABORT, 'episode memory influence events are immutable'); END;
CREATE TRIGGER episode_memory_influence_events_immutable_delete
BEFORE DELETE ON episode_memory_influence_events
BEGIN SELECT RAISE(ABORT, 'episode memory influence events are immutable'); END;

-- ---------------------------------------------------------------------
-- MEM-001: the exact task fingerprint (schema version + hash) is computed
-- once, by TaskPreflightService.ForecastTask, long before a run -- and so
-- long before an episode -- exists. Nothing durable carried it from there
-- to episode-open, so OpenEpisode had no honest value to put in
-- episodes.fingerprint_hash without recomputing it from inputs a run does
-- not have (internal/coordinator/task_intake.go's AffectedPaths,
-- AffectedPackages, AffectedSymbols, ToolchainBindings, and
-- RequestedAuthority are never persisted onto the task row itself). This
-- table is that carry: one immutable binding per task, written once at
-- forecast time and read once at episode-open time.
-- ---------------------------------------------------------------------

CREATE TABLE task_exact_fingerprint_bindings (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id),
    fingerprint_schema_version INTEGER NOT NULL REFERENCES task_fingerprint_schema_versions(version),
    fingerprint_hash TEXT NOT NULL CHECK (length(fingerprint_hash) = 64 AND fingerprint_hash NOT GLOB '*[^0-9a-f]*'),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER task_exact_fingerprint_bindings_immutable_update
BEFORE UPDATE ON task_exact_fingerprint_bindings
BEGIN SELECT RAISE(ABORT, 'a task''s exact fingerprint binding is immutable once recorded'); END;
CREATE TRIGGER task_exact_fingerprint_bindings_immutable_delete
BEFORE DELETE ON task_exact_fingerprint_bindings
BEGIN SELECT RAISE(ABORT, 'a task''s exact fingerprint binding is immutable once recorded'); END;

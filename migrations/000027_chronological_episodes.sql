-- Milestone 21 (M21-029..039) chronological episode capture: one immutable,
-- append-only record per completed or terminated task (docs/plan.md §0
-- "Episode"; §31 "Chronological Episodes"). See internal/domain/episode.go
-- for the boundary, freeze, and overlay rules this schema enforces.
--
-- Design note: per the M21-032 direction "do not copy event payloads;
-- reference the existing event journal", and generalized across this whole
-- section, an episode is deliberately a thin index over facts that already
-- have a durable, immutable home elsewhere:
--   * ordered actions and results (M21-032) already live in task_events,
--     which already carries a per-task monotonic `sequence`; because
--     episodes.task_id is unique (one episode per task), the ordered
--     timeline is simply `task_events WHERE task_id = episode.task_id
--     ORDER BY sequence` -- no new link table, no payload copy.
--   * requirement/plan revisions (M21-030), forecast/actual metrics
--     (M21-035), validation/final decisions (M21-034), and repair attempts
--     (M21-036) already live in task_requirement_revisions,
--     agent_plan_revisions, effort_forecast_revisions, forecast_outcomes,
--     acceptance_reviews, acceptance_decisions, and repair_attempts, all
--     already keyed by task_id.
-- episode_fact_references (declared below) is the one generic, uniform
-- reference mechanism this migration adds: it lets an episode explicitly
-- name which revision-numbered or ID-keyed row of those existing tables it
-- is grounded in, for every M21-030..036 fact kind, without duplicating any
-- of their content and without requiring this migration to re-derive the
-- deep multi-table consistency rules those tables' own migrations already
-- enforce.

-- episodes (M21-029, M21-037, M21-038): the boundary everything else hangs
-- off. An episode opens when its task is received and its starting
-- revision is selected, and it closes exactly once, at the task's terminal
-- user decision (outcome). One episode covers exactly one task
-- (UNIQUE task_id).
CREATE TABLE episodes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
    fingerprint_schema_version INTEGER NOT NULL REFERENCES task_fingerprint_schema_versions(version),
    fingerprint_hash TEXT NOT NULL CHECK (length(fingerprint_hash) = 64 AND fingerprint_hash NOT GLOB '*[^0-9a-f]*'),
    starting_revision TEXT NOT NULL CHECK (length(starting_revision) BETWEEN 1 AND 255),
    ending_revision TEXT CHECK (ending_revision IS NULL OR length(ending_revision) BETWEEN 1 AND 255),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    outcome TEXT CHECK (outcome IS NULL OR outcome IN ('accepted', 'rejected', 'abandoned', 'unresolved')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    started_at_unix_micros INTEGER NOT NULL CHECK (started_at_unix_micros >= 0),
    closed_at_unix_micros INTEGER CHECK (closed_at_unix_micros IS NULL OR closed_at_unix_micros >= started_at_unix_micros),
    UNIQUE (task_id, idempotency_key),
    -- M21-037: outcome, ending_revision, and closed_at are a single
    -- all-or-nothing group that appears exactly when status = 'closed'.
    CHECK (
        (status = 'open' AND outcome IS NULL AND ending_revision IS NULL AND closed_at_unix_micros IS NULL)
        OR
        (status = 'closed' AND outcome IS NOT NULL AND ending_revision IS NOT NULL AND closed_at_unix_micros IS NOT NULL)
    )
) STRICT;

CREATE INDEX episodes_by_project ON episodes(project_id, status);
CREATE INDEX episodes_by_repository ON episodes(repository_id, status);

-- Every episode must belong to the project and repository its own task
-- actually belongs to, per AGENTS.md "Add explicit project-boundary
-- predicates to memory, graph, vector, and retrieval queries."
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

-- M21-029: identity and every fact fixed at episode start are immutable for
-- the entire life of the episode, including its one legitimate open ->
-- closed transition.
CREATE TRIGGER episodes_immutable_identity
BEFORE UPDATE ON episodes
WHEN NEW.id != OLD.id
  OR NEW.project_id != OLD.project_id
  OR NEW.repository_id != OLD.repository_id
  OR NEW.task_id != OLD.task_id
  OR NEW.fingerprint_schema_version != OLD.fingerprint_schema_version
  OR NEW.fingerprint_hash != OLD.fingerprint_hash
  OR NEW.starting_revision != OLD.starting_revision
  OR NEW.idempotency_key != OLD.idempotency_key
  OR NEW.started_at_unix_micros != OLD.started_at_unix_micros
BEGIN SELECT RAISE(ABORT, 'episode identity and starting facts are immutable'); END;

-- M21-038: freeze the episode after its terminal user decision. Once
-- status = 'closed', no further UPDATE of any kind is accepted -- not even
-- an attempt to "unclose" it -- so this is strictly stronger than the
-- identity trigger above for a closed row. This is the storage-layer
-- mirror of domain.ValidateEpisodeStatusTransition, which declares no
-- transition out of Closed.
CREATE TRIGGER episodes_immutable_after_close
BEFORE UPDATE ON episodes
WHEN OLD.status = 'closed'
BEGIN SELECT RAISE(ABORT, 'episode is frozen after its terminal decision'); END;

CREATE TRIGGER episodes_reject_delete
BEFORE DELETE ON episodes
BEGIN SELECT RAISE(ABORT, 'episodes are immutable history and are never deleted'); END;

-- episode_fact_references (M21-030, M21-031, M21-033, M21-034, M21-035,
-- M21-036): a generic, append-only reference from an episode to an
-- already-durable fact recorded elsewhere, by revision number, row
-- identity, or both. Never a payload copy.
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

-- M21-036: "failures BEFORE repairs ... the pre-repair failure is the
-- evidence; do not let a later repair erase it." Because this table is
-- insert-only (see the immutability triggers below), a recorded
-- 'pre-repair-failure' fact can never be overwritten by a later
-- 'repair-attempt' fact; this trigger additionally makes the ORDERING
-- itself mechanical, refusing any 'repair-attempt' fact for an episode
-- that has no 'pre-repair-failure' fact recorded first.
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

-- episode_invalidation_overlays (M21-039): later correction of a closed
-- episode's history is expressed only as a new, append-only overlay row,
-- per docs/plan.md §31 "Historical Claim Invalidation": "Immutable
-- revisions retain what was asserted at the time, but a mutable
-- claim-status overlay records what is currently known." The episode row
-- itself is never mutated to reflect a later correction; see
-- episodes_immutable_after_close above.
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

-- An overlay only ever corrects a closed (frozen) episode's history; there
-- is nothing yet to invalidate about one still open.
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

-- memory_artifact_supporting_episode closes the domain.MemoryArtifactLineage.
-- SupportingEpisodes landmine: that field previously had no backing table
-- and was always read back empty, which would let
-- domain.ConfirmsMemoryArtifactIndependently treat every candidate episode
-- as "never exposed" and always confirm independence the moment any caller
-- populated an episode-derived lineage index -- defeating the §31
-- anti-contamination rule. This table is the real backing store: it
-- records which episode(s) exposed/supported one memory-artifact identity.
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

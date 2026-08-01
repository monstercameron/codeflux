-- Milestone 21 (M21-064..076 retrieval gate; M21-137 storage gap) additions
-- to the retrieval-candidate and retrieval-decision logs migration 000025
-- already declared. Forward-only: this migration adds two new tables and
-- touches nothing in 000025, 000026, or 000027.
--
-- M21-137 ("Record whether an atom was retrieved from exact identity,
-- structured fields, vector similarity, or several channels") cannot be
-- represented losslessly by memory_retrieval_candidates.candidate_source
-- alone: that column is a single-valued CHECK column (migration 000025),
-- so a candidate legitimately discovered through several channels at once
-- (for example matched by both an exact-identity lookup and a structured
-- applicability filter in the same retrieval pass) can only be logged
-- there as whichever single channel internal/retrievalgate's
-- CollapseChannelsForStorage picked as strongest -- the other channel(s)
-- are silently lost. memory_retrieval_candidate_channels is the lossless
-- companion: one row per (candidate, channel) pair. The legacy
-- candidate_source column is kept, unchanged, because
-- memory_retrieval_decisions and every existing query that only needs "the
-- one strongest channel" still has a single value to read; this table is
-- additive, not a replacement.
CREATE TABLE memory_retrieval_candidate_channels (
    candidate_id TEXT NOT NULL REFERENCES memory_retrieval_candidates(id),
    channel TEXT NOT NULL CHECK (channel IN ('exact-match', 'applicability-pass', 'vector-similarity')),
    similarity_score REAL CHECK (similarity_score IS NULL OR (similarity_score >= -1.0 AND similarity_score <= 1.0)),
    created_at_unix_micros INTEGER NOT NULL CHECK (created_at_unix_micros >= 0),
    PRIMARY KEY (candidate_id, channel),
    CHECK ((channel = 'vector-similarity') = (similarity_score IS NOT NULL))
) STRICT, WITHOUT ROWID;

CREATE INDEX memory_retrieval_candidate_channels_by_candidate
    ON memory_retrieval_candidate_channels(candidate_id);

-- No separate project-boundary trigger is needed: every row here is scoped
-- by its REFERENCES to memory_retrieval_candidates(id), which already
-- enforces (via memory_retrieval_candidates_project_boundary, migration
-- 000025) that the candidate's revision belongs to the query's project.

CREATE TRIGGER memory_retrieval_candidate_channels_immutable_update
BEFORE UPDATE ON memory_retrieval_candidate_channels
BEGIN SELECT RAISE(ABORT, 'memory retrieval candidate channels are immutable'); END;
CREATE TRIGGER memory_retrieval_candidate_channels_immutable_delete
BEFORE DELETE ON memory_retrieval_candidate_channels
BEGIN SELECT RAISE(ABORT, 'memory retrieval candidate channels are immutable'); END;

-- M21-076 ("Fall back to ordinary planning when no eligible item exists...
-- [this] must be recorded as such"): a normal outcome, not an error, and a
-- QUERY-scoped fact distinct from any one candidate's own gate-failure
-- reason. memory_retrieval_decisions cannot carry it: every row there is
-- scoped to exactly one candidate (candidate_id is NOT NULL and UNIQUE), so
-- there is nothing to attach a "nothing in this query was eligible" fact
-- to when either zero candidates were discovered at all, or every
-- discovered candidate was individually rejected for its own
-- already-logged reason. memory_retrieval_fallbacks is the query-scoped
-- record of that outcome instead.
CREATE TABLE memory_retrieval_fallbacks (
    id TEXT PRIMARY KEY,
    query_id TEXT NOT NULL UNIQUE REFERENCES memory_retrieval_queries(id),
    candidates_considered INTEGER NOT NULL CHECK (candidates_considered >= 0),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0)
) STRICT;

CREATE TRIGGER memory_retrieval_fallbacks_immutable_update
BEFORE UPDATE ON memory_retrieval_fallbacks
BEGIN SELECT RAISE(ABORT, 'memory retrieval fallbacks are immutable'); END;
CREATE TRIGGER memory_retrieval_fallbacks_immutable_delete
BEFORE DELETE ON memory_retrieval_fallbacks
BEGIN SELECT RAISE(ABORT, 'memory retrieval fallbacks are immutable'); END;

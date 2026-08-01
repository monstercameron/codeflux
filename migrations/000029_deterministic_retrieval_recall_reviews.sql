-- M21-078 "Measure deterministic retrieval misses before enabling
-- embeddings": the durable half of the measurement instrument. Forward-only:
-- this migration adds one new table and touches nothing in 000025..000028.
--
-- docs/plan.md §0 "Branch Points and Stop Gates" keeps vector discovery
-- closed "unless deterministic retrieval has a measured recall problem."
-- memory_retrieval_fallbacks (migration 000028) already durably records
-- WHEN the exact/structured channels returned zero eligible candidates for a
-- query, but that fact alone cannot answer the branch question: a fallback
-- is the expected, correct outcome whenever no reusable artifact actually
-- existed yet. Telling "no reusable artifact existed" apart from "one
-- existed and deterministic retrieval genuinely missed it" is not a fact
-- retrieval can derive about itself -- it requires a human reviewer who
-- later learns whether a reusable artifact existed (e.g. because the same
-- shape of work reappeared, or a human recognized a duplicate after the
-- fact). memory_retrieval_recall_reviews is that reviewer verdict, bound
-- immutably to the exact fallback it is reviewing.
--
-- Per docs/plan.md §31 "Agent explanations are stored only as
-- agent_self_report with evidence_strength: none. They are not treated as
-- causal accounts" and AGENTS.md's prohibition on the dependency
-- "agent self-report -> accepted outcome", this table has no reviewer-kind
-- column offering a self-report option at all: reviewer_identity always
-- names a human reviewer (a free-text, bounded, redacted handle), enforced
-- at the internal/storage.CreateMemoryRetrievalRecallReview call site
-- exactly like UpsertExtractedMemoryFact already refuses
-- domain.EvidenceSourceKindAgentSelfReport for fact admission.
CREATE TABLE memory_retrieval_recall_reviews (
    id TEXT PRIMARY KEY,
    query_id TEXT NOT NULL UNIQUE REFERENCES memory_retrieval_queries(id),
    verdict TEXT NOT NULL CHECK (verdict IN ('genuine-miss', 'no-reusable-artifact-existed', 'inconclusive')),
    missed_artifact_reference TEXT CHECK (missed_artifact_reference IS NULL OR length(missed_artifact_reference) BETWEEN 1 AND 512),
    reviewer_identity_redacted TEXT NOT NULL CHECK (length(reviewer_identity_redacted) BETWEEN 1 AND 255),
    detail_redacted TEXT NOT NULL CHECK (length(detail_redacted) BETWEEN 1 AND 2048),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    -- Mirrors the vector-similarity-score CHECK pattern already used on
    -- memory_retrieval_candidates/memory_retrieval_candidate_channels: the
    -- one field meaningful only for one specific value of another column is
    -- required exactly when that value is present, never otherwise.
    CHECK ((verdict = 'genuine-miss') = (missed_artifact_reference IS NOT NULL))
) STRICT;

CREATE INDEX memory_retrieval_recall_reviews_by_verdict
    ON memory_retrieval_recall_reviews(verdict);

-- A review verdict may only attach to a query that actually fell back (M21-
-- 078 is specifically about the "exact/structured channels return nothing
-- eligible" case); a query that found an eligible candidate is out of this
-- instrument's scope by construction, not something a reviewer overrides.
CREATE TRIGGER memory_retrieval_recall_reviews_requires_fallback
BEFORE INSERT ON memory_retrieval_recall_reviews
WHEN NOT EXISTS (SELECT 1 FROM memory_retrieval_fallbacks WHERE query_id = NEW.query_id)
BEGIN SELECT RAISE(ABORT, 'a deterministic-retrieval-recall review requires the query to have already recorded a fallback'); END;

CREATE TRIGGER memory_retrieval_recall_reviews_immutable_update
BEFORE UPDATE ON memory_retrieval_recall_reviews
BEGIN SELECT RAISE(ABORT, 'memory retrieval recall reviews are immutable'); END;
CREATE TRIGGER memory_retrieval_recall_reviews_immutable_delete
BEFORE DELETE ON memory_retrieval_recall_reviews
BEGIN SELECT RAISE(ABORT, 'memory retrieval recall reviews are immutable'); END;

-- PIPE-016 "Make composition-obligations and control-obligations produce a
-- durable obligation per composition and per path": the obligation as a
-- durable thing rather than a sentence in a stage's detail. Forward-only.
--
-- docs/plan.md §9 makes the proof obligation the unit of assurance. What the
-- flow had was two stages that stated obligations in prose and reported
-- satisfied for having stated them, and two later stages that reported
-- satisfied without reference to anything the first two said. Nothing
-- connected a claim to its discharge, so "this composition is verified" was a
-- sentence rather than a link.
--
-- An obligation is identified by what it is about rather than by when it was
-- raised, so the same claim recurring in a later attempt is the same
-- obligation and can be seen to have been discharged, regressed, or left open.
-- That is what lets a review be reopened rather than expiring with the attempt
-- that produced it.

CREATE TABLE proof_obligations (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 128),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    -- What kind of claim this is. Closed set: a kind nobody can discharge is
    -- an obligation that will sit open for ever and teach a reader to ignore
    -- the list.
    kind TEXT NOT NULL CHECK (kind IN (
        'composition', 'control-path', 'effect', 'review-finding'
    )),
    -- The thing the claim is about: a function name, a composition, a path.
    -- Part of the identity, so the same subject recurring is the same
    -- obligation.
    subject TEXT NOT NULL CHECK (length(subject) BETWEEN 1 AND 256),
    -- What must hold, in words, so a reader can tell what was promised
    -- without reconstructing it from the kind.
    statement_redacted TEXT NOT NULL CHECK (
        length(statement_redacted) BETWEEN 1 AND 2048
    ),
    -- open: raised and not yet answered.
    -- discharged: a later stage established it, and says which.
    -- regressed: it had been discharged and no longer holds.
    state TEXT NOT NULL CHECK (state IN ('open', 'discharged', 'regressed')),
    -- The stage that discharged it, so a discharge can be traced to the check
    -- that performed it rather than asserted.
    discharged_by_stage INTEGER CHECK (
        discharged_by_stage IS NULL OR discharged_by_stage BETWEEN 1 AND 64
    ),
    discharged_detail_redacted TEXT CHECK (
        discharged_detail_redacted IS NULL
        OR length(discharged_detail_redacted) <= 2048
    ),
    raised_at_unix_micros INTEGER NOT NULL CHECK (raised_at_unix_micros >= 0),
    settled_at_unix_micros INTEGER CHECK (
        settled_at_unix_micros IS NULL
        OR settled_at_unix_micros >= raised_at_unix_micros
    ),
    -- A discharged obligation must name what discharged it. Recording the
    -- state without the stage would reproduce the defect this table exists to
    -- repair: a claim with nothing behind it.
    CHECK (
        (state = 'discharged' AND discharged_by_stage IS NOT NULL)
        OR state <> 'discharged'
    ),
    UNIQUE (task_id, attempt, kind, subject)
) STRICT;

CREATE INDEX proof_obligations_by_task_attempt
    ON proof_obligations (task_id, attempt, state);

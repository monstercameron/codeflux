-- AUDIT-011 "Replace caller-supplied approved instruction paths with a
-- durable approval identity": the durable half of M08-049's first-use
-- approval. Forward-only: this migration adds one new table and touches
-- nothing before it.
--
-- M08-049 requires first-use approval before a repository's own instruction
-- file may influence a run. What was built expressed that approval as
-- ContextQuery.ApprovedInstructionPaths, a caller-supplied list of relative
-- paths. Any caller assembling a query could assert that any instruction was
-- approved, and nothing recorded who approved what, against which repository
-- revision, or whether the approval had already been used.
--
-- Repository content is untrusted input. docs/plan.md §27 "Commands, Secrets,
-- and Malicious Repository Content" treats an instruction file as text an
-- attacker may control, so an approval that can be asserted by its consumer
-- is not an approval: it is a prompt-injection path into every later run that
-- reads the same repository.
--
-- The approval is therefore bound to four things, all of which must still
-- hold when it is consumed:
--
--   project and repository -- an approval granted in one project never
--     applies in another, matching the project-boundary predicate every
--     memory, graph, and retrieval query already carries.
--   repository revision    -- an approval is granted against the tree the
--     person actually read.
--   content digest         -- the exact bytes approved. A file edited after
--     approval is a different instruction and needs a new one, which is what
--     makes the approval resistant to a later commit rewriting it.
--   consumption            -- recorded, so a grant can be audited and a
--     single-use scope can refuse a replay.
--
-- Immutability follows the repository's existing rule for historical records:
-- a grant is never edited. Revocation is a later row, and consumption is
-- recorded in its own table rather than by mutating the grant.

CREATE TABLE repository_instruction_approvals (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 64),
    project_id TEXT NOT NULL REFERENCES projects(id),
    repository_id TEXT NOT NULL,
    -- The revision the approver actually read. Forty hexadecimal characters,
    -- matching every other Git revision column in this schema.
    repository_revision TEXT NOT NULL CHECK (length(repository_revision) = 40),
    -- Repository-relative, forward-slashed, and never absolute: an approval
    -- naming an absolute path would be an approval for a file outside the
    -- repository it is bound to.
    instruction_path TEXT NOT NULL CHECK (
        length(instruction_path) BETWEEN 1 AND 512
        AND instruction_path NOT LIKE '/%'
        AND instruction_path NOT LIKE '%..%'
        AND instruction_path NOT LIKE '%\%'
    ),
    -- The exact approved bytes. A changed file is a different instruction.
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64),
    -- 'single-use' expires on first consumption; 'revision' lasts as long as
    -- the repository stays at the approved revision with the approved bytes.
    -- There is deliberately no 'always' scope: an approval that outlives the
    -- content it approved is the thing this table exists to prevent.
    scope TEXT NOT NULL CHECK (scope IN ('single-use', 'revision')),
    granted_at_unix_micros INTEGER NOT NULL CHECK (granted_at_unix_micros >= 0),
    -- Redacted, bounded, and optional: why the person allowed it.
    reason_redacted TEXT CHECK (reason_redacted IS NULL OR length(reason_redacted) <= 2048),
    revoked_at_unix_micros INTEGER CHECK (
        revoked_at_unix_micros IS NULL OR revoked_at_unix_micros >= granted_at_unix_micros
    ),
    FOREIGN KEY (project_id, repository_id) REFERENCES repositories(project_id, id),
    -- One live grant per (project, repository, revision, path, digest). A
    -- second grant for the same four facts is the same decision, not a new
    -- one, and allowing duplicates would make consumption counting ambiguous.
    UNIQUE (project_id, repository_id, repository_revision, instruction_path, content_sha256)
) STRICT;

CREATE INDEX repository_instruction_approvals_lookup
    ON repository_instruction_approvals (
        project_id, repository_id, repository_revision, instruction_path
    );

CREATE TABLE repository_instruction_approval_consumptions (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 8 AND 64),
    approval_id TEXT NOT NULL REFERENCES repository_instruction_approvals(id),
    -- The context manifest that consumed it, so a run's inputs can be traced
    -- back to the exact approval that admitted each instruction.
    consumed_by TEXT NOT NULL CHECK (length(consumed_by) BETWEEN 1 AND 128),
    consumed_at_unix_micros INTEGER NOT NULL CHECK (consumed_at_unix_micros >= 0)
) STRICT;

CREATE INDEX repository_instruction_approval_consumptions_by_approval
    ON repository_instruction_approval_consumptions (approval_id);

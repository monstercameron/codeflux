-- PIPE-013 "Record a non-functional baseline on first run and compare later
-- runs against it": the durable half. Forward-only; adds one table.
--
-- The non-functional stage compared the suite's duration against a fixed
-- sixty-second budget. A fixed number measures the machine, not the change: on
-- a fast host every program passes however much slower it just became, and on
-- a slow one a correct program fails for being run somewhere modest. Neither
-- answer is about the work.
--
-- A baseline is per repository rather than per task, because the question the
-- stage exists to answer is whether this change made this repository's suite
-- slower, and a task-scoped baseline resets that question on every request.
--
-- The revision is recorded but not part of the key. A baseline is a rolling
-- measurement of the same suite, so a new one supersedes the old rather than
-- accumulating a row per commit; the revision says which commit produced the
-- number currently held.

CREATE TABLE non_functional_baselines (
    repository_id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    -- The measurement itself, in milliseconds. Integer rather than real: a
    -- duration compared with a tolerance does not need sub-millisecond
    -- precision, and an exact integer cannot drift when it is read back.
    elapsed_millis INTEGER NOT NULL CHECK (elapsed_millis >= 0),
    -- The commit the measurement was taken at, so a reader can tell whether
    -- the baseline predates the change being judged.
    repository_revision TEXT NOT NULL CHECK (length(repository_revision) = 40),
    -- Which host measured it. A baseline recorded on one machine and compared
    -- on another is a comparison between machines, and saying so is the
    -- difference between a usable number and a misleading one.
    host_platform TEXT NOT NULL CHECK (length(host_platform) BETWEEN 1 AND 64),
    recorded_at_unix_micros INTEGER NOT NULL CHECK (recorded_at_unix_micros >= 0),
    FOREIGN KEY (project_id, repository_id) REFERENCES repositories(project_id, id)
) STRICT;

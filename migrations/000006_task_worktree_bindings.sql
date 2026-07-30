-- Bind each task worktree to its durable task and expected Codeflux-owned HEAD.
-- Columns remain nullable for compatibility with pre-M09 rows; all new storage
-- operations require and populate them.

ALTER TABLE worktree_bindings
    ADD COLUMN task_id TEXT REFERENCES tasks(id);

ALTER TABLE worktree_bindings
    ADD COLUMN head_revision TEXT CHECK (
        head_revision IS NULL
        OR (
            length(head_revision) IN (40, 64)
            AND head_revision NOT GLOB '*[^0-9a-f]*'
        )
    );

CREATE UNIQUE INDEX worktree_bindings_by_task
    ON worktree_bindings(task_id)
    WHERE task_id IS NOT NULL;

CREATE TRIGGER worktree_bindings_require_task_on_insert
BEFORE INSERT ON worktree_bindings
WHEN NEW.task_id IS NULL OR NEW.head_revision IS NULL
BEGIN
    SELECT RAISE(ABORT, 'new worktree bindings require task and head revisions');
END;

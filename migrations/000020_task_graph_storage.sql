CREATE UNIQUE INDEX repositories_by_project_identity
    ON repositories(project_id, id);

CREATE UNIQUE INDEX threads_by_project_repository_identity
    ON threads(project_id, repository_id, id);

CREATE UNIQUE INDEX tasks_by_thread_repository_identity
    ON tasks(thread_id, repository_id, id);

CREATE UNIQUE INDEX messages_by_thread_identity
    ON messages(thread_id, id);

CREATE UNIQUE INDEX task_events_by_task_identity
    ON task_events(task_id, id);

CREATE TABLE graphs (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 40 AND 64),
    project_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (id, repository_id),
    UNIQUE (id, project_id, repository_id),
    FOREIGN KEY (project_id, repository_id)
        REFERENCES repositories(project_id, id)
) STRICT;

CREATE TABLE graph_task_bindings (
    graph_id TEXT PRIMARY KEY REFERENCES graphs(id),
    project_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    task_id TEXT NOT NULL UNIQUE,
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (graph_id, task_id),
    UNIQUE (graph_id, task_id, thread_id),
    FOREIGN KEY (graph_id, project_id, repository_id)
        REFERENCES graphs(id, project_id, repository_id),
    FOREIGN KEY (project_id, repository_id, thread_id)
        REFERENCES threads(project_id, repository_id, id),
    FOREIGN KEY (thread_id, repository_id, task_id)
        REFERENCES tasks(thread_id, repository_id, id)
) STRICT;

CREATE TABLE graph_revisions (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 40 AND 64),
    graph_id TEXT NOT NULL REFERENCES graphs(id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    parent_revision_id TEXT,
    graph_schema_version INTEGER NOT NULL CHECK (graph_schema_version = 1),
    metadata_policy TEXT NOT NULL CHECK (
        metadata_policy = 'typed-fields-only'
    ),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (graph_id, id),
    UNIQUE (graph_id, revision),
    FOREIGN KEY (graph_id, parent_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    CHECK (
        (revision = 1 AND parent_revision_id IS NULL)
        OR (revision > 1 AND parent_revision_id IS NOT NULL)
    )
) STRICT;

CREATE TRIGGER graph_revisions_parent_sequence
BEFORE INSERT ON graph_revisions
WHEN NEW.revision > 1 AND NOT EXISTS (
    SELECT 1
    FROM graph_revisions AS parent
    WHERE parent.graph_id = NEW.graph_id
      AND parent.id = NEW.parent_revision_id
      AND parent.revision = NEW.revision - 1
)
BEGIN
    SELECT RAISE(ABORT, 'graph revision parent must be the preceding revision');
END;

CREATE TABLE graph_nodes (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 40 AND 64),
    graph_id TEXT NOT NULL REFERENCES graphs(id),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (graph_id, id)
) STRICT;

CREATE TABLE graph_node_revisions (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    node_class TEXT NOT NULL CHECK (node_class IN (
        'requirement', 'plan-region', 'atom-operation', 'effect',
        'branch-match-merge', 'obligation', 'artifact-result'
    )),
    status TEXT NOT NULL CHECK (status IN (
        'pending', 'active', 'passed', 'warning', 'failed', 'blocked',
        'invalidated'
    )),
    display_name_redacted TEXT NOT NULL CHECK (
        length(display_name_redacted) BETWEEN 1 AND 256
    ),
    contract_purpose_redacted TEXT NOT NULL CHECK (
        length(contract_purpose_redacted) BETWEEN 0 AND 1024
    ),
    tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (graph_revision_id, node_id),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_id, node_id)
        REFERENCES graph_nodes(graph_id, id),
    CHECK (tombstoned = 0 OR status = 'invalidated')
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_node_contract_items (
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    item_kind TEXT NOT NULL CHECK (item_kind IN ('input', 'output', 'effect')),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 31),
    value_redacted TEXT NOT NULL CHECK (
        length(value_redacted) BETWEEN 1 AND 512
    ),
    PRIMARY KEY (graph_revision_id, node_id, item_kind, ordinal),
    UNIQUE (graph_revision_id, node_id, item_kind, value_redacted),
    FOREIGN KEY (graph_revision_id, node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id)
) STRICT, WITHOUT ROWID;

CREATE TRIGGER graph_node_contract_items_require_purpose
BEFORE INSERT ON graph_node_contract_items
WHEN NOT EXISTS (
    SELECT 1
    FROM graph_node_revisions AS node
    WHERE node.graph_revision_id = NEW.graph_revision_id
      AND node.node_id = NEW.node_id
      AND length(node.contract_purpose_redacted) >= 1
)
BEGIN
    SELECT RAISE(ABORT, 'graph contract items require a typed purpose');
END;

CREATE TABLE graph_edges (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 40 AND 64),
    graph_id TEXT NOT NULL REFERENCES graphs(id),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (graph_id, id)
) STRICT;

CREATE TABLE graph_edge_revisions (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    edge_id TEXT NOT NULL,
    edge_class TEXT NOT NULL CHECK (edge_class IN (
        'control', 'data-provenance', 'evidence-dependency', 'retry',
        'reconciliation', 'compensation'
    )),
    source_node_id TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (graph_revision_id, edge_id),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_id, edge_id)
        REFERENCES graph_edges(graph_id, id),
    FOREIGN KEY (graph_revision_id, source_node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id),
    FOREIGN KEY (graph_revision_id, target_node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_plan_bindings (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    UNIQUE (graph_revision_id, task_id, plan_revision),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_id, task_id)
        REFERENCES graph_task_bindings(graph_id, task_id),
    FOREIGN KEY (task_id, plan_revision)
        REFERENCES agent_plan_revisions(task_id, revision)
) STRICT;

CREATE TABLE graph_revision_plan_step_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL,
    step_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
    PRIMARY KEY (graph_revision_id, task_id, plan_revision, step_id),
    UNIQUE (graph_revision_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, task_id, plan_revision)
        REFERENCES graph_plan_bindings(
            graph_revision_id, task_id, plan_revision
        ),
    FOREIGN KEY (task_id, plan_revision, step_id)
        REFERENCES agent_plan_steps(task_id, plan_revision, step_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_node_plan_step_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL,
    step_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
    PRIMARY KEY (
        graph_revision_id, node_id, task_id, plan_revision, step_id
    ),
    UNIQUE (graph_revision_id, node_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id),
    FOREIGN KEY (graph_revision_id, task_id, plan_revision)
        REFERENCES graph_plan_bindings(
            graph_revision_id, task_id, plan_revision
        ),
    FOREIGN KEY (task_id, plan_revision, step_id)
        REFERENCES agent_plan_steps(task_id, plan_revision, step_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_edge_plan_step_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    edge_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    plan_revision INTEGER NOT NULL,
    step_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
    PRIMARY KEY (
        graph_revision_id, edge_id, task_id, plan_revision, step_id
    ),
    UNIQUE (graph_revision_id, edge_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, edge_id)
        REFERENCES graph_edge_revisions(graph_revision_id, edge_id),
    FOREIGN KEY (graph_revision_id, task_id, plan_revision)
        REFERENCES graph_plan_bindings(
            graph_revision_id, task_id, plan_revision
        ),
    FOREIGN KEY (task_id, plan_revision, step_id)
        REFERENCES agent_plan_steps(task_id, plan_revision, step_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_revision_event_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
    PRIMARY KEY (graph_revision_id, event_id),
    UNIQUE (graph_revision_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_id, task_id)
        REFERENCES graph_task_bindings(graph_id, task_id),
    FOREIGN KEY (task_id, event_id)
        REFERENCES task_events(task_id, id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_node_event_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
    PRIMARY KEY (graph_revision_id, node_id, event_id),
    UNIQUE (graph_revision_id, node_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id),
    FOREIGN KEY (graph_id, task_id)
        REFERENCES graph_task_bindings(graph_id, task_id),
    FOREIGN KEY (task_id, event_id)
        REFERENCES task_events(task_id, id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_edge_event_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    edge_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
    PRIMARY KEY (graph_revision_id, edge_id, event_id),
    UNIQUE (graph_revision_id, edge_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, edge_id)
        REFERENCES graph_edge_revisions(graph_revision_id, edge_id),
    FOREIGN KEY (graph_id, task_id)
        REFERENCES graph_task_bindings(graph_id, task_id),
    FOREIGN KEY (task_id, event_id)
        REFERENCES task_events(task_id, id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_message_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    PRIMARY KEY (graph_revision_id, node_id, message_id),
    UNIQUE (graph_revision_id, node_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id),
    FOREIGN KEY (graph_id, task_id, thread_id)
        REFERENCES graph_task_bindings(graph_id, task_id, thread_id),
    FOREIGN KEY (thread_id, message_id)
        REFERENCES messages(thread_id, id)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_source_links (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    repository_revision TEXT NOT NULL CHECK (
        length(repository_revision) IN (40, 64)
        AND repository_revision NOT GLOB '*[^0-9a-f]*'
    ),
    repository_relative_path TEXT NOT NULL CHECK (
        length(repository_relative_path) BETWEEN 1 AND 4096
        AND repository_relative_path NOT LIKE '/%'
        AND instr(repository_relative_path, char(0)) = 0
        AND instr(repository_relative_path, char(92)) = 0
        AND repository_relative_path != '..'
        AND repository_relative_path NOT LIKE '../%'
        AND repository_relative_path NOT LIKE '%/../%'
        AND repository_relative_path NOT LIKE '%/..'
    ),
    start_line INTEGER NOT NULL CHECK (start_line >= 1),
    start_column INTEGER NOT NULL CHECK (start_column >= 1),
    end_line INTEGER NOT NULL CHECK (end_line >= start_line),
    end_column INTEGER NOT NULL CHECK (end_column >= 1),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    PRIMARY KEY (graph_revision_id, node_id, ordinal),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id),
    FOREIGN KEY (graph_id, repository_id)
        REFERENCES graphs(id, repository_id),
    CHECK (end_line > start_line OR end_column >= start_column)
) STRICT, WITHOUT ROWID;

CREATE TABLE graph_revision_seals (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT PRIMARY KEY,
    node_count INTEGER NOT NULL CHECK (node_count >= 0),
    edge_count INTEGER NOT NULL CHECK (edge_count >= 0),
    content_sha256 TEXT NOT NULL CHECK (
        length(content_sha256) = 64
        AND content_sha256 NOT GLOB '*[^0-9a-f]*'
    ),
    sealed_at_unix_micros INTEGER NOT NULL CHECK (
        sealed_at_unix_micros >= 0
    ),
    UNIQUE (graph_id, content_sha256),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id)
) STRICT;

CREATE TRIGGER graph_revision_seals_complete_counts
BEFORE INSERT ON graph_revision_seals
WHEN NEW.node_count != (
        SELECT count(*) FROM graph_node_revisions
        WHERE graph_revision_id = NEW.graph_revision_id
    )
    OR NEW.edge_count != (
        SELECT count(*) FROM graph_edge_revisions
        WHERE graph_revision_id = NEW.graph_revision_id
    )
BEGIN
    SELECT RAISE(ABORT, 'graph revision seal counts differ from its immutable rows');
END;

CREATE TABLE graph_layout_hints (
    graph_id TEXT NOT NULL,
    graph_revision_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    algorithm TEXT NOT NULL CHECK (length(algorithm) BETWEEN 1 AND 128),
    algorithm_version INTEGER NOT NULL CHECK (algorithm_version >= 1),
    x_milli INTEGER NOT NULL CHECK (x_milli BETWEEN -1000000000 AND 1000000000),
    y_milli INTEGER NOT NULL CHECK (y_milli BETWEEN -1000000000 AND 1000000000),
    width_milli INTEGER NOT NULL CHECK (width_milli BETWEEN 1 AND 1000000000),
    height_milli INTEGER NOT NULL CHECK (height_milli BETWEEN 1 AND 1000000000),
    rank INTEGER NOT NULL CHECK (rank >= 0),
    sibling_order INTEGER NOT NULL CHECK (sibling_order >= 0),
    created_at_unix_micros INTEGER NOT NULL CHECK (
        created_at_unix_micros >= 0
    ),
    PRIMARY KEY (
        graph_revision_id, node_id, algorithm, algorithm_version
    ),
    FOREIGN KEY (graph_id, graph_revision_id)
        REFERENCES graph_revisions(graph_id, id),
    FOREIGN KEY (graph_revision_id, node_id)
        REFERENCES graph_node_revisions(graph_revision_id, node_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX graph_task_bindings_task_slice
    ON graph_task_bindings(task_id, graph_id);

CREATE INDEX graph_revisions_latest
    ON graph_revisions(graph_id, revision DESC);

CREATE INDEX graph_revision_seals_by_graph
    ON graph_revision_seals(graph_id, graph_revision_id);

CREATE INDEX graph_node_revisions_task_slice
    ON graph_node_revisions(
        graph_revision_id, tombstoned, node_class, status, node_id
    );

CREATE INDEX graph_node_revisions_lookup
    ON graph_node_revisions(node_id, graph_revision_id);

CREATE INDEX graph_edge_revisions_from_neighbor
    ON graph_edge_revisions(
        graph_revision_id, source_node_id, tombstoned, target_node_id
    );

CREATE INDEX graph_edge_revisions_to_neighbor
    ON graph_edge_revisions(
        graph_revision_id, target_node_id, tombstoned, source_node_id
    );

CREATE INDEX graph_edge_revisions_evidence_cone
    ON graph_edge_revisions(
        graph_revision_id, target_node_id, source_node_id
    )
    WHERE edge_class = 'evidence-dependency' AND tombstoned = 0;

CREATE INDEX graph_message_links_message
    ON graph_message_links(message_id, graph_revision_id, node_id);

CREATE INDEX graph_revision_event_links_event
    ON graph_revision_event_links(event_id, graph_revision_id);

CREATE INDEX graph_node_event_links_event
    ON graph_node_event_links(event_id, graph_revision_id, node_id);

CREATE INDEX graph_edge_event_links_event
    ON graph_edge_event_links(event_id, graph_revision_id, edge_id);

CREATE INDEX graph_source_links_path
    ON graph_source_links(repository_id, repository_revision, repository_relative_path);

CREATE INDEX graph_layout_hints_slice
    ON graph_layout_hints(
        graph_revision_id, algorithm, algorithm_version, rank, sibling_order
    );

CREATE TRIGGER graphs_immutable_update BEFORE UPDATE ON graphs
BEGIN SELECT RAISE(ABORT, 'graphs are immutable'); END;
CREATE TRIGGER graphs_immutable_delete BEFORE DELETE ON graphs
BEGIN SELECT RAISE(ABORT, 'graphs are immutable'); END;

CREATE TRIGGER graph_task_bindings_immutable_update BEFORE UPDATE ON graph_task_bindings
BEGIN SELECT RAISE(ABORT, 'graph task bindings are immutable'); END;
CREATE TRIGGER graph_task_bindings_immutable_delete BEFORE DELETE ON graph_task_bindings
BEGIN SELECT RAISE(ABORT, 'graph task bindings are immutable'); END;

CREATE TRIGGER graph_revisions_immutable_update BEFORE UPDATE ON graph_revisions
BEGIN SELECT RAISE(ABORT, 'graph revisions are immutable'); END;
CREATE TRIGGER graph_revisions_immutable_delete BEFORE DELETE ON graph_revisions
BEGIN SELECT RAISE(ABORT, 'graph revisions are immutable'); END;

CREATE TRIGGER graph_node_revisions_sealed_insert BEFORE INSERT ON graph_node_revisions
WHEN EXISTS (
    SELECT 1 FROM graph_revision_seals
    WHERE graph_revision_id = NEW.graph_revision_id
)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain node rows'); END;

CREATE TRIGGER graph_nodes_immutable_update BEFORE UPDATE ON graph_nodes
BEGIN SELECT RAISE(ABORT, 'graph nodes are immutable'); END;
CREATE TRIGGER graph_nodes_immutable_delete BEFORE DELETE ON graph_nodes
BEGIN SELECT RAISE(ABORT, 'graph nodes are immutable'); END;

CREATE TRIGGER graph_node_revisions_immutable_update BEFORE UPDATE ON graph_node_revisions
BEGIN SELECT RAISE(ABORT, 'graph node revisions are immutable'); END;
CREATE TRIGGER graph_node_revisions_immutable_delete BEFORE DELETE ON graph_node_revisions
BEGIN SELECT RAISE(ABORT, 'graph node revisions are immutable'); END;

CREATE TRIGGER graph_node_contract_items_immutable_update BEFORE UPDATE ON graph_node_contract_items
BEGIN SELECT RAISE(ABORT, 'graph node contract items are immutable'); END;
CREATE TRIGGER graph_node_contract_items_immutable_delete BEFORE DELETE ON graph_node_contract_items
BEGIN SELECT RAISE(ABORT, 'graph node contract items are immutable'); END;

CREATE TRIGGER graph_node_contract_items_sealed_insert BEFORE INSERT ON graph_node_contract_items
WHEN EXISTS (
    SELECT 1 FROM graph_revision_seals
    WHERE graph_revision_id = NEW.graph_revision_id
)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain contract rows'); END;

CREATE TRIGGER graph_edges_immutable_update BEFORE UPDATE ON graph_edges
BEGIN SELECT RAISE(ABORT, 'graph edges are immutable'); END;
CREATE TRIGGER graph_edges_immutable_delete BEFORE DELETE ON graph_edges
BEGIN SELECT RAISE(ABORT, 'graph edges are immutable'); END;

CREATE TRIGGER graph_edge_revisions_immutable_update BEFORE UPDATE ON graph_edge_revisions
BEGIN SELECT RAISE(ABORT, 'graph edge revisions are immutable'); END;
CREATE TRIGGER graph_edge_revisions_immutable_delete BEFORE DELETE ON graph_edge_revisions
BEGIN SELECT RAISE(ABORT, 'graph edge revisions are immutable'); END;

CREATE TRIGGER graph_edge_revisions_sealed_insert BEFORE INSERT ON graph_edge_revisions
WHEN EXISTS (
    SELECT 1 FROM graph_revision_seals
    WHERE graph_revision_id = NEW.graph_revision_id
)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain edge rows'); END;

CREATE TRIGGER graph_plan_bindings_immutable_update BEFORE UPDATE ON graph_plan_bindings
BEGIN SELECT RAISE(ABORT, 'graph plan bindings are immutable'); END;
CREATE TRIGGER graph_plan_bindings_immutable_delete BEFORE DELETE ON graph_plan_bindings
BEGIN SELECT RAISE(ABORT, 'graph plan bindings are immutable'); END;

CREATE TRIGGER graph_plan_bindings_sealed_insert BEFORE INSERT ON graph_plan_bindings
WHEN EXISTS (
    SELECT 1 FROM graph_revision_seals
    WHERE graph_revision_id = NEW.graph_revision_id
)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain plan bindings'); END;

CREATE TRIGGER graph_revision_plan_step_links_immutable_update BEFORE UPDATE ON graph_revision_plan_step_links
BEGIN SELECT RAISE(ABORT, 'graph revision plan step links are immutable'); END;
CREATE TRIGGER graph_revision_plan_step_links_immutable_delete BEFORE DELETE ON graph_revision_plan_step_links
BEGIN SELECT RAISE(ABORT, 'graph revision plan step links are immutable'); END;
CREATE TRIGGER graph_revision_plan_step_links_sealed_insert BEFORE INSERT ON graph_revision_plan_step_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain plan step links'); END;

CREATE TRIGGER graph_node_plan_step_links_immutable_update BEFORE UPDATE ON graph_node_plan_step_links
BEGIN SELECT RAISE(ABORT, 'graph node plan step links are immutable'); END;
CREATE TRIGGER graph_node_plan_step_links_immutable_delete BEFORE DELETE ON graph_node_plan_step_links
BEGIN SELECT RAISE(ABORT, 'graph node plan step links are immutable'); END;
CREATE TRIGGER graph_node_plan_step_links_sealed_insert BEFORE INSERT ON graph_node_plan_step_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain node plan links'); END;

CREATE TRIGGER graph_edge_plan_step_links_immutable_update BEFORE UPDATE ON graph_edge_plan_step_links
BEGIN SELECT RAISE(ABORT, 'graph edge plan step links are immutable'); END;
CREATE TRIGGER graph_edge_plan_step_links_immutable_delete BEFORE DELETE ON graph_edge_plan_step_links
BEGIN SELECT RAISE(ABORT, 'graph edge plan step links are immutable'); END;
CREATE TRIGGER graph_edge_plan_step_links_sealed_insert BEFORE INSERT ON graph_edge_plan_step_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain edge plan links'); END;

CREATE TRIGGER graph_revision_event_links_immutable_update BEFORE UPDATE ON graph_revision_event_links
BEGIN SELECT RAISE(ABORT, 'graph revision event links are immutable'); END;
CREATE TRIGGER graph_revision_event_links_immutable_delete BEFORE DELETE ON graph_revision_event_links
BEGIN SELECT RAISE(ABORT, 'graph revision event links are immutable'); END;
CREATE TRIGGER graph_revision_event_links_sealed_insert BEFORE INSERT ON graph_revision_event_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain event links'); END;

CREATE TRIGGER graph_node_event_links_immutable_update BEFORE UPDATE ON graph_node_event_links
BEGIN SELECT RAISE(ABORT, 'graph node event links are immutable'); END;
CREATE TRIGGER graph_node_event_links_immutable_delete BEFORE DELETE ON graph_node_event_links
BEGIN SELECT RAISE(ABORT, 'graph node event links are immutable'); END;
CREATE TRIGGER graph_node_event_links_sealed_insert BEFORE INSERT ON graph_node_event_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain node event links'); END;

CREATE TRIGGER graph_edge_event_links_immutable_update BEFORE UPDATE ON graph_edge_event_links
BEGIN SELECT RAISE(ABORT, 'graph edge event links are immutable'); END;
CREATE TRIGGER graph_edge_event_links_immutable_delete BEFORE DELETE ON graph_edge_event_links
BEGIN SELECT RAISE(ABORT, 'graph edge event links are immutable'); END;
CREATE TRIGGER graph_edge_event_links_sealed_insert BEFORE INSERT ON graph_edge_event_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain edge event links'); END;

CREATE TRIGGER graph_message_links_immutable_update BEFORE UPDATE ON graph_message_links
BEGIN SELECT RAISE(ABORT, 'graph message links are immutable'); END;
CREATE TRIGGER graph_message_links_immutable_delete BEFORE DELETE ON graph_message_links
BEGIN SELECT RAISE(ABORT, 'graph message links are immutable'); END;
CREATE TRIGGER graph_message_links_sealed_insert BEFORE INSERT ON graph_message_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain message links'); END;

CREATE TRIGGER graph_source_links_immutable_update BEFORE UPDATE ON graph_source_links
BEGIN SELECT RAISE(ABORT, 'graph source links are immutable'); END;
CREATE TRIGGER graph_source_links_immutable_delete BEFORE DELETE ON graph_source_links
BEGIN SELECT RAISE(ABORT, 'graph source links are immutable'); END;
CREATE TRIGGER graph_source_links_sealed_insert BEFORE INSERT ON graph_source_links
WHEN EXISTS (SELECT 1 FROM graph_revision_seals WHERE graph_revision_id = NEW.graph_revision_id)
BEGIN SELECT RAISE(ABORT, 'sealed graph revisions cannot gain source links'); END;

CREATE TRIGGER graph_revision_seals_immutable_update BEFORE UPDATE ON graph_revision_seals
BEGIN SELECT RAISE(ABORT, 'graph revision seals are immutable'); END;
CREATE TRIGGER graph_revision_seals_immutable_delete BEFORE DELETE ON graph_revision_seals
BEGIN SELECT RAISE(ABORT, 'graph revision seals are immutable'); END;

CREATE TRIGGER graph_layout_hints_immutable_update BEFORE UPDATE ON graph_layout_hints
BEGIN SELECT RAISE(ABORT, 'graph layout hints are immutable'); END;
CREATE TRIGGER graph_layout_hints_immutable_delete BEFORE DELETE ON graph_layout_hints
BEGIN SELECT RAISE(ABORT, 'graph layout hints are immutable'); END;

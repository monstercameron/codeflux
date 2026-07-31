-- Every durable thread owns one authoritative ordered session stream. Legacy
-- rows receive deterministic session identities derived from their UUIDv7.

INSERT INTO sessions (
    id, thread_id, current_sequence, compacted_through_sequence,
    created_at_unix_micros, updated_at_unix_micros
)
SELECT 'ses_' || substr(threads.id, 5), threads.id, 0, 0,
       threads.created_at_unix_micros, threads.created_at_unix_micros
FROM threads
WHERE NOT EXISTS (
    SELECT 1 FROM sessions WHERE sessions.thread_id = threads.id
);

-- A zero-sequence legacy session has no observable history, so its initial
-- projection can be installed at sequence one without reordering prior facts.
UPDATE sessions
SET current_sequence = 1
WHERE current_sequence = 0
  AND NOT EXISTS (
      SELECT 1 FROM session_events
      WHERE session_events.session_id = sessions.id
  );

INSERT INTO session_events (
    session_id, sequence, thread_id, timestamp_unix_micros, kind,
    entity_revision, payload_version, payload_json,
    delivery_class, correctness_bearing
)
SELECT sessions.id, 1, threads.id, threads.created_at_unix_micros,
       'thread-created', 0, 1,
       json_object(
           'thread_created', json_object(
               'workspace_id', threads.workspace_id,
               'title', threads.title,
               'archived', json(
                   CASE WHEN threads.archived_at_unix_micros IS NULL
                        THEN 'false' ELSE 'true' END
               )
           )
       ),
       'material', 1
FROM sessions
JOIN threads ON threads.id = sessions.thread_id
WHERE sessions.current_sequence = 1
  AND NOT EXISTS (
      SELECT 1 FROM session_events
      WHERE session_events.session_id = sessions.id
  );

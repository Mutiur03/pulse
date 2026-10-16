-- Identity stitching: maps a distinct_id to the person key (the
-- identified user id) it belongs to. Written by the stream processor for
-- $identify (anonymous id -> user id) and $create_alias (alias -> id).
-- A distinct_id without a row is its own person. The latest row wins.
CREATE TABLE IF NOT EXISTS person_aliases
(
    project_id  UInt64,
    distinct_id String,
    person_key  String,
    updated_at  DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (project_id, distinct_id);

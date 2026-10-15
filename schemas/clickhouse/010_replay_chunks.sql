-- One row per session replay chunk. The rrweb events live in object storage
-- under chunk_key; sessions are summarised from these rows at query time.
-- Retention: rows expire after 30 days (object storage needs a matching
-- lifecycle rule on the bucket).
CREATE TABLE IF NOT EXISTS replay_chunks
(
    project_id  UInt64,
    session_id  String,
    chunk_key   String,
    start       DateTime64(3, 'UTC'),
    end         DateTime64(3, 'UTC'),
    distinct_id String DEFAULT '',
    events      UInt32 DEFAULT 0,
    pages       UInt16 DEFAULT 0,
    errors      UInt16 DEFAULT 0,
    urls        Array(String),
    trace_ids   Array(String),
    bytes       UInt32 DEFAULT 0,
    received_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(start)
ORDER BY (project_id, session_id, start)
TTL toDateTime(start) + INTERVAL 30 DAY;

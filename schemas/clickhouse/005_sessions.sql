-- Sessions table
CREATE TABLE IF NOT EXISTS sessions
(
    project_id       UInt64,
    session_id       String,
    person_id        UUID DEFAULT toUUID('00000000-0000-0000-0000-000000000000'),
    distinct_id      String DEFAULT '',
    started_at       DateTime64(3, 'UTC') DEFAULT now64(3),
    ended_at         DateTime64(3, 'UTC') DEFAULT now64(3),
    pageviews        UInt32 DEFAULT 0,
    events           UInt32 DEFAULT 0,
    errors           UInt32 DEFAULT 0,
    rage_clicks      UInt32 DEFAULT 0,
    entry_url        String DEFAULT '',
    exit_url         String DEFAULT '',
    has_replay       UInt8 DEFAULT 0,
    country          LowCardinality(String) DEFAULT '',
    browser          LowCardinality(String) DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(started_at)
ORDER BY (project_id, toDate(started_at), session_id)
TTL toDateTime(started_at) + INTERVAL 25 MONTH;

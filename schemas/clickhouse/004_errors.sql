-- Errors table
CREATE TABLE IF NOT EXISTS errors
(
    project_id     UInt64,
    event_id       UUID DEFAULT generateUUIDv4(),
    timestamp      DateTime64(3, 'UTC') DEFAULT now64(3),
    issue_id       UInt64 DEFAULT 0,
    fingerprint    String DEFAULT '',
    platform       LowCardinality(String) DEFAULT '',
    exception_type LowCardinality(String) DEFAULT '',
    message        String DEFAULT '',
    release        LowCardinality(String) DEFAULT '',
    environment    LowCardinality(String) DEFAULT '',
    trace_id       String DEFAULT '',
    span_id        String DEFAULT '',
    session_id     String DEFAULT '',
    person_id      UUID DEFAULT toUUID('00000000-0000-0000-0000-000000000000'),
    url            String DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (project_id, issue_id, timestamp)
TTL toDateTime(timestamp) + INTERVAL 90 DAY;

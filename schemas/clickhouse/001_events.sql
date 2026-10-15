-- Events table
CREATE TABLE IF NOT EXISTS events
(
    project_id      UInt64,
    uuid            UUID DEFAULT generateUUIDv4(),
    event           LowCardinality(String),
    timestamp       DateTime64(3, 'UTC') DEFAULT now64(3),
    distinct_id     String DEFAULT '',
    person_id       UUID DEFAULT toUUID('00000000-0000-0000-0000-000000000000'),
    session_id      String DEFAULT '',
    trace_id        String DEFAULT '',
    span_id         String DEFAULT '',
    properties      String DEFAULT '{}',
    current_url     String DEFAULT '',
    elements_chain  String DEFAULT '',
    release         LowCardinality(String) DEFAULT '',
    country         LowCardinality(String) DEFAULT '',
    browser         LowCardinality(String) DEFAULT '',
    os              LowCardinality(String) DEFAULT '',
    inserted_at     DateTime DEFAULT now()
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (project_id, event, toDate(timestamp), uuid)
TTL toDateTime(timestamp) + INTERVAL 30 DAY;

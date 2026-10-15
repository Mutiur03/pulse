-- Spans table
CREATE TABLE IF NOT EXISTS spans
(
    project_id      UInt64,
    timestamp       DateTime64(9, 'UTC') DEFAULT now64(9),
    trace_id        String,
    span_id         String,
    parent_span_id  String DEFAULT '',
    span_name       LowCardinality(String),
    span_kind       LowCardinality(String) DEFAULT '',
    service_name    LowCardinality(String),
    duration_ns     UInt64 DEFAULT 0,
    status_code     LowCardinality(String) DEFAULT '',
    status_message  String DEFAULT '',
    http_route      LowCardinality(String) DEFAULT '',
    http_status     UInt16 DEFAULT 0,
    db_system       LowCardinality(String) DEFAULT '',
    db_statement    String DEFAULT '',
    session_id      String DEFAULT '',
    person_id       UUID DEFAULT toUUID('00000000-0000-0000-0000-000000000000')
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (project_id, service_name, span_name, toStartOfHour(timestamp), timestamp)
TTL toDateTime(timestamp) + INTERVAL 7 DAY;

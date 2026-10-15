-- Logs table
CREATE TABLE IF NOT EXISTS logs
(
    project_id     UInt64,
    timestamp      DateTime64(9, 'UTC') DEFAULT now64(9),
    trace_id       String DEFAULT '',
    span_id        String DEFAULT '',
    severity       LowCardinality(String) DEFAULT '',
    service_name   LowCardinality(String) DEFAULT '',
    body           String DEFAULT '',
    attributes     String DEFAULT '{}'
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (project_id, service_name, severity, toStartOfHour(timestamp), timestamp)
TTL toDateTime(timestamp) + INTERVAL 3 DAY;

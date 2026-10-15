-- OTLP metric data points, one row per point.
CREATE TABLE IF NOT EXISTS metrics
(
    project_id   UInt64,
    timestamp    DateTime64(9, 'UTC'),
    service_name LowCardinality(String) DEFAULT '',
    name         LowCardinality(String),
    description  String DEFAULT '',
    unit         LowCardinality(String) DEFAULT '',
    type         LowCardinality(String),
    temporality  LowCardinality(String) DEFAULT '',
    monotonic    Bool DEFAULT false,
    value        Float64,
    count        UInt64 DEFAULT 0,
    sum          Float64 DEFAULT 0,
    min          Float64 DEFAULT 0,
    max          Float64 DEFAULT 0,
    bounds       Array(Float64),
    buckets      Array(UInt64),
    attributes   Map(LowCardinality(String), String)
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (project_id, name, service_name, timestamp)
TTL toDateTime(timestamp) + INTERVAL 30 DAY;

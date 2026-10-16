-- One pre-aggregated row per trace and hour, kept up to date by a
-- materialized view on spans. The trace list reads this instead of grouping
-- every span in the range. A trace that crosses an hour boundary has a row
-- for each hour; the query merges them.
CREATE TABLE IF NOT EXISTS trace_summaries
(
    project_id   UInt64,
    hour         DateTime('UTC'),
    trace_id     String,
    root         AggregateFunction(argMin, Tuple(String, String, UInt16), Tuple(UInt8, DateTime64(9, 'UTC'))),
    services     SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
    span_names   SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
    start        SimpleAggregateFunction(min, DateTime64(9, 'UTC')),
    end_ns       SimpleAggregateFunction(max, Int64),
    span_count   SimpleAggregateFunction(sum, UInt64),
    error_count  SimpleAggregateFunction(sum, UInt64),
    browser_spans SimpleAggregateFunction(sum, UInt64),
    INDEX idx_trace trace_id TYPE bloom_filter(0.01) GRANULARITY 4
)
ENGINE = AggregatingMergeTree()
PARTITION BY toDate(hour)
ORDER BY (project_id, hour, trace_id)
TTL hour + INTERVAL 7 DAY
SETTINGS ttl_only_drop_parts = 1;

-- Fill the table from existing spans the first time only (it is empty then).
-- Spans that arrive between this insert and the view below are not counted;
-- that gap only exists once, while the migration runs.
INSERT INTO trace_summaries
SELECT project_id, toStartOfHour(timestamp) AS hour, trace_id,
    argMinState((span_name, service_name, http_status), (toUInt8(parent_span_id != ''), timestamp)),
    groupUniqArray(toString(service_name)), groupUniqArray(200)(toString(span_name)),
    min(timestamp), max(toInt64(toUnixTimestamp64Nano(timestamp)) + toInt64(duration_ns)),
    count(), countIf(status_code = 'error'), countIf(platform = 'webjs')
FROM spans
WHERE (SELECT count() FROM trace_summaries) = 0
  AND NOT EXISTS (SELECT 1 FROM system.tables WHERE database = currentDatabase() AND name = 'trace_summaries_mv')
GROUP BY project_id, hour, trace_id;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_summaries_mv TO trace_summaries AS
SELECT project_id, toStartOfHour(timestamp) AS hour, trace_id,
    argMinState((span_name, service_name, http_status), (toUInt8(parent_span_id != ''), timestamp)) AS root,
    groupUniqArray(toString(service_name)) AS services, groupUniqArray(200)(toString(span_name)) AS span_names,
    min(timestamp) AS start, max(toInt64(toUnixTimestamp64Nano(timestamp)) + toInt64(duration_ns)) AS end_ns,
    count() AS span_count, countIf(status_code = 'error') AS error_count, countIf(platform = 'webjs') AS browser_spans
FROM spans
GROUP BY project_id, hour, trace_id;

-- Skip indexes for lookups by id. (Projections would conflict with the
-- lightweight deletes used for data governance.)
ALTER TABLE spans ADD INDEX IF NOT EXISTS idx_trace_id trace_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE spans ADD INDEX IF NOT EXISTS idx_session_id session_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE events ADD INDEX IF NOT EXISTS idx_distinct_id distinct_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE events ADD INDEX IF NOT EXISTS idx_session_id session_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE events ADD INDEX IF NOT EXISTS idx_timestamp timestamp TYPE minmax GRANULARITY 1;

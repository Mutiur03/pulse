# 3. ClickHouse for events, traces, logs, errors and metrics

Date: 2026-10-16. Status: accepted.

## Context

Product analytics (funnels, retention, paths, trends) and observability
(trace search, log search, error aggregation, metrics) both scan large time
ranges with filters and group-bys. A single store for all signals is what
lets Pulse join them by `trace_id`, `session_id` and person.

## Decision

Store every telemetry signal in ClickHouse MergeTree tables partitioned by
day, ordered by project first, with a table TTL per signal (events 30 days,
spans 7, logs 3, errors 90, replay index 30, metrics 30, sessions 25
months). Identity links live in a ReplacingMergeTree (`person_aliases`) and
queries resolve persons with a join. Funnels use `windowFunnel`, retention
and paths use array functions. Per-project retention shorter than the TTL is
enforced by the governance worker with lightweight deletes;
`ttl_only_drop_parts` drops whole expired parts.

## Consequences

- One query engine and SQL dialect for all features, with 10x+ compression.
- No transactions or cheap updates: deletion requests and retention use
  lightweight deletes, which are eventually applied by merges.
- Query cost grows with the scanned range. The load test showed the trace
  list (a group-by over all spans in the range) taking seconds at about 18M
  spans per day; a pre-aggregated trace summary table is the planned fix.
- Managed options exist (ClickHouse Cloud) and the client supports TLS and
  credentials (`CLICKHOUSE_SECURE`, `CLICKHOUSE_USER`, `CLICKHOUSE_PASSWORD`).

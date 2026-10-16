# 13. Pre-aggregated trace summaries and query limits

Date: 2026-10-16

## Status

Accepted

## Context

With about 18.8M events and 18.8M spans in one day, list and chart reads
took seconds (trace list p95 11.8 s and funnel 54 s at 5 users; at 20
users the trace list hit the 60 s API timeout). The trace list grouped
every span in the range by trace. The funnel read the events table about
eight times, because ClickHouse inlines a CTE every time it is referenced.
A single expensive query could also hold every core with no bound on its
time or size.

Options considered for the trace list: a projection on `spans` ordered by
time; a separate root-span table written by the stream processor; or an
aggregating table filled by a materialized view.

## Decision

- `trace_summaries`, an `AggregatingMergeTree` filled by a materialized
  view on `spans`, holds one row per trace and hour: the root span (as an
  `argMin` state), services, span names, start, end, span and error
  counts. The trace list merges these rows. The migration backfills it
  once, when the table is empty and the view does not exist yet.
- No projections on `events` or `spans`. They conflict with the
  lightweight `DELETE` used by retention and deletion requests (ADR 0010).
  Bloom-filter skip indexes serve lookups by id instead.
- The funnel computes levels and per-step health in one scan, from
  per-person aggregate states merged by level.
- Trends and funnels use the ClickHouse query cache (60 s) with
  minute-aligned ranges.
- Every query from the query API has `max_execution_time`,
  `max_rows_to_read` and `max_memory_usage`. Funnels run with fewer
  threads. Each API replica limits how many analytical requests of one
  organization run at once. Limit errors map to clear 504, 422 and 429
  responses that the UI explains.

## Consequences

- Trace list p95 at 5 users went from 11.8 s to under 1 s. Funnels went from
  54 s to under 5 s (docs/load-testing.md).
- Trace data is stored twice, as spans and summaries. The summaries are
  much smaller (one row per trace and hour) and share the 7-day TTL.
  Governance deletes from both tables.
- Spans that arrive while the migration backfills, before the view
  exists, are missing from the summaries. This only happens once, at the
  upgrade that adds the table.
- Cached charts can be up to a minute stale, including just after a
  deletion.
- The concurrency limit is per replica, not global. A shared limit would
  need Redis.

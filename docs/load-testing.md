# Load testing

k6 scripts live in `load/`:

- `ingest.js` posts product events and spans to the ingest gateway at a
  constant request rate. Half the requests are events and half are spans.
  Each request carries `BATCH` records.
- `query.js` runs dashboard-style reads with several logged-in virtual users:
  the trace list, the event list, a unique-users trend and a two-step funnel.
- `run.sh` measures end-to-end throughput. It runs `ingest.js`, then waits
  until the stream processor has written every accepted record to
  ClickHouse, using `pulse_processor_rows_written_total`. It prints both
  rates.

```bash
cd deploy/compose && docker compose --profile demo up -d --build --wait
RATE=1000 BATCH=50 DURATION=60s load/run.sh   # needs k6, docker, curl and python3
k6 run -e VUS=20 -e DURATION=60s load/query.js
```

`run.sh` adds an ingest key with a high rate limit to the local demo
project, so the per-key limit of 500 requests/s does not cap the test
(`load/setup-key.sql`). Do not run it against a production database.

## Results on the development box

The box is one VM with 8 vCPUs and 16 GB RAM. It runs the whole Compose
stack (single-broker Kafka, single-node ClickHouse, Postgres, Redis and
every Pulse service) and k6 itself. The Go services are at their defaults:
one replica each. Measured on 2026-10-16. Each run lasted 60 seconds, and
the records are product events and spans of about 300 to 400 bytes each.

### Ingest, end to end

| Target | Accepted by the gateway | Ingest p95 | Written to ClickHouse | Backlog after sending stopped |
| --- | --- | --- | --- | --- |
| 25,000 records/s (500 req/s × 50) | 24,579 records/s | 6.4 ms | 24,553 records/s | none |
| 50,000 records/s (1,000 req/s × 50) | 48,928 records/s | 9.3 ms | 48,880 records/s | none |
| 100,000 records/s (2,000 req/s × 50) | 97,921 records/s | 36.2 ms | 97,806 records/s | none |
| 150,000 records/s (3,000 req/s × 50) | 138,024 records/s | 315.6 ms | 101,063 records/s | drained in 22 s |

No request failed in any run.

- **Sustained capacity on this box is about 100,000 records/s end to end.**
- Above that, the gateway still accepts about 140,000 records/s into Kafka.
  The stream processor and ClickHouse write about 100,000/s, and Kafka
  absorbs the difference as consumer lag (ADR 0002).
- k6 shares the CPUs, so the gateway's own ceiling is higher.

One finding from these tests is fixed in the same change. The gateway used
to write a request's records to Kafka one at a time, waiting for each. At
200 requests/s × 50 records, p95 latency was 448 ms. Producing each
request's records together brought it to 4.3 ms at the same rate.

A longer run on 2026-10-16 (2,000 requests/s × 50 records for 380 s, 37.6M
records) accepted 98,681 records/s and stored 97,884 records/s. The
backlog drained 3 s after sending stopped. Ingest p95 rose to 156 ms
because k6 ran out of its 400 virtual users. That run also produced the
dataset for the large-data query tests below.

### Query API

Each virtual user runs the trace list, the event list, a unique-users trend
(hourly) and a two-step funnel over the last 24 hours, then waits one
second.

```bash
k6 run -e VUS=5 load/query.js                  # repeated dashboards (cache hits)
k6 run -e VUS=5 -e VARY_RANGE=1 load/query.js  # every request computed
```

With the demo data (about 130 events and 40 spans), 20 users got p95 of
29 ms (traces), 16 ms (events), 43 ms (trend) and 238 ms (funnel). That is
67 requests/s with no errors.

#### Large data: 18.8M events and 18.8M spans in the last 24 hours

The data comes from the ingest run above. It has 100,000 users, about
376,000 traces and 376,000 sessions in one project. Before is the code
before the query work below. After is the current code. "Computed" uses
`VARY_RANGE=1`, so no request is answered from the cache. All values are
p95.

| 5 virtual users | Events | Trend | Traces | Funnel | Errors |
| --- | --- | --- | --- | --- | --- |
| Target | < 1 s | < 1 s | < 1.5 s | < 5 s | |
| Before | 4.0 s | 2.5 s | 11.8 s | 54.3 s | 0 |
| After, computed (run 1) | 0.38 s | 0.90 s | 0.72 s | 4.4 s | 0 |
| After, computed (run 2) | 0.37 s | 0.78 s | 0.65 s | 3.9 s | 0 |
| After, repeated dashboards | 0.29 s | 0.02 s | 0.59 s | 2.1 s | 0 |

| 20 virtual users | Events | Trend | Traces | Funnel | Errors |
| --- | --- | --- | --- | --- | --- |
| Before | — | — | timed out (60 s) | — | |
| After, computed | 5.8 s | 7.8 s | 7.5 s | 14.8 s | 1.1% (funnels rejected with 429) |
| After, repeated dashboards | 1.9 s | 0.8 s | 2.4 s | 7.2 s | 0.3% (429) |

A single user (computed) gets 63 ms (events), 0.24 s (trend), 0.23 s
(traces) and 3.2 s (funnel).

At 20 users the box is CPU-bound. The per-organization limit queues the
extra requests, and funnels that wait more than 10 s for a slot get a 429
instead of piling up. No request timed out.

#### What changed

- **Trace list.** A materialized view keeps `trace_summaries`, one
  pre-aggregated row per trace and hour (ADR 0013). The list merges about
  376,000 rows instead of grouping 18.8M spans. When the last two hours
  already hold a full page, older hours are not read.
- **Event list.** A subquery finds the timestamp of the 100th newest event
  by reading only the timestamp column. The wide `properties` column is then
  read only for those rows.
- **Trend.** The buckets and the totals come from one scan (`GROUPING
  SETS`). The few aliased ids are mapped to persons with `transform()`
  instead of joining every event to `person_aliases`.
- **Funnel.** It used to read events about 8 times, because ClickHouse
  inlines CTEs on every reference. Now one scan computes both the levels
  and the per-step health from per-person states. A second scan for top
  errors runs only when the range has errors tied to sessions.
- **Cache.** Trends and funnels use the ClickHouse query cache for 60 s.
  Their ranges are aligned to whole minutes, so everyone opening the same
  chart within a minute shares one computation. Lists are never cached.
- **Skip indexes.** Bloom filters on `trace_id`/`session_id` (spans) and
  `distinct_id`/`session_id` (events) serve lookups by id. Projections
  were ruled out because they conflict with the lightweight deletes used by
  data governance.

#### Query limits

The query API caps every ClickHouse read. The defaults suit an 8 vCPU
server:

| Variable | Default | Effect |
| --- | --- | --- |
| `QUERY_MAX_EXECUTION_SECONDS` | 30 | `max_execution_time`. The API answers 504 and the UI shows "Query timed out" with a retry button. |
| `QUERY_MAX_ROWS_TO_READ` | 2,000,000,000 | `max_rows_to_read`. Answers 422 ("Query too large"). |
| `QUERY_MAX_MEMORY_MB` | 4096 | `max_memory_usage` per query. Answers 422. |
| `QUERY_MAX_CONCURRENT_PER_ORG` | 8 | Analytical requests per organization running at once in one API replica. Others wait. |
| `QUERY_QUEUE_WAIT_SECONDS` | 10 | How long a request waits for a slot before a 429. |
| `QUERY_HEAVY_MAX_THREADS` | 3 | Threads per funnel query, so funnels leave cores for lists and charts. 2 and 4 were also measured: 3 kept every read under target. |

The concurrency limit is per replica. With several replicas, an
organization can run that many requests on each.

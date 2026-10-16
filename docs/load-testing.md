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

### Query API

20 virtual users, each running the four reads and then waiting one second:

| Data in the last 24 hours | Trace list p95 | Event list p95 | Trend p95 | Funnel p95 | Throughput |
| --- | --- | --- | --- | --- | --- |
| Demo data (about 130 events, 40 spans) | 29 ms | 16 ms | 43 ms | 238 ms | 67 req/s, no errors |

The load runs above left about 18.6M events and 18.5M spans in the demo
project. With that data, even 5 virtual users were slow:

| Read | p95 |
| --- | --- |
| Event list | 4.0 s |
| Trend | 3.4 s |
| Trace list | 10.2 s |
| Funnel | 43 s |

With 20 users, trace list requests hit the 60-second API timeout.

The trace list groups every span in the range by trace. The funnel joins
person aliases for about 100,000 synthetic users. Both scale with the rows
in the range. Known follow-ups:

- A pre-aggregated trace summary table.
- Ordering the event list by time within the primary key.
- A query cost limit.

Until then, keep dashboards on shorter ranges for very high-volume projects.

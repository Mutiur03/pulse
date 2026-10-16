# 2. Kafka between ingest and storage

Date: 2026-10-16. Status: accepted.

## Context

Telemetry arrives in bursts from browsers and services we do not control.
ClickHouse prefers large, infrequent batch inserts, can be slow or down
during maintenance, and must never cause the ingest endpoint to reject data.
Several consumers need the same stream (storage, error grouping, alerting).

## Decision

The ingest gateway validates, scrubs and enriches each request, then writes
the records to Kafka topics (`raw.events`, `raw.spans`, `raw.errors`,
`raw.logs`, `raw.metrics`; `issue.events` downstream). It answers only after
Kafka acknowledged every record of the request; the records of a request are
produced together in one call. The stream processor consumes the raw topics
in a consumer group, writes batches to ClickHouse and commits offsets only
after a successful write. We use franz-go as the client and Apache Kafka in
KRaft mode (no ZooKeeper); managed Kafka (MSK, Confluent, Aiven) works the
same way with TLS and SASL/SCRAM settings.

## Consequences

- Ingest latency and availability depend on Kafka only. ClickHouse outages
  turn into consumer lag, not data loss, for as long as topic retention.
- Delivery is at least once: a crash between the ClickHouse write and the
  offset commit replays a batch. Tables tolerate duplicates
  (ReplacingMergeTree where identity matters), and dashboards count with
  uniq functions where it matters.
- One more stateful system to run. Locally it is a single broker; in
  production a managed service is expected (see ADR 0012).
- Measured on one 8-core box (docs/load-testing.md): about 98k records/s
  end to end with no backlog, limited by the box rather than Kafka.

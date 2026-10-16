# 11. Prometheus metrics for self-monitoring

Date: 2026-10-16. Status: accepted.

## Context

Operators need to see ingest rate, rejections, Kafka write latency,
processing delay, ClickHouse write errors and query latency, including when
Pulse itself is unhealthy, so Pulse cannot be the only place to look.

## Decision

Every Go service exposes Prometheus metrics (`pulse_*` plus Go runtime and
process metrics) at `/metrics`: on the health port for workers and on a
separate metrics port for the ingest gateway (9101) and query API (9102), so
metrics are never on public ports. HTTP metrics use route patterns, not raw
paths, to keep cardinality bounded. We ship a Prometheus config, alert
rules and a Grafana dashboard (`deploy/monitoring`), a Compose profile to run
them, and pod annotations or ServiceMonitors in the Helm chart.

## Consequences

- Works with any Prometheus-compatible stack and needs no Pulse dependency.
- Dogfooding through Pulse's own OpenTelemetry metrics remains possible but
  is not the primary path.

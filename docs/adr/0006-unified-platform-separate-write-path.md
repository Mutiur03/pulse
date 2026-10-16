# 6. One product and data model, with the write path deployed separately

Date: 2026-10-16. Status: accepted.

## Context

Pulse's value is one place for analytics and observability with shared ids.
That argues for few services and one data model. On the other hand the
public ingest endpoint has different traffic, security exposure and
availability needs from the authenticated API and UI.

## Decision

Keep an "all-in-one" product: one web app, one query API, one ClickHouse
schema with shared correlation ids, and processing stages inside one stream
processor rather than separate microservices. Deploy the write path
separately: ingest gateway, stream processor, query API, alerting worker,
governance worker and web are separate containers in Compose and separate
Deployments in the Helm chart, so ingest scales and fails independently.
Metrics are served on a separate port from the public ingest and API ports.

## Consequences

- Six small deployables instead of dozens; cross-signal features need no
  service-to-service calls.
- An ingest flood cannot starve dashboards, and a slow query cannot drop
  telemetry (Kafka buffers in between, ADR 0002).
- The design's single combined binary for tiny installs is not built yet;
  small installs run the Compose stack (about 4 vCPU / 16 GB).

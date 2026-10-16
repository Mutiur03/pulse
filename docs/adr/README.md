# Architecture decision records

Short records of the decisions that shape Pulse: the context, what we chose,
and what it costs us. A new decision gets the next number; a reversed one is
marked superseded rather than deleted.

| # | Decision | Status |
| --- | --- | --- |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-kafka-between-ingest-and-storage.md) | Kafka between ingest and storage | Accepted |
| [0003](0003-clickhouse-for-telemetry.md) | ClickHouse for events, traces, logs, errors and metrics | Accepted |
| [0004](0004-go-backend.md) | Go for every backend service, in one module | Accepted |
| [0005](0005-opentelemetry-wire-format.md) | OpenTelemetry as the wire format and SDK base | Accepted |
| [0006](0006-unified-platform-separate-write-path.md) | One product and data model, with the write path deployed separately | Accepted |
| [0007](0007-replay-storage.md) | Session replay in object storage, indexed in ClickHouse | Accepted |
| [0008](0008-postgres-for-metadata.md) | Postgres for metadata and control-plane state | Accepted |
| [0009](0009-spec-first-api.md) | Spec-first OpenAPI with generated Go server and TS client | Accepted |
| [0010](0010-pii-scrubbing-at-ingest.md) | PII scrubbing at the ingest gateway | Accepted |
| [0011](0011-self-monitoring-with-prometheus.md) | Prometheus metrics for self-monitoring | Accepted |
| [0012](0012-managed-data-services-in-kubernetes.md) | Managed data services; Helm deploys only Pulse | Accepted |

# Pulse documentation

## Using Pulse

| Guide | What it covers |
| --- | --- |
| [Sending backend telemetry](sending-backend-telemetry.md) | Pointing OpenTelemetry SDKs or the Collector at the ingest gateway |
| [Source maps](source-maps.md) | Releases, upload tokens, `pulse-cli` and the Vite plugin |
| [Product analytics](product-analytics.md) | Persons, identify and alias, retention and paths |
| [Dashboards](dashboards.md) | Trends, formulas, breakdowns and shared dashboards |
| [Logs](logs.md) | Log search, live tail, logs on traces and log alerts |
| [Alerts](alerts.md) | Alert rules, states and notification channels |
| [Data governance](data-governance.md) | PII scrubbing, retention and person deletion |
| [Security](SECURITY.md) | Sessions, CSRF, rate limits, roles, audit log and a production checklist |

## Running Pulse

| Guide | What it covers |
| --- | --- |
| [Deployment](deployment.md) | Helm chart, managed data services and Terraform |
| [Staging](staging.md) | The Helm chart on a local k3s cluster, with checks and results |
| [Monitoring](monitoring.md) | Prometheus metrics, alert rules and the Grafana dashboard |
| [Load testing](load-testing.md) | k6 scripts, measured ingest throughput, query latency and query limits |
| [Backup and restore](backup-restore.md) | Postgres, ClickHouse and object storage |

## Developing Pulse

| Guide | What it covers |
| --- | --- |
| [Development](development.md) | Local setup, code generation, tests and data flow |
| [Architecture](architecture.md) | The full technical design |
| [Decision records](adr/README.md) | Why Kafka, ClickHouse, Go, OpenTelemetry and the rest |
| [Service diagram](services-diagram.mmd), [sequence diagram](sequence-diagram.mmd) | Mermaid sources |

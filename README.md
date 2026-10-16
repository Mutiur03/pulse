# Telemetry Platform

A unified product analytics and observability platform that combines product analytics (events, funnels, retention), distributed tracing (APM), logs, metrics, and error tracking in one system.

**Core idea:** One trace per user action. A click in the browser starts an OpenTelemetry trace that flows through your entire stack, correlating product events, session replay, errors, and backend spans.

## Architecture

See [docs/architecture.md](docs/architecture.md) for the complete technical design (Draft v4).

## Features

- **Product Analytics**: Events, autocapture, funnels, retention, paths, cohorts, session replay
- **Logs Explorer**: Search OpenTelemetry logs by text, level, service, trace and attributes, live tail, logs on the trace view, and log count alerts; see [docs/logs.md](docs/logs.md)
- **Observability (APM)**: Distributed traces, service maps, RED metrics, logs with full-text search
- **Error Tracking**: Exception capture, stack traces, symbolication, issue grouping
- **Unified Correlation**: Every signal (event, span, log, error, replay) shares `trace_id`, `session_id`, and `person_id`
- **Dashboards and Insights**: Trend query builder (series, filters, breakdowns, formulas) and shareable dashboards of trends, funnels, metrics and trace stats; see [docs/dashboards.md](docs/dashboards.md)
- **Product Analytics**: Persons with identify and alias stitching, retention cohorts, user paths and per-person sessions; see [docs/product-analytics.md](docs/product-analytics.md)
- **Operations**: Helm chart, Terraform skeleton, Prometheus metrics with a Grafana dashboard and alerts, k6 load tests, and backup and restore scripts; see [docs/deployment.md](docs/deployment.md), [docs/monitoring.md](docs/monitoring.md), [docs/load-testing.md](docs/load-testing.md), [docs/backup-restore.md](docs/backup-restore.md) and the [architecture decision records](docs/adr/README.md)
- **Data Governance**: Per-project PII scrubbing at ingest, enforced retention periods and audited person deletion requests; see [docs/data-governance.md](docs/data-governance.md)

## Repository Structure

```
/
├── go/                      # Go module: all backend services
│   ├── cmd/                 # Service entry points
│   │   ├── ingest-gateway/  # Public write endpoint (OTLP + events)
│   │   ├── stream-processor/# Kafka consumer pipeline
│   │   ├── query-api/       # Read API (chi + OpenAPI)
│   │   ├── alerting-worker/ # Alert evaluation and notification
│   │   ├── pulse-cli/       # Source map inject and upload CLI
│   │   └── platform/        # Combined binary for self-host
│   └── internal/            # Shared Go packages
│       ├── config/          # Configuration
│       ├── kafka/           # Kafka producer/consumer helpers
│       ├── store/           # ClickHouse and Postgres clients
│       ├── telemetry/       # OpenTelemetry setup
│       └── ...
├── apps/web/                # Next.js web application
├── packages/
│   ├── sdk-browser/         # TypeScript browser SDK
│   ├── vite-plugin/         # Debug ids and source map upload for Vite builds
│   └── api-client/          # Generated TypeScript client
├── workers/ai-worker/       # Python AI worker (optional)
├── schemas/
│   ├── openapi/             # OpenAPI 3.1 spec (source of truth)
│   ├── postgres/            # Postgres migrations
│   ├── clickhouse/          # ClickHouse DDL
│   └── proto/               # Protobuf schemas (Kafka messages)
├── deploy/
│   ├── compose/             # Docker Compose for self-host
│   └── helm/                # Kubernetes Helm charts
└── docs/                    # Architecture and design docs
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend Services | Go (chi, pgx, clickhouse-go, franz-go, asynq) |
| Message Bus | Apache Kafka (KRaft mode) |
| Data Stores | ClickHouse (events/traces/logs), Postgres (metadata), Redis (cache/queue), S3/MinIO (blobs) |
| Web App | Next.js / React / TypeScript |
| Browser SDK | TypeScript on OpenTelemetry Web |
| AI Worker | Python (FastAPI, uv) |
| API Contract | OpenAPI 3.1 → oapi-codegen (Go) + openapi-typescript (TS) |

## Quick Start (Docker Compose)

### Prerequisites

- Docker and Docker Compose
- Go 1.26+ (for local development)
- Node.js 20+ and pnpm (for web development)

### Run the Stack

```bash
# Start infrastructure and services
make compose-up

# Or manually
cd deploy/compose && docker compose up -d
```

This starts:
- **Kafka** (port 9092) - Message broker
- **ClickHouse** (ports 8123, 9000) - Columnar database
- **Postgres** (port 5432) - Metadata store
- **Redis** (port 6379) - Cache and queue
- **MinIO** (ports 9001, 9002) - Object storage
- **ingest-gateway** (port 8080) - Public write endpoint
- **stream-processor** (port 8082) - Event processing pipeline (writes to ClickHouse)
- **query-api** (port 8081) - Read API
- **alerting-worker** (port 8083) - Alert evaluation
- **web** (port 3000) - Web UI

### Verify the Stack

```bash
# Check service health
curl http://localhost:8080/health  # Ingest gateway
curl http://localhost:8081/health  # Query API
curl http://localhost:8082/health  # Stream processor
curl http://localhost:8083/health  # Alerting worker

# Open the web UI (local login: demo@pulse.local / pulse-demo)
open http://localhost:3000
```

### Demo Data and UI Tests

The compose stack seeds a dev user (`demo@pulse.local` / `pulse-demo`),
project 1 and the ingest key `pk_dev_local_0123456789abcdef`
(`deploy/compose/seed/dev-seed.sql`). Every ingest request needs a key in the
`X-Pulse-Key` header or `?key=`; the project is always taken from the key.

```bash
make seed       # real browsers click through the demo shop (SDK + Go demo API)
make e2e-web    # Playwright: login, trace waterfall, issue, funnel, events, settings
```

The funnel page (`/funnels`) shows, for the users who dropped at each step,
the top errors and backend p95 latency in their sessions next to the users
who continued.

### End-to-End Smoke Test

See [docs/sending-backend-telemetry.md](docs/sending-backend-telemetry.md) for pointing OpenTelemetry SDKs or the Collector at the gateway, and [docs/SECURITY.md](docs/SECURITY.md) for sessions, CSRF, rate limits, roles, the audit log and the production checklist.

Run the smoke test to verify the complete data flow (ingest → Kafka → ClickHouse → query API):

```bash
./tools/smoke-test.sh
```

This posts an event and a span with a unique trace ID, then polls ClickHouse until both rows exist. It exits non-zero if either is missing after 90 seconds (`SMOKE_TIMEOUT`). ClickHouse tables are created on every startup by the `clickhouse-migrate` service from `schemas/clickhouse/*.sql`. Session replay chunks go to the `objectstore` service, an S3-compatible gateway (any S3 works via `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`); their metadata is in the ClickHouse `replay_chunks` table.

### Alerts

Alert rules live in Postgres per project (`schemas/postgres/006_alerts.sql`) and are managed under **Alerts** in the web app. A rule watches one metric over a window of 1m to 24h:

- `error_count`, `error_rate` (errors per minute), `failure_rate` (percent of server requests that failed)
- `latency_p95` and `latency_p99` (server spans, ms), `throughput` (requests per minute; compare with "below" to catch drops)
- `event_count` for a named product event
- `new_issue` and `issue_regression`, which fire from the `issue.events` topic

Rules can filter by service, route and environment. The `alerting-worker` checks each rule on its own interval (10s to 1h), using a Redis lease so only one replica evaluates it. A rule moves between OK, ALERTING and NO_DATA, and every change is kept as history with an example issue and trace. Notifications go out when a rule fires and when it resolves, and optionally repeat while it keeps firing; that same interval stops a flapping rule from paging again. Muting a rule pauses notifications but not evaluation.

Channels are email (through `SMTP_ADDR`; Mailpit locally), Slack incoming webhooks, and generic webhooks. Webhook requests carry `X-Pulse-Signature: t=<unix>,v1=<hex>`, where `v1` is HMAC-SHA256 of `<unix>.<body>` with the channel's secret, which is shown once when the channel is created.

### Source Maps

Minified browser stacks are resolved to original files, lines, functions and code with source maps uploaded per release:

1. Set `release` in the browser SDK (for example `web@1.4.2`).
2. Create an upload token under **Settings > Source map upload tokens**. Upload tokens are secret and project-scoped, unlike public ingest keys, and can be revoked there.
3. After each build, inject debug ids and upload the maps, with `pulse-cli` (any bundler) or `@pulse/vite-plugin`:

```bash
cd go && go run ./cmd/pulse-cli sourcemaps inject ../dist
go run ./cmd/pulse-cli sourcemaps upload --release web@1.4.2 --url-prefix ~/assets \
  --url http://localhost:8081 --project 1 --token pul_... ../dist
```

`inject` appends a small snippet and a `//# debugId=` comment to each script (after all code, so no mapping moves) and writes the same `debugId` into its map; the SDK sends the ids of the scripts in each stack. Uploads go to `POST /v1/projects/{id}/releases/{release}/sourcemaps` (multipart, `Authorization: Bearer pul_...`, 50 MB per request, 20 MB per map) and are stored in object storage (`S3_ARTIFACT_BUCKET`, default `pulse-artifacts`), with releases and artifacts in Postgres (`schemas/postgres/007_releases.sql`).

The stream processor symbolicates JavaScript errors before grouping them: frames are matched to a map by debug id, falling back to the file URL within the error's release, and parsed maps are kept in an in-memory LRU. Issues are fingerprinted on original files and functions, so a new build with new bundle names and minified identifiers keeps the same issue. Both the raw stack and the resolved frames are stored. Maps uploaded after an error are applied when the issue is opened, or with **Reprocess**; that does not regroup existing events. The **Releases** page lists each release's artifacts.

### Manual Testing

Send a test event:

```bash
curl -X POST http://localhost:8080/v1/events \
  -H "Content-Type: application/json" \
  -H "X-Pulse-Key: pk_dev_local_0123456789abcdef" \
  -d '{
    "event": "$pageview",
    "timestamp": "2026-10-15T05:00:00Z",
    "distinct_id": "user_123",
    "session_id": "sess_abc",
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "properties": {
      "$current_url": "https://example.com/home",
      "title": "Home Page"
    }
  }'
```

The event flows through:
1. **ingest-gateway** accepts it and produces to Kafka `raw.events`
2. **stream-processor** consumes from Kafka and inserts to ClickHouse `events` table
3. **query-api** can read it from ClickHouse

Query the data from ClickHouse:

```bash
docker exec clickhouse clickhouse-client --query \
  "SELECT * FROM events WHERE project_id = 1 ORDER BY timestamp DESC LIMIT 5 FORMAT Vertical"
```

### Send a Test Trace

```bash
curl -X POST http://localhost:8080/v1/traces \
  -H "Content-Type: application/json" \
  -H "X-Pulse-Key: pk_dev_local_0123456789abcdef" \
  -d '[{
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "span_id": "00f067aa0ba902b7",
    "name": "GET /api/checkout"
  }]'
```

Query traces:

```bash
docker exec clickhouse clickhouse-client --query \
  "SELECT * FROM spans WHERE project_id = 1 ORDER BY timestamp DESC LIMIT 5 FORMAT Vertical"
```

### Stop the Stack

```bash
make compose-down

# Or manually
cd deploy/compose && docker compose down
```

## Local Development

### Backend (Go)

```bash
cd go

# Download dependencies
go mod download

# Run a service locally
go run ./cmd/ingest-gateway
go run ./cmd/stream-processor
go run ./cmd/query-api
go run ./cmd/alerting-worker

# Run tests
go test ./...

# Build binaries
make build
```

### Web (Next.js)

```bash
# Install dependencies
pnpm install

# Run dev server
cd apps/web
pnpm run dev

# Build for production
pnpm run build
```

### Browser SDK

```bash
cd packages/sdk-browser

# Build SDK
pnpm run build

# Use in an app
import { init } from '@pulse/browser';

const sdk = init({
  projectKey: 'pk_live_...',
  endpoint: 'http://localhost:8080',
  autocapture: true,
  propagateTraceHeaderCorsUrls: [/^https:\/\/api\.myshop\.com/],
});

// Capture custom event
sdk.capture('button_clicked', { button: 'checkout' });

// Identify user
sdk.identify('user_123', { email: 'user@example.com' });
```

The SDK:
- Sends `traceparent` headers on fetch requests to configured URLs
- Captures `$pageview` events automatically
- Generates trace IDs for correlation
- Includes `session.id` in `baggage` header

### Code Generation

Generate Go server and TypeScript client from OpenAPI spec:

```bash
make generate
```

This runs:
1. `oapi-codegen` - Generates Go chi server interfaces and models in `go/internal/api/gen/`
2. `openapi-typescript` - Generates TypeScript types in `packages/api-client/src/generated/`

The generated code is checked into git and validated in CI.

## Data Flow

```
Browser SDK → ingest-gateway → Kafka raw.* → stream-processor → ClickHouse
                                                   ↓
                                              Postgres (persons, issues)

User → Web (Next.js) → query-api → ClickHouse / Postgres
```

## Schema Overview

### ClickHouse Tables

- `events` - Product analytics events (100% kept)
- `spans` - Distributed trace spans (tail-sampled)
- `logs` - Application logs
- `errors` - Error occurrences
- `sessions` - Session rollups

### Postgres Tables

- `projects` - Project/tenant configuration
- `persons` - Person identity (authoritative)
- `person_distinct_ids` - Identity mappings
- `issues` - Error issue metadata

### Kafka Topics

- `raw.events` - Incoming events
- `raw.spans` - Incoming spans
- `raw.logs` - Incoming logs
- `raw.errors` - Incoming errors
- `raw.replay` - Session replay chunks

## Configuration

Environment variables (see `go/internal/config/config.go`):

```bash
# Service
SERVICE_NAME=ingest-gateway
ENVIRONMENT=production
LOG_LEVEL=info

# Infrastructure
KAFKA_BOOTSTRAP_SERVERS=localhost:9092
REDIS_ADDR=localhost:6379
CLICKHOUSE_ADDR=localhost:9000
POSTGRES_URL=postgres://platform:platform@localhost:5432/platform

# Service ports
INGEST_GATEWAY_PORT=8080
QUERY_API_PORT=8081
```

## Production Deployment (Kubernetes/Helm)

See `deploy/helm/` for the Helm chart structure (skeleton).

Each service scales independently:
- **ingest-gateway**: HPA on CPU/RPS
- **stream-processor**: KEDA on Kafka consumer lag
- **query-api**: HPA on CPU
- **alerting-worker**: Fixed replicas with distributed workers
- **ai-worker**: KEDA on job queue depth (optional)

## Testing

```bash
# Go tests
cd go && go test ./...

# TypeScript tests
pnpm run test

# Lint
make lint
```

## CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs on every push:
- Go: build, test, lint
- TypeScript: build, test, lint
- Docker: multi-stage builds for all services

## Roadmap

- **Phase 0** (current): Foundations - monorepo scaffold, Docker Compose, basic ingest/query
- **Phase 1**: Browser SDK with OTel, autocapture, errors, trace correlation
- **Phase 2**: Session replay (rrweb), funnels, retention, identity stitching
- **Phase 3**: Logs, metrics, service map, AI root cause
- **Phase 4**: Mobile SDKs, feature flags, warehouse export

## Contributing

This is the initial scaffold. See [ARCHITECTURE.md](docs/architecture.md) for design decisions and future plans.

## License

MIT License - see [LICENSE](LICENSE)

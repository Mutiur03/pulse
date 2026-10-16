# Development

How to run Pulse locally, test it and change it. To deploy it, see
[deployment.md](deployment.md).

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
1. `oapi-codegen` - Generates the shared models in `go/internal/api/gen/` and one server interface per query-api module in `go/internal/modules/<module>/adapters/httpapi/oapi/` (configs in `schemas/openapi/modules/`)
2. `openapi-typescript` - Generates TypeScript types in `packages/api-client/src/generated/`

The generated code is checked into git and validated in CI.

## Manual testing

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

## Tests

```bash
cd go && go test ./...                       # Go unit tests
PULSE_TEST_CLICKHOUSE=localhost:9000 go test ./internal/store/ch/   # against the compose ClickHouse
pnpm run test                                # TypeScript and SDK tests
make e2e-web                                 # Playwright against the compose stack
./tools/smoke-test.sh                        # ingest -> Kafka -> ClickHouse
```

CI (`.github/workflows/ci.yml`) runs Go build, tests and lint, the
TypeScript build and SDK tests, the generated code check, a dependency audit,
the Docker builds, the deployment config check (Helm, Terraform,
Prometheus rules) and the end-to-end Playwright suite on the compose stack.

## Data flow

```
Browser SDK / OTel SDKs -> ingest-gateway -> Kafka raw.* -> stream-processor -> ClickHouse
                                                                  |
                                                                  v
                                                        Postgres (issues, persons)

User -> web (Next.js) -> query-api -> ClickHouse / Postgres / object storage
```

- ClickHouse tables (`schemas/clickhouse`): `events`, `spans`,
  `trace_summaries` (materialized view), `logs`, `errors`, `sessions`,
  `metrics`, `replay_chunks` and `person_aliases`.
- Postgres tables (`schemas/postgres`): users, organizations, projects,
  keys, issues, alerts, dashboards, releases, data settings and the audit
  log.
- Kafka topics: `raw.events`, `raw.spans`, `raw.logs`, `raw.errors`,
  `raw.metrics` and `issue.events`.

Service configuration is read from the environment
(`go/internal/config/config.go` and each `cmd/*/main.go`). The compose file
and the Helm values list every variable with its default.

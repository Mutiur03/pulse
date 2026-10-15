# Go demo API

A small Go HTTP API instrumented with OpenTelemetry: `otelhttp` for
incoming requests, `otelsql` for every Postgres query, and the OTLP/HTTP
protobuf exporter pointed at the Pulse ingest gateway. The browser demo
calls it, so one trace covers browser click -> fetch -> Go handler -> SQL.

| Route | What it does |
| --- | --- |
| `GET /api/orders?n=3` | runs a `generate_series` query and returns rows |
| `POST /api/pay` | looks up a limit, then fails with a recorded exception (HTTP 402) |

Run it with the stack:

```bash
cd deploy/compose && docker compose --profile demo up -d --build --wait
pnpm --filter @pulse/browser build && pnpm --filter @pulse/browser-demo start
# open http://localhost:5173 and click "Pay via Go API (fails)"
curl localhost:8081/v1/issues?service=go-demo-api
```

Or on its own: `go run .` with `DATABASE_URL` and
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` (default
`http://localhost:8080/v1/traces`).

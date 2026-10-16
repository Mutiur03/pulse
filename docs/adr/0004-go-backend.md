# 4. Go for every backend service, in one module

Date: 2026-10-16. Status: accepted.

## Context

The backend has a high-volume write path, a stream processor, an API and
background workers. They share types (event shapes, scrubbing rules, alert
metrics, query builders) and must stay consistent.

## Decision

Write all backend services in Go, as separate binaries (`go/cmd/*`) in one
module with shared packages in `go/internal`. The query API uses chi with
handlers generated from the OpenAPI spec (ADR 0009); storage uses pgx and
clickhouse-go; Kafka uses franz-go. Each binary builds into a small Alpine
image from one multi-stage Dockerfile.

## Consequences

- Low memory per service (tens of MB at idle), static binaries, fast
  startup, and one toolchain, linter (golangci-lint) and test runner.
- Shared code (for example the alert metric SQL used by both the API preview
  and the alerting worker) cannot drift between services.
- Python is not used yet; the AI worker in the design stays future work.
- The `platform` combined binary from the design is still a stub; the
  services run as separate containers everywhere (ADR 0006).

# 9. Spec-first OpenAPI with generated Go server and TS client

Date: 2026-10-16. Status: accepted.

## Context

The web app, the CLI and external users all call the query API. Hand-kept
types on both sides drift.

## Decision

`schemas/openapi/api.yaml` is the source of truth. `make generate` produces
the Go server interface and models (oapi-codegen, chi) and the TypeScript
types used by `@pulse/api-client`. CI regenerates and fails if the committed
code differs.

## Consequences

- Adding an endpoint is spec, regenerate, implement; the compiler lists
  every missing handler.
- Generated files are committed and reviewed like code.

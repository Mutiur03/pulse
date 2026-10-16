# 5. OpenTelemetry as the wire format and SDK base

Date: 2026-10-16. Status: accepted.

## Context

Backend teams already instrument with OpenTelemetry, and asking them to adopt
a proprietary agent is a barrier. The browser also needs traces that connect
to the backend, plus product events, errors and replay.

## Decision

The ingest gateway accepts OTLP over HTTP (protobuf and JSON) and gRPC for
traces, logs and metrics, so any OpenTelemetry SDK or Collector can send
directly with an `X-Pulse-Key` header. The browser SDK (`@pulse/browser`)
builds on OpenTelemetry JS for fetch/XHR tracing and W3C trace context
propagation, and adds Pulse envelopes for product events, errors and rrweb
replay chunks. Semantic conventions (`service.name`, `http.route`,
`db.system`, `enduser.id`) map onto ClickHouse columns.

## Consequences

- Zero-code adoption for services that already use OpenTelemetry; the Go
  demo API uses the stock SDKs.
- We follow upstream conventions as they evolve and keep raw attributes in
  maps for anything we do not model.
- Product events and replay are not OpenTelemetry signals, so those parts of
  the SDK and API remain Pulse-specific.

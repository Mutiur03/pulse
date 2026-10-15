# Sending backend traces, errors and logs

The ingest gateway speaks OTLP/HTTP, so any OpenTelemetry SDK or the
OpenTelemetry Collector can send to it directly.

| Signal | Endpoint | Encodings |
| --- | --- | --- |
| Traces | `POST /v1/traces` | `application/x-protobuf`, `application/json` (OTLP JSON) |
| Logs | `POST /v1/logs` | `application/x-protobuf`, `application/json` |
| Errors (browser SDK, custom) | `POST /v1/errors` | JSON object or array |
| Product events | `POST /v1/events` | JSON object or array |

gRPC (port 4317) is not supported yet; use the HTTP exporter.

## Ingest key

Every request needs the project's public ingest key (Settings page), in the
`X-Pulse-Key` header or the `?key=` query parameter. The project is taken
from the key; any `project_id` in the payload or resource is ignored.
Unknown or revoked keys get `401`, and requests over the key's per-second
limit get `429` with `Retry-After`. The local compose stack seeds
`pk_dev_local_0123456789abcdef`.

## OpenTelemetry SDKs

Every OTel SDK reads the standard environment variables:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080   # SDK appends /v1/traces and /v1/logs
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_SERVICE_NAME=checkout-api
export OTEL_EXPORTER_OTLP_HEADERS=X-Pulse-Key=pk_dev_local_0123456789abcdef
export OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production
export OTEL_PROPAGATORS=tracecontext,baggage
```

Go, using `otlptracehttp` (see `examples/go-demo-api` for a full program
with `otelhttp` and `otelsql`):

```go
exp, _ := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL("http://localhost:8080/v1/traces"))
tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
otel.SetTracerProvider(tp)
otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
```

Node.js:

```js
new NodeSDK({
  traceExporter: new OTLPTraceExporter({ url: 'http://localhost:8080/v1/traces' }),
  logRecordProcessors: [new BatchLogRecordProcessor(new OTLPLogExporter({ url: 'http://localhost:8080/v1/logs' }))],
}).start();
```

Python:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080 opentelemetry-instrument python app.py
```

To connect browser traces to the backend, allow the `traceparent`,
`tracestate` and `baggage` request headers in your API's CORS settings and
list the API in the browser SDK's `propagateTraceHeaderCorsUrls`. The
baggage carries `session.id` and `enduser.id`; copy them onto your server
span (as the Go demo's `withSession` middleware does) to filter backend
spans by session.

## OpenTelemetry Collector

```yaml
exporters:
  otlphttp/pulse:
    endpoint: http://pulse-ingest:8080
    encoding: proto          # or json
    headers:
      X-Pulse-Key: pk_dev_local_0123456789abcdef

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/pulse]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/pulse]
```

## How backend errors are found

- A span event named `exception` becomes an error row with
  `exception.type`, `exception.message` and `exception.stacktrace`, linked
  to the span, trace and service. In Go use
  `span.RecordError(err, trace.WithStackTrace(true))`; most other SDKs
  record stacks by default.
- A span with status `ERROR` and no exception event becomes an error of
  type `error.type` (or `SpanError`) with the status message, unless the
  same trace already reported an exception in that export. That keeps one
  failure that bubbles up through several spans from being counted
  several times.
- Browser spans (`telemetry.sdk.language=webjs`) are not turned into errors;
  the browser SDK reports its errors to `/v1/errors` with more context.

## Issues

Errors are grouped into issues by fingerprint: the exception type plus the
top three in-app stack frames (function and file, without line numbers,
build hashes or hosts). Frames from `node_modules`, the Go module cache,
the Go runtime, `site-packages`, the JDK and so on are not in-app. Errors
without a usable stack are grouped by type plus the message with numbers,
ids, hex, UUIDs, emails, URLs and quoted values stripped.

Issues live in Postgres (`issues` table) with `first_seen`, `last_seen`,
`count`, `status` (`unresolved`, `resolved`, `ignored`) and a sample trace.
When a resolved issue gets an event that happened after it was resolved it
reopens as a regression. New issues and regressions are published to the
Kafka topic `issue.events`:

```json
{"type":"regression","issue_id":42,"project_id":1,"fingerprint":"…","title":"PaymentError: declined","service_name":"checkout-api","trace_id":"…","timestamp":"…"}
```

## Reading it back

```bash
curl localhost:8081/v1/issues?status=unresolved&sort=count
curl localhost:8081/v1/issues/42
curl -X PATCH localhost:8081/v1/issues/42 -H 'Content-Type: application/json' -d '{"status":"resolved"}'
curl localhost:8081/v1/traces/<trace_id>     # browser + backend + DB spans as one tree
```

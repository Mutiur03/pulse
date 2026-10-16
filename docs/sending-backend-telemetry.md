# Sending backend traces, metrics, errors and logs

The ingest gateway speaks OTLP over HTTP (port 8080) and gRPC (port 4317),
so any OpenTelemetry SDK or the OpenTelemetry Collector can send to it
directly.

| Signal | OTLP/HTTP | OTLP/gRPC (:4317) |
| --- | --- | --- |
| Traces | `POST /v1/traces` (`application/x-protobuf` or OTLP JSON) | `TraceService/Export` |
| Metrics | `POST /v1/metrics` (protobuf or OTLP JSON) | `MetricsService/Export` |
| Logs | `POST /v1/logs` (protobuf or OTLP JSON) | `LogsService/Export` |
| Errors (browser SDK, custom) | `POST /v1/errors` (JSON object or array) | |
| Product events | `POST /v1/events` (JSON object or array) | |

## Ingest key

Every request needs the project's public ingest key (Settings page). The
project is taken from the key; any `project_id` in the payload or resource
is ignored. The local compose stack seeds `pk_dev_local_0123456789abcdef`.

| | OTLP/HTTP | OTLP/gRPC |
| --- | --- | --- |
| Key | `X-Pulse-Key` header or `?key=` | `x-pulse-key` metadata or `authorization: Bearer <key>` |
| Missing, unknown or revoked key | `401` | `UNAUTHENTICATED` |
| Over the key's per-second limit | `429` with `Retry-After` | `RESOURCE_EXHAUSTED` with `RetryInfo` |
| Malformed payload | `400` | `INVALID_ARGUMENT` |
| Queue unavailable (retry) | `503` | `UNAVAILABLE` |

Both share the same keys, rate limits, validation and Kafka topics. Over
gRPC, spans without a trace or span id and metric points that cannot be
stored (no name, NaN, malformed histogram buckets) are dropped and counted
in the response's `partial_success`; if nothing in an export is usable the
call fails with `INVALID_ARGUMENT`. gzip compression is accepted. Messages
over 16 MiB are refused with `RESOURCE_EXHAUSTED`.

gRPC receiver settings on the gateway:

| Variable | Default | |
| --- | --- | --- |
| `OTLP_GRPC_PORT` | `4317` | `off` disables the receiver |
| `OTLP_GRPC_MAX_RECV_MIB` | `16` | largest accepted message |
| `OTLP_GRPC_TLS_CERT`, `OTLP_GRPC_TLS_KEY` | unset | PEM files; serve TLS when set |

On shutdown the gateway stops accepting new calls and finishes in-flight
exports for up to 10 seconds.

## OpenTelemetry SDKs

Every OTel SDK reads the standard environment variables. Over HTTP:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080   # SDK appends /v1/traces, /v1/metrics, /v1/logs
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_HEADERS=X-Pulse-Key=pk_dev_local_0123456789abcdef
export OTEL_SERVICE_NAME=checkout-api
export OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production
export OTEL_PROPAGATORS=tracecontext,baggage
```

Over gRPC (header names become lowercase gRPC metadata):

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317   # https:// when the gateway serves TLS
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
export OTEL_EXPORTER_OTLP_HEADERS=x-pulse-key=pk_dev_local_0123456789abcdef
export OTEL_EXPORTER_OTLP_COMPRESSION=gzip
export OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE=delta
```

Delta temporality is recommended for metrics: sums and histograms are
stored per point, so delta points add up and percentiles cover each
interval rather than the process lifetime.

Go: `examples/go-demo-api` exports traces and `otelhttp` server metrics with
`otlptracehttp`/`otlpmetrichttp`, or `otlptracegrpc`/`otlpmetricgrpc` when
`OTEL_EXPORTER_OTLP_PROTOCOL=grpc` (the default in the compose stack):

```go
// gRPC; endpoint and headers come from OTEL_EXPORTER_OTLP_* or options.
exp, _ := otlptracegrpc.New(ctx,
	otlptracegrpc.WithEndpointURL("http://localhost:4317"),
	otlptracegrpc.WithHeaders(map[string]string{"x-pulse-key": key}),
	otlptracegrpc.WithCompressor("gzip"))
mexp, _ := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL("http://localhost:4317"),
	otlpmetricgrpc.WithHeaders(map[string]string{"x-pulse-key": key}))

// HTTP
exp, _ := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL("http://localhost:8080/v1/traces"))

tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp)), sdkmetric.WithResource(res))
otel.SetTracerProvider(tp)
otel.SetMeterProvider(mp)
otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
```

Node.js:

```js
// HTTP: @opentelemetry/exporter-*-otlp-proto
new NodeSDK({
  traceExporter: new OTLPTraceExporter({ url: 'http://localhost:8080/v1/traces', headers: { 'X-Pulse-Key': key } }),
  metricReader: new PeriodicExportingMetricReader({ exporter: new OTLPMetricExporter({ url: 'http://localhost:8080/v1/metrics', headers: { 'X-Pulse-Key': key } }) }),
}).start();

// gRPC: @opentelemetry/exporter-*-otlp-grpc
const metadata = new Metadata();
metadata.set('x-pulse-key', key);
new NodeSDK({
  traceExporter: new OTLPTraceExporter({ url: 'http://localhost:4317', metadata, compression: 'gzip' }),
  metricReader: new PeriodicExportingMetricReader({ exporter: new OTLPMetricExporter({ url: 'http://localhost:4317', metadata }) }),
}).start();
```

Python:

```bash
# HTTP
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080 OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf \
  OTEL_EXPORTER_OTLP_HEADERS=X-Pulse-Key=pk_... opentelemetry-instrument python app.py
# gRPC
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 OTEL_EXPORTER_OTLP_PROTOCOL=grpc \
  OTEL_EXPORTER_OTLP_HEADERS=x-pulse-key=pk_... opentelemetry-instrument python app.py
```

To connect browser traces to the backend, allow the `traceparent`,
`tracestate` and `baggage` request headers in your API's CORS settings and
list the API in the browser SDK's `propagateTraceHeaderCorsUrls`. The
baggage carries `session.id` and `enduser.id`; copy them onto your server
span (as the Go demo's `withSession` middleware does) to filter backend
spans by session.

## OpenTelemetry Collector

Use either exporter; both carry traces, metrics and logs.

```yaml
exporters:
  # gRPC
  otlp/pulse:
    endpoint: pulse-ingest:4317
    tls:
      insecure: true         # remove when the gateway serves TLS
    compression: gzip
    headers:
      x-pulse-key: pk_dev_local_0123456789abcdef
  # HTTP
  otlphttp/pulse:
    endpoint: http://pulse-ingest:8080
    encoding: proto          # or json
    headers:
      X-Pulse-Key: pk_dev_local_0123456789abcdef

processors:
  batch: {}
  cumulativetodelta: {}      # recommended for metrics

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/pulse]
    metrics:
      receivers: [otlp]
      processors: [cumulativetodelta, batch]
      exporters: [otlp/pulse]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/pulse]
```

The collector's `retry_on_failure` backs off on `UNAVAILABLE` and on
`RESOURCE_EXHAUSTED` using the gateway's `RetryInfo`; `INVALID_ARGUMENT`
and `UNAUTHENTICATED` are not retried.

To check a gateway quickly, `tools/otlp-grpc-smoke.sh` sends traces,
metrics and logs with the OpenTelemetry `telemetrygen` tool over gRPC and
waits for them in ClickHouse.

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
curl 'localhost:8081/v1/metrics/names'
curl 'localhost:8081/v1/metrics/query?name=http.server.request.duration&agg=p95&group_by=http.route'
```

Metrics are stored one row per data point in the ClickHouse `metrics`
table (30 day TTL). The query endpoint buckets by `step` (default about 60
buckets over the range) and aggregates with `avg`, `sum`, `min`, `max`,
`count`, `rate` or a histogram percentile (`p50` to `p99`, interpolated
within explicit buckets); `group_by` splits by `service.name` or a point
attribute into at most 20 series. The **Metrics** page charts them.

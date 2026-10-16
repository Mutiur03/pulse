# Logs

Pulse stores OpenTelemetry logs sent to the ingest gateway (`POST /v1/logs`
over OTLP/HTTP, or the gRPC LogsService on port 4317) and keeps them for
three days. A log written with a span in its context carries the trace and
span ids, which is how logs and traces are linked.

## Sending logs

Any OpenTelemetry logs SDK works. In Go, the slog bridge is enough:

```go
lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
logger := otelslog.NewLogger("checkout", otelslog.WithLoggerProvider(lp))

// With the request context the log gets the current trace and span ids.
logger.ErrorContext(r.Context(), "payment failed", "order_id", id)
```

`examples/go-demo-api` does this for its order and payment handlers.

## Search

The search box takes terms that must all match:

| Term | Matches |
| --- | --- |
| `timeout` | body contains the word (case-insensitive) |
| `"payment failed"` | body contains the phrase |
| `level:error` | severity is error (`trace`, `debug`, `info`, `warn`, `error`, `fatal`) |
| `level:warn+` | warn or worse |
| `service:checkout` | service name |
| `trace:<id>`, `span:<id>` | trace or span id |
| `order_id:42` | attribute equals the value |
| `-term` | excludes any of the above |

The page shows log volume per level over time, counts by level and service
(click one to filter), and the newest logs first with "Load older" for
more. Click a log to see all its attributes; click a value to filter by it.

**Live tail** polls for new logs matching the search every two seconds and
adds them on top.

## Logs and traces

Logs with a trace id link to the trace. The trace page lists the trace's
logs in time order next to the waterfall, and the span panel shows the
logs of the selected span. "Open in logs" opens the explorer with
`trace:<id>`.

## Alerts on logs

The `log_count` alert metric counts logs matching a search in the window,
for example `level:error+ service:checkout` above 10 over 5 minutes. Use
"Alert on this search" on the logs page to start a rule from the current
search. The service filter of the rule also applies.

## Limits

- Logs are kept for three days (the ClickHouse TTL of the logs table).
- Body search is a substring match without an index, so wide time ranges on
  busy projects are slower. Narrow by service or level first.
- Live tail polls; logs that share the exact nanosecond timestamp of the
  newest shown log can be skipped.

# Monitoring Pulse

Every Go service serves Prometheus metrics at `/metrics`. The Go runtime and
process metrics (`go_*`, `process_*`) are included.

| Service | Metrics endpoint |
| --- | --- |
| ingest-gateway | `:9101/metrics` (`METRICS_ADDR`), never on the public port 8080 |
| query-api | `:9102/metrics` (`METRICS_ADDR`), never on 8081 |
| stream-processor | `:8082/metrics` |
| alerting-worker | `:8083/metrics` |
| governance-worker | `:8084/metrics` |

## Metrics

| Metric | Labels | Meaning |
| --- | --- | --- |
| `pulse_build_info` | service | 1 per running service |
| `pulse_http_requests_total` | service, route, code | Ingest and query API requests. Routes are patterns such as `/v1/issues/{id}`, never raw paths |
| `pulse_http_request_duration_seconds` | service, route | Request latency histogram |
| `pulse_ingest_records_total` | topic | Records accepted and written to Kafka |
| `pulse_ingest_rejected_total` | reason | `missing_key`, `invalid_key`, `origin`, `rate_limit`, `too_large`, `invalid` |
| `pulse_ingest_produce_errors_total` | topic | Failed Kafka writes (clients get 503) |
| `pulse_ingest_produce_duration_seconds` | topic | Kafka write latency per request |
| `pulse_processor_records_total` | topic | Records consumed from Kafka |
| `pulse_processor_decode_errors_total` | topic | Records that were not valid JSON |
| `pulse_processor_rows_written_total` | kind | Rows written to ClickHouse |
| `pulse_processor_write_errors_total` | kind | Batches that failed after retries |
| `pulse_processor_write_duration_seconds` | kind | ClickHouse batch write latency |
| `pulse_processor_delay_seconds` | topic | Age of the newest record in the last batch when it was written: the ingest-to-storage delay |
| `pulse_processor_last_batch_timestamp_seconds` | | When the last batch was written. The delay gauge is current only while this is recent |
| `pulse_alert_evaluations_total` | result | Scheduled rule evaluations (`ok`, `error`) |
| `pulse_alert_notifications_total` | kind, result | Notifications per channel kind |
| `pulse_governance_deletions_total` | status | Finished deletion requests |
| `pulse_governance_retention_deleted_total` | kind | Rows and objects removed by retention |
| `pulse_governance_runs_total` | job, result | Deletion and retention runs |

## Dashboards and alerts

`deploy/monitoring` contains three things:

- `prometheus.yml`: a scrape config for the Compose services.
- `alerts.yml`: alert rules.
  - A service is down.
  - Kafka write errors.
  - Ingest-to-storage delay is above 2 minutes.
  - ClickHouse write errors.
  - More than 5% of query API requests return 5xx.
  - Query API p95 is above 2 seconds.
- `grafana/dashboards/pulse-overview.json`: the **Pulse self-monitoring**
  dashboard. It shows ingest rate and rejections, ingest and Kafka latency,
  rows written and delay, ClickHouse write latency and errors, query API
  status and slowest routes, alerting and governance activity, and memory
  and goroutines per service.

Run them locally next to the stack:

```bash
cd deploy/compose
docker compose --profile monitoring up -d
# Prometheus: http://localhost:9090, Grafana: http://localhost:3001 (anonymous viewer; admin password pulse-local)
```

On Kubernetes the chart adds `prometheus.io/scrape` annotations to every Go
pod. With the Prometheus Operator, set `metrics.serviceMonitor.enabled=true`
instead. Then import the dashboard JSON and load `alerts.yml` as a
PrometheusRule or rule file. The dashboard uses a data source with uid
`prometheus`.

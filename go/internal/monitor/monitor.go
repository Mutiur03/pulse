// Package monitor exposes Prometheus metrics for Pulse's own services.
//
// Every Go service serves /metrics on its health or metrics port (never on
// the public ingest or API port). Metric names start with pulse_.
package monitor

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds Pulse's metrics plus Go runtime and process metrics.
var Registry = prometheus.NewRegistry()

func counter(name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	Registry.MustRegister(c)
	return c
}

func histogram(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels)
	Registry.MustRegister(h)
	return h
}

var latency = []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}

var (
	buildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "pulse_build_info", Help: "Always 1; labels name the service."}, []string{"service"})

	// HTTPRequests and HTTPDuration cover the ingest gateway and query API.
	HTTPRequests = counter("pulse_http_requests_total", "HTTP requests by service, route and status code.", "service", "route", "code")
	HTTPDuration = histogram("pulse_http_request_duration_seconds", "HTTP request latency by service and route.", latency, "service", "route")

	// Ingest.
	IngestRecords        = counter("pulse_ingest_records_total", "Records accepted and queued to Kafka, by topic.", "topic")
	IngestRejected       = counter("pulse_ingest_rejected_total", "Ingest requests refused, by reason.", "reason")
	IngestProduceErrors  = counter("pulse_ingest_produce_errors_total", "Failed Kafka writes, by topic.", "topic")
	IngestProduceLatency = histogram("pulse_ingest_produce_duration_seconds", "Kafka write latency per ingest request (all its records), by topic.", latency, "topic")

	// Stream processing.
	ProcessorRecords      = counter("pulse_processor_records_total", "Records consumed from Kafka, by topic.", "topic")
	ProcessorDecodeErrors = counter("pulse_processor_decode_errors_total", "Records that were not valid JSON, by topic.", "topic")
	ProcessorRowsWritten  = counter("pulse_processor_rows_written_total", "Rows written to ClickHouse, by kind.", "kind")
	ProcessorWriteErrors  = counter("pulse_processor_write_errors_total", "ClickHouse batches that failed after retries, by kind.", "kind")
	ProcessorWriteLatency = histogram("pulse_processor_write_duration_seconds", "ClickHouse batch write latency, by kind.", latency, "kind")
	ProcessorDelay        = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pulse_processor_delay_seconds",
		Help: "Age of the newest record in the last batch when it was written (ingest-to-storage delay), by topic.",
	}, []string{"topic"})
	ProcessorLastBatch = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pulse_processor_last_batch_timestamp_seconds",
		Help: "Unix time of the last batch written; the delay gauge is only current while this is recent.",
	})

	// Alerting.
	AlertEvaluations   = counter("pulse_alert_evaluations_total", "Scheduled alert rule evaluations, by result (ok or error).", "result")
	AlertNotifications = counter("pulse_alert_notifications_total", "Alert notifications, by channel kind and result.", "kind", "result")

	// Governance.
	GovernanceDeletions    = counter("pulse_governance_deletions_total", "Deletion requests finished, by status.", "status")
	GovernanceRetainedRows = counter("pulse_governance_retention_deleted_total", "Rows and objects deleted by retention, by kind.", "kind")
	GovernanceRuns         = counter("pulse_governance_runs_total", "Worker runs, by job and result.", "job", "result")
)

func init() {
	Registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), buildInfo, ProcessorDelay, ProcessorLastBatch)
}

// Handler serves the registry in the Prometheus text format.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{Registry: Registry})
}

// Service records the service name in pulse_build_info.
func Service(name string) { buildInfo.WithLabelValues(name).Set(1) }

// Serve runs a metrics-only server on addr in the background.
func Serve(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("metrics endpoint listening", "addr", addr, "path", "/metrics")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server", "err", err)
		}
	}()
}

type recorder struct {
	http.ResponseWriter
	code int
}

func (r *recorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Flush keeps streaming responses working.
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware counts requests and their latency. route names the request
// with a bounded value (a route pattern, never the raw path).
func Middleware(service string, route func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.code == 0 {
				rec.code = http.StatusOK
			}
			name := route(r)
			HTTPRequests.WithLabelValues(service, name, strconv.Itoa(rec.code)).Inc()
			HTTPDuration.WithLabelValues(service, name).Observe(time.Since(start).Seconds())
		})
	}
}

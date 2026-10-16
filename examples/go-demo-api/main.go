// Command go-demo-api is a small HTTP API instrumented with OpenTelemetry.
// It continues the browser's trace (traceparent), records every SQL query
// as a child span via otelsql, and sends spans to the Pulse ingest gateway
// and HTTP server metrics over OTLP/HTTP protobuf, or OTLP/gRPC with
// OTEL_EXPORTER_OTLP_PROTOCOL=grpc.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// grpcExport reports whether OTEL_EXPORTER_OTLP_PROTOCOL selects gRPC
// (port 4317) instead of the default http/protobuf (port 8080).
func grpcExport() bool { return os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL") == "grpc" }

// setupTelemetry starts trace and metric export. Exporters honor the
// standard OTEL_EXPORTER_OTLP_* variables (endpoint, headers, compression);
// without an endpoint they target the local ingest gateway.
func setupTelemetry(ctx context.Context) (func(context.Context) error, error) {
	noEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == ""
	var exp sdktrace.SpanExporter
	var mexp sdkmetric.Exporter
	var lexp sdklog.Exporter
	var err error
	if grpcExport() {
		var topts []otlptracegrpc.Option
		var mopts []otlpmetricgrpc.Option
		if noEndpoint {
			topts = append(topts, otlptracegrpc.WithEndpointURL("http://localhost:4317"))
			mopts = append(mopts, otlpmetricgrpc.WithEndpointURL("http://localhost:4317"))
		}
		var lopts []otlploggrpc.Option
		if noEndpoint {
			lopts = append(lopts, otlploggrpc.WithEndpointURL("http://localhost:4317"))
		}
		if exp, err = otlptracegrpc.New(ctx, topts...); err == nil {
			mexp, err = otlpmetricgrpc.New(ctx, mopts...)
		}
		if err == nil {
			lexp, err = otlploggrpc.New(ctx, lopts...)
		}
	} else {
		var topts []otlptracehttp.Option
		var mopts []otlpmetrichttp.Option
		if noEndpoint && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
			topts = append(topts, otlptracehttp.WithEndpointURL("http://localhost:8080/v1/traces"))
		}
		if noEndpoint && os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" {
			mopts = append(mopts, otlpmetrichttp.WithEndpointURL("http://localhost:8080/v1/metrics"))
		}
		var lopts []otlploghttp.Option
		if noEndpoint && os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") == "" {
			lopts = append(lopts, otlploghttp.WithEndpointURL("http://localhost:8080/v1/logs"))
		}
		if exp, err = otlptracehttp.New(ctx, topts...); err == nil {
			mexp, err = otlpmetrichttp.New(ctx, mopts...)
		}
		if err == nil {
			lexp, err = otlploghttp.New(ctx, lopts...)
		}
	}
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(env("OTEL_SERVICE_NAME", "go-demo-api")),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(time.Second)),
		sdktrace.WithResource(res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp, sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(lexp, sdklog.WithExportInterval(time.Second))),
		sdklog.WithResource(res),
	)
	// Logs written with the request context carry its trace and span ids.
	logger = otelslog.NewLogger("go-demo-api", otelslog.WithLoggerProvider(lp))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx), lp.Shutdown(ctx))
	}, nil
}

type server struct{ db *sql.DB }

var (
	tracer = otel.Tracer("go-demo-api")
	logger = slog.Default()
)

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// orders runs a real query so the trace shows browser -> API -> SQL.
func (s *server) orders(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n < 1 || n > 100 {
		n = 5
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT g AS id, g * 42 AS total_cents FROM generate_series(1, $1) AS g`, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type order struct {
		ID         int `json:"id"`
		TotalCents int `json:"total_cents"`
	}
	out := []order{}
	for rows.Next() {
		var o order
		if err := rows.Scan(&o.ID, &o.TotalCents); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, o)
	}
	logger.InfoContext(r.Context(), "listed orders", "count", len(out))
	writeJSON(w, http.StatusOK, map[string]interface{}{"orders": out, "trace_id": trace.SpanContextFromContext(r.Context()).TraceID().String()})
}

var errDeclined = errors.New("payment declined")

// payFailureRate is the share of /api/pay calls that fail (PAY_FAILURE_RATE,
// default 0.4). Failing calls are also slow, as when a payment provider
// times out, so funnel drop-offs line up with errors and latency.
var payFailureRate = func() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("PAY_FAILURE_RATE"), 64); err == nil && v >= 0 && v <= 1 {
		return v
	}
	return 0.4
}()

// chargeCard does a lookup and calls the (simulated) payment provider.
func chargeCard(ctx context.Context, db *sql.DB, orderID int, fail bool) error {
	ctx, span := tracer.Start(ctx, "chargeCard")
	defer span.End()
	var limit int
	if err := db.QueryRowContext(ctx, `SELECT 1000 + $1::int`, orderID).Scan(&limit); err != nil {
		return err
	}
	pctx, provider := tracer.Start(ctx, "payment-provider authorize")
	logger.InfoContext(pctx, "authorizing payment", "order_id", orderID, "limit_cents", limit)
	if fail {
		time.Sleep(time.Duration(700+rand.Intn(1800)) * time.Millisecond)
	} else {
		time.Sleep(time.Duration(20+rand.Intn(80)) * time.Millisecond)
	}
	if fail {
		logger.WarnContext(pctx, "payment provider timed out", "order_id", orderID)
	}
	provider.End()
	if !fail {
		return nil
	}
	err := fmt.Errorf("%w for order %d (limit %d)", errDeclined, orderID, limit)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())
	return err
}

// pay fails for a share of calls (always with ?fail=1, never with ?fail=0).
func (s *server) pay(w http.ResponseWriter, r *http.Request) {
	orderID := int(time.Now().UnixNano()%9000) + 1000
	fail := rand.Float64() < payFailureRate
	switch r.URL.Query().Get("fail") {
	case "1":
		fail = true
	case "0":
		fail = false
	}
	if err := chargeCard(r.Context(), s.db, orderID, fail); err != nil {
		// The exception was recorded where it happened (chargeCard); the
		// request span only gets an error status.
		span := trace.SpanFromContext(r.Context())
		span.SetStatus(codes.Error, "payment failed")
		logger.ErrorContext(r.Context(), "payment failed", "order_id", orderID, "error", err.Error())
		writeJSON(w, http.StatusPaymentRequired, map[string]string{
			"error": err.Error(), "trace_id": span.SpanContext().TraceID().String(),
		})
		return
	}
	logger.InfoContext(r.Context(), "payment succeeded", "order_id", orderID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "paid"})
}

// withSession copies the browser's session and user ids from W3C baggage
// onto the server span, so backend spans can be filtered by session too.
func withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := baggage.FromContext(r.Context())
		span := trace.SpanFromContext(r.Context())
		for _, k := range []string{"session.id", "enduser.id"} {
			if v := b.Member(k).Value(); v != "" {
				span.SetAttributes(attribute.String(k, v))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// cors allows the browser demo (another origin) to call this API and to
// send trace headers.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", env("CORS_ORIGIN", "*"))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, traceparent, tracestate, baggage")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := setupTelemetry(ctx)
	if err != nil {
		log.Fatalf("telemetry: %v", err)
	}
	proto := "http/protobuf"
	if grpcExport() {
		proto = "grpc"
	}
	log.Printf("exporting OTLP over %s", proto)
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(c)
	}()

	db, err := otelsql.Open("pgx", env("DATABASE_URL", "postgres://platform:platform@localhost:5432/platform?sslmode=disable"),
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{OmitConnResetSession: true, OmitRows: true, OmitConnPrepare: true}),
	)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	s := &server{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.Handle("GET /api/orders", routeTag("/api/orders", http.HandlerFunc(s.orders)))
	mux.Handle("POST /api/pay", routeTag("/api/pay", http.HandlerFunc(s.pay)))

	handler := cors(otelhttp.NewHandler(withSession(mux), "go-demo-api",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }),
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/health" }),
	))

	addr := ":" + env("PORT", "8090")
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("go-demo-api listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(c)
}

// routeTag records the route on the server span as http.route.
func routeTag(route string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace.SpanFromContext(r.Context()).SetAttributes(semconv.HTTPRoute(route))
		h.ServeHTTP(w, r)
	})
}

// Command go-demo-api is a small HTTP API instrumented with OpenTelemetry.
// It continues the browser's trace (traceparent), records every SQL query
// as a child span via otelsql, and sends spans to the Pulse ingest gateway
// over OTLP/HTTP protobuf.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
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

func setupTracing(ctx context.Context) (func(context.Context) error, error) {
	// Honors OTEL_EXPORTER_OTLP_TRACES_ENDPOINT / OTEL_EXPORTER_OTLP_ENDPOINT;
	// defaults to the local ingest gateway.
	opts := []otlptracehttp.Option{}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		opts = append(opts, otlptracehttp.WithEndpointURL("http://localhost:8080/v1/traces"))
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(env("OTEL_SERVICE_NAME", "go-demo-api")),
		attribute.String("pulse.project_id", env("PULSE_PROJECT_ID", "1")),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp.Shutdown, nil
}

type server struct{ db *sql.DB }

var tracer = otel.Tracer("go-demo-api")

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
	writeJSON(w, http.StatusOK, map[string]interface{}{"orders": out, "trace_id": trace.SpanContextFromContext(r.Context()).TraceID().String()})
}

var errDeclined = errors.New("payment declined")

// chargeCard does a lookup, then fails, the way a real payment bug would.
func chargeCard(ctx context.Context, db *sql.DB, orderID int) error {
	ctx, span := tracer.Start(ctx, "chargeCard")
	defer span.End()
	var limit int
	if err := db.QueryRowContext(ctx, `SELECT 1000 + $1::int`, orderID).Scan(&limit); err != nil {
		return err
	}
	err := fmt.Errorf("%w for order %d (limit %d)", errDeclined, orderID, limit)
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())
	return err
}

// pay always fails: it is the demo's error endpoint.
func (s *server) pay(w http.ResponseWriter, r *http.Request) {
	orderID := int(time.Now().UnixNano()%9000) + 1000
	if err := chargeCard(r.Context(), s.db, orderID); err != nil {
		// The exception was recorded where it happened (chargeCard); the
		// request span only gets an error status.
		span := trace.SpanFromContext(r.Context())
		span.SetStatus(codes.Error, "payment failed")
		writeJSON(w, http.StatusPaymentRequired, map[string]string{
			"error": err.Error(), "trace_id": span.SpanContext().TraceID().String(),
		})
		return
	}
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

	shutdown, err := setupTracing(ctx)
	if err != nil {
		log.Fatalf("tracing: %v", err)
	}
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
	mux.Handle("GET /api/orders", otelhttp.WithRouteTag("/api/orders", http.HandlerFunc(s.orders)))
	mux.Handle("POST /api/pay", otelhttp.WithRouteTag("/api/pay", http.HandlerFunc(s.pay)))

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

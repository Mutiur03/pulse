package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/telemetry"
)

type Event struct {
	ProjectID  string                 `json:"project_id"`
	Event      string                 `json:"event"`
	Timestamp  time.Time              `json:"timestamp"`
	DistinctID string                 `json:"distinct_id,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	Properties map[string]interface{} `json:"properties"`
	TraceID    string                 `json:"trace_id,omitempty"`
	SpanID     string                 `json:"span_id,omitempty"`
}

// Span is a simplified JSON span. Full OTLP/protobuf support comes later.
type Span struct {
	ProjectID    string    `json:"project_id,omitempty"`
	TraceID      string    `json:"trace_id"`
	SpanID       string    `json:"span_id"`
	ParentSpanID string    `json:"parent_span_id,omitempty"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind,omitempty"`
	ServiceName  string    `json:"service_name,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	DurationNs   uint64    `json:"duration_ns,omitempty"`
	StatusCode   string    `json:"status_code,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
}

func main() {
	cfg := config.Load()
	cfg.ServiceName = "ingest-gateway"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Printf("Failed to initialize tracer: %v", err)
	} else {
		defer func() {
			_ = cleanup(context.Background())
		}()
	}

	producer, err := kafka.NewProducer(cfg.KafkaBootstrapServers)
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer producer.Close()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","service":"ingest-gateway"}`))
	})

	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var event Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if event.Event == "" {
			http.Error(w, "event is required", http.StatusBadRequest)
			return
		}
		if event.ProjectID == "" {
			event.ProjectID = "1"
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now()
		}

		value, err := json.Marshal(event)
		if err != nil {
			http.Error(w, "failed to marshal event", http.StatusInternalServerError)
			return
		}

		key := []byte(event.ProjectID)
		if err := producer.Produce(r.Context(), "raw.events", key, value); err != nil {
			log.Printf("Failed to produce event: %v", err)
			http.Error(w, "failed to produce event", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})

	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var spans []Span
		if err := json.NewDecoder(r.Body).Decode(&spans); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		for i := range spans {
			span := &spans[i]
			if span.TraceID == "" || span.SpanID == "" {
				http.Error(w, "trace_id and span_id are required", http.StatusBadRequest)
				return
			}
			if span.ProjectID == "" {
				span.ProjectID = "1"
			}
			if span.Timestamp.IsZero() {
				span.Timestamp = time.Now()
			}
		}

		for _, span := range spans {
			value, err := json.Marshal(span)
			if err != nil {
				http.Error(w, "failed to marshal span", http.StatusInternalServerError)
				return
			}
			if err := producer.Produce(r.Context(), "raw.spans", []byte(span.TraceID), value); err != nil {
				log.Printf("Failed to produce span: %v", err)
				http.Error(w, "failed to produce span", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})

	addr := fmt.Sprintf(":%d", cfg.IngestGatewayPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Printf("Ingest gateway listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}

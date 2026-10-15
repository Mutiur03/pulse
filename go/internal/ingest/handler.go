package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Producer is the subset of the Kafka producer the handlers need.
type Producer interface {
	Produce(ctx context.Context, topic string, key, value []byte) error
}

const (
	TopicEvents = "raw.events"
	TopicSpans  = "raw.spans"
	TopicErrors = "raw.errors"

	maxBody = 5 << 20 // 5 MiB
)

// NewHandler returns the ingest HTTP routes.
func NewHandler(p Producer) http.Handler {
	h := &handler{p: p}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy","service":"ingest-gateway"}`))
	})
	mux.HandleFunc("/v1/events", h.post(h.events))
	mux.HandleFunc("/v1/traces", h.post(h.traces))
	mux.HandleFunc("/v1/errors", h.post(h.errors))
	return cors(mux)
}

type handler struct{ p Producer }

type apiError struct {
	code int
	msg  string
}

func (h *handler) post(fn func(r *http.Request, body []byte, project string) (int, *apiError)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
		n, aerr := fn(r, body, projectID(r))
		if aerr != nil {
			http.Error(w, aerr.msg, aerr.code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "accepted", "count": n})
	}
}

// projectID comes from the X-Pulse-Project header or ?project_id=, default "1".
// Query parameters are supported because sendBeacon cannot set headers.
func projectID(r *http.Request) string {
	if v := r.Header.Get("X-Pulse-Project"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("project_id"); v != "" {
		return v
	}
	return "1"
}

// decodeOneOrMany accepts either a JSON object or an array of objects.
func decodeOneOrMany[T any](body []byte) ([]T, error) {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		var many []T
		err := json.Unmarshal(body, &many)
		return many, err
	}
	var one T
	if err := json.Unmarshal(body, &one); err != nil {
		return nil, err
	}
	return []T{one}, nil
}

func (h *handler) produceAll(ctx context.Context, topic string, keys []string, vals []interface{}) *apiError {
	for i, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			return &apiError{http.StatusInternalServerError, "failed to encode record"}
		}
		if err := h.p.Produce(ctx, topic, []byte(keys[i]), b); err != nil {
			log.Printf("produce to %s failed: %v", topic, err)
			return &apiError{http.StatusServiceUnavailable, "failed to enqueue"}
		}
	}
	return nil
}

func (h *handler) events(r *http.Request, body []byte, project string) (int, *apiError) {
	events, err := decodeOneOrMany[Event](body)
	if err != nil {
		return 0, &apiError{http.StatusBadRequest, "invalid JSON"}
	}
	keys := make([]string, len(events))
	vals := make([]interface{}, len(events))
	for i := range events {
		e := &events[i]
		if e.Event == "" {
			return 0, &apiError{http.StatusBadRequest, "event is required"}
		}
		if e.ProjectID == "" {
			e.ProjectID = project
		}
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now().UTC()
		}
		keys[i], vals[i] = e.ProjectID, e
	}
	return len(events), h.produceAll(r.Context(), TopicEvents, keys, vals)
}

func (h *handler) traces(r *http.Request, body []byte, project string) (int, *apiError) {
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "protobuf") {
		return 0, &apiError{http.StatusUnsupportedMediaType, "only OTLP/HTTP JSON is supported"}
	}
	var spans []Span
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var err error
		if spans, err = DecodeOTLPTraces(trimmed, project); err != nil {
			return 0, &apiError{http.StatusBadRequest, "invalid OTLP JSON"}
		}
	} else if err := json.Unmarshal(trimmed, &spans); err != nil {
		return 0, &apiError{http.StatusBadRequest, "invalid JSON"}
	}
	keys := make([]string, len(spans))
	vals := make([]interface{}, len(spans))
	for i := range spans {
		s := &spans[i]
		if s.TraceID == "" || s.SpanID == "" {
			return 0, &apiError{http.StatusBadRequest, "trace_id and span_id are required"}
		}
		if s.ProjectID == "" {
			s.ProjectID = project
		}
		if s.Timestamp.IsZero() {
			s.Timestamp = time.Now().UTC()
		}
		keys[i], vals[i] = s.TraceID, s
	}
	return len(spans), h.produceAll(r.Context(), TopicSpans, keys, vals)
}

func (h *handler) errors(r *http.Request, body []byte, project string) (int, *apiError) {
	errs, err := decodeOneOrMany[ErrorEvent](body)
	if err != nil {
		return 0, &apiError{http.StatusBadRequest, "invalid JSON"}
	}
	keys := make([]string, len(errs))
	vals := make([]interface{}, len(errs))
	for i := range errs {
		e := &errs[i]
		if e.Message == "" && e.ExceptionType == "" {
			return 0, &apiError{http.StatusBadRequest, "message or exception_type is required"}
		}
		if e.ProjectID == "" {
			e.ProjectID = project
		}
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now().UTC()
		}
		keys[i], vals[i] = e.ProjectID, e
	}
	return len(errs), h.produceAll(r.Context(), TopicErrors, keys, vals)
}

// cors allows browser SDKs on any origin to send data. No credentials are
// used, so a wildcard origin is safe here.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Pulse-Project")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

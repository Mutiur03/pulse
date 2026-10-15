package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/replay"
)

// Producer is the subset of the Kafka producer the handlers need.
type Producer interface {
	Produce(ctx context.Context, topic string, key, value []byte) error
}

const (
	TopicEvents = "raw.events"
	TopicSpans  = "raw.spans"
	TopicErrors = "raw.errors"
	TopicLogs   = "raw.logs"

	maxBody = 5 << 20 // 5 MiB
)

// NewHandler returns the ingest HTTP routes.
func NewHandler(p Producer, auth Authenticator, opts ...Option) http.Handler {
	h := &handler{p: p, auth: auth}
	for _, o := range opts {
		o(h)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy","service":"ingest-gateway"}`))
	})
	mux.HandleFunc("/v1/events", h.post(h.events))
	mux.HandleFunc("/v1/traces", h.post(h.traces))
	mux.HandleFunc("/v1/errors", h.post(h.errors))
	mux.HandleFunc("/v1/logs", h.post(h.logs))
	mux.HandleFunc("/v1/replay", h.post(h.replayChunk))
	return cors(mux)
}

type handler struct {
	p       Producer
	auth    Authenticator
	replays ReplaySink
}

// Option configures optional routes.
type Option func(*handler)

// ReplaySink stores session replay chunks.
type ReplaySink interface {
	Store(ctx context.Context, project uint64, c replay.Chunk) error
}

// WithReplay enables POST /v1/replay.
func WithReplay(s ReplaySink) Option { return func(h *handler) { h.replays = s } }

// ErrUnauthorized is returned by an Authenticator for unknown or revoked keys.
var ErrUnauthorized = errors.New("unauthorized")

// Authenticator maps a public ingest key to a project and enforces its rate
// limit. Allow reports whether the request fits the budget and, if not, how
// long the client should wait.
type Authenticator interface {
	Authenticate(ctx context.Context, key string) (projectID string, err error)
	Allow(ctx context.Context, key string) (bool, time.Duration)
}

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
		key := ingestKey(r)
		if key == "" {
			http.Error(w, "missing ingest key", http.StatusUnauthorized)
			return
		}
		project, err := h.auth.Authenticate(r.Context(), key)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) {
				http.Error(w, "invalid ingest key", http.StatusUnauthorized)
			} else {
				log.Printf("key lookup failed: %v", err)
				http.Error(w, "key lookup unavailable", http.StatusServiceUnavailable)
			}
			return
		}
		if ok, retry := h.auth.Allow(r.Context(), key); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		n, aerr := fn(r, body, project)
		if aerr != nil {
			http.Error(w, aerr.msg, aerr.code)
			return
		}
		if isProtobuf(r) {
			// OTLP/HTTP protobuf clients expect 200 with an empty
			// Export*ServiceResponse, which encodes to zero bytes.
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "accepted", "count": n})
	}
}

// ingestKey reads the public key from the X-Pulse-Key header or ?key=.
// The query parameter exists because navigator.sendBeacon cannot set headers.
func ingestKey(r *http.Request) string {
	if v := r.Header.Get("X-Pulse-Key"); v != "" {
		return v
	}
	return r.URL.Query().Get("key")
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
		e.ProjectID = project
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now().UTC()
		}
		keys[i], vals[i] = e.ProjectID, e
	}
	return len(events), h.produceAll(r.Context(), TopicEvents, keys, vals)
}

func isProtobuf(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Content-Type"), "protobuf")
}

func (h *handler) traces(r *http.Request, body []byte, project string) (int, *apiError) {
	var spans []Span
	var errs []ErrorEvent
	trimmed := bytes.TrimSpace(body)
	switch {
	case isProtobuf(r):
		batch, err := DecodeOTLPTraces(body, true, project)
		if err != nil {
			return 0, &apiError{http.StatusBadRequest, "invalid OTLP protobuf"}
		}
		spans, errs = batch.Spans, batch.Errors
	case len(trimmed) > 0 && trimmed[0] == '{':
		batch, err := DecodeOTLPTraces(trimmed, false, project)
		if err != nil {
			return 0, &apiError{http.StatusBadRequest, "invalid OTLP JSON"}
		}
		spans, errs = batch.Spans, batch.Errors
	default:
		// Simple JSON array of spans.
		if err := json.Unmarshal(trimmed, &spans); err != nil {
			return 0, &apiError{http.StatusBadRequest, "invalid JSON"}
		}
	}
	keys := make([]string, len(spans))
	vals := make([]interface{}, len(spans))
	for i := range spans {
		s := &spans[i]
		if s.TraceID == "" || s.SpanID == "" {
			return 0, &apiError{http.StatusBadRequest, "trace_id and span_id are required"}
		}
		s.ProjectID = project
		if s.Timestamp.IsZero() {
			s.Timestamp = time.Now().UTC()
		}
		keys[i], vals[i] = s.TraceID, s
	}
	if aerr := h.produceAll(r.Context(), TopicSpans, keys, vals); aerr != nil {
		return 0, aerr
	}
	ekeys := make([]string, len(errs))
	evals := make([]interface{}, len(errs))
	for i := range errs {
		ekeys[i], evals[i] = errs[i].ProjectID, &errs[i]
	}
	return len(spans), h.produceAll(r.Context(), TopicErrors, ekeys, evals)
}

func (h *handler) logs(r *http.Request, body []byte, project string) (int, *apiError) {
	recs, err := DecodeOTLPLogs(bytes.TrimSpace(body), isProtobuf(r), project)
	if err != nil {
		return 0, &apiError{http.StatusBadRequest, "invalid OTLP logs payload"}
	}
	keys := make([]string, len(recs))
	vals := make([]interface{}, len(recs))
	for i := range recs {
		keys[i], vals[i] = recs[i].ProjectID, &recs[i]
	}
	return len(recs), h.produceAll(r.Context(), TopicLogs, keys, vals)
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
		e.ProjectID = project
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Pulse-Key")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// replay accepts one rrweb chunk: ?enc=gzip for a gzip body (the SDK keeps
// Content-Type text/plain so the request stays CORS-simple), else JSON.
func (h *handler) replayChunk(r *http.Request, body []byte, project string) (int, *apiError) {
	if h.replays == nil {
		return 0, &apiError{http.StatusServiceUnavailable, "replay storage is not configured"}
	}
	if len(body) > replay.MaxCompressed {
		return 0, &apiError{http.StatusRequestEntityTooLarge, "replay chunk too large"}
	}
	c, err := replay.Decode(body, r.URL.Query().Get("enc") == "gzip")
	var inv *replay.ErrInvalid
	if errors.As(err, &inv) {
		return 0, &apiError{http.StatusBadRequest, inv.Msg}
	} else if err != nil {
		return 0, &apiError{http.StatusBadRequest, "invalid chunk"}
	}
	pid, err := strconv.ParseUint(project, 10, 64)
	if err != nil {
		return 0, &apiError{http.StatusInternalServerError, "bad project"}
	}
	if err := h.replays.Store(r.Context(), pid, c); err != nil {
		log.Printf("replay store failed: %v", err)
		return 0, &apiError{http.StatusServiceUnavailable, "replay storage unavailable"}
	}
	return len(c.Events), nil
}

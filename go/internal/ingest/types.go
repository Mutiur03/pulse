// Package ingest holds the wire types and decoders used by the ingest gateway.
package ingest

import "time"

// Event is a product analytics event.
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

// Span is the internal span record written to the raw.spans topic.
type Span struct {
	ProjectID     string    `json:"project_id,omitempty"`
	TraceID       string    `json:"trace_id"`
	SpanID        string    `json:"span_id"`
	ParentSpanID  string    `json:"parent_span_id,omitempty"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind,omitempty"`
	ServiceName   string    `json:"service_name,omitempty"`
	Platform      string    `json:"platform,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	DurationNs    uint64    `json:"duration_ns,omitempty"`
	StatusCode    string    `json:"status_code,omitempty"`
	StatusMessage string    `json:"status_message,omitempty"`
	HTTPRoute     string    `json:"http_route,omitempty"`
	HTTPStatus    uint16    `json:"http_status,omitempty"`
	DBSystem      string    `json:"db_system,omitempty"`
	DBStatement   string    `json:"db_statement,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	// Attributes holds the span's attributes plus a few resource attributes
	// (prefixed "resource.").
	Attributes map[string]string `json:"attributes,omitempty"`
	Events     []SpanEvent       `json:"events,omitempty"`
}

// SpanEvent is a non-exception event recorded on a span.
type SpanEvent struct {
	Name       string            `json:"name"`
	Time       time.Time         `json:"time"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// ErrorEvent is a captured exception.
type ErrorEvent struct {
	ProjectID     string    `json:"project_id,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	Platform      string    `json:"platform,omitempty"`
	ExceptionType string    `json:"exception_type,omitempty"`
	Message       string    `json:"message"`
	Stack         string    `json:"stack,omitempty"`
	Fingerprint   string    `json:"fingerprint,omitempty"`
	Release       string    `json:"release,omitempty"`
	Environment   string    `json:"environment,omitempty"`
	ServiceName   string    `json:"service_name,omitempty"`
	TraceID       string    `json:"trace_id,omitempty"`
	SpanID        string    `json:"span_id,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	DistinctID    string    `json:"distinct_id,omitempty"`
	URL           string    `json:"url,omitempty"`
}

// LogRecord is one log line written to the raw.logs topic.
type LogRecord struct {
	ProjectID   string    `json:"project_id"`
	Timestamp   time.Time `json:"timestamp"`
	TraceID     string    `json:"trace_id,omitempty"`
	SpanID      string    `json:"span_id,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	ServiceName string    `json:"service_name,omitempty"`
	Body        string    `json:"body"`
	Attributes  string    `json:"attributes"`
}

package ch

import (
	"context"
	"time"
)

// SpanRecord is a span as stored in ClickHouse.
type SpanRecord struct {
	Timestamp     time.Time         `ch:"timestamp"`
	TraceID       string            `ch:"trace_id"`
	SpanID        string            `ch:"span_id"`
	ParentSpanID  string            `ch:"parent_span_id"`
	Name          string            `ch:"span_name"`
	Kind          string            `ch:"span_kind"`
	ServiceName   string            `ch:"service_name"`
	Platform      string            `ch:"platform"`
	DurationNs    uint64            `ch:"duration_ns"`
	StatusCode    string            `ch:"status_code"`
	StatusMessage string            `ch:"status_message"`
	HTTPRoute     string            `ch:"http_route"`
	HTTPStatus    uint16            `ch:"http_status"`
	DBSystem      string            `ch:"db_system"`
	DBStatement   string            `ch:"db_statement"`
	SessionID     string            `ch:"session_id"`
	Attributes    map[string]string `ch:"attributes"`
	SpanEvents    string            `ch:"span_events"`
}

// ErrorRecord is an error as stored in ClickHouse.
type ErrorRecord struct {
	Timestamp     time.Time `ch:"timestamp"`
	IssueID       uint64    `ch:"issue_id"`
	ExceptionType string    `ch:"exception_type"`
	Message       string    `ch:"message"`
	Stack         string    `ch:"stack"`
	Culprit       string    `ch:"culprit"`
	TraceID       string    `ch:"trace_id"`
	SpanID        string    `ch:"span_id"`
	ServiceName   string    `ch:"service_name"`
	Platform      string    `ch:"platform"`
	SessionID     string    `ch:"session_id"`
	DistinctID    string    `ch:"distinct_id"`
	URL           string    `ch:"url"`
	Release       string    `ch:"release"`
	Frames        string    `ch:"frames"`
	DebugIDs      string    `ch:"debug_ids"`
	MappedFrames  uint16    `ch:"mapped_frames"`
}

// EventRecord is a product event as stored in ClickHouse.
type EventRecord struct {
	Event      string    `ch:"event"`
	Timestamp  time.Time `ch:"timestamp"`
	DistinctID string    `ch:"distinct_id"`
	SessionID  string    `ch:"session_id"`
	SpanID     string    `ch:"span_id"`
	Properties string    `ch:"properties"`
}

// MaxTraceSpans bounds how much of a runaway trace is loaded.
const MaxTraceSpans = 10000

const spanCols = `timestamp, trace_id, span_id, parent_span_id, span_name, span_kind, service_name, platform,
	duration_ns, status_code, status_message, http_route, http_status, db_system, db_statement, session_id, attributes, span_events`

const errorCols = `timestamp, issue_id, exception_type, message, stack, culprit, trace_id, span_id,
	service_name, platform, session_id, distinct_id, url, release, frames, debug_ids, mapped_frames`

func (c *Client) SpansByTrace(ctx context.Context, project uint64, traceID string) ([]SpanRecord, error) {
	var out []SpanRecord
	err := c.conn.Select(ctx, &out, `SELECT `+spanCols+` FROM spans
		WHERE project_id = ? AND trace_id = ?
		ORDER BY timestamp LIMIT 1 BY span_id LIMIT ?`, project, traceID, MaxTraceSpans)
	return out, err
}

func (c *Client) ErrorsByTrace(ctx context.Context, project uint64, traceID string) ([]ErrorRecord, error) {
	var out []ErrorRecord
	err := c.conn.Select(ctx, &out, `SELECT `+errorCols+` FROM errors
		WHERE project_id = ? AND trace_id = ? ORDER BY timestamp LIMIT 1000`, project, traceID)
	return out, err
}

func (c *Client) EventsByTrace(ctx context.Context, project uint64, traceID string) ([]EventRecord, error) {
	var out []EventRecord
	err := c.conn.Select(ctx, &out, `SELECT event, timestamp, distinct_id, session_id, span_id, properties FROM events
		WHERE project_id = ? AND trace_id = ? ORDER BY timestamp LIMIT 1000`, project, traceID)
	return out, err
}

func (c *Client) ErrorsByIssue(ctx context.Context, project, issueID uint64, limit int) ([]ErrorRecord, error) {
	var out []ErrorRecord
	err := c.conn.Select(ctx, &out, `SELECT `+errorCols+` FROM errors
		WHERE project_id = ? AND issue_id = ? ORDER BY timestamp DESC LIMIT ?`, project, issueID, limit)
	return out, err
}

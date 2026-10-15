package ch

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Client struct {
	conn driver.Conn
}

func NewClient(addr string) (*Client, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	return &Client{conn: conn}, nil
}

func (c *Client) InsertEvents(ctx context.Context, events []map[string]interface{}) error {
	if len(events) == 0 {
		return nil
	}

	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO events (project_id, event, timestamp, distinct_id, session_id, trace_id, span_id, properties)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}

	for _, event := range events {
		projectID := uint64(1)
		if pid, ok := event["project_id"].(string); ok {
			if id, err := strconv.ParseUint(pid, 10, 64); err == nil {
				projectID = id
			}
		}

		eventName := ""
		if e, ok := event["event"].(string); ok {
			eventName = e
		}

		timestamp := time.Now()
		if ts, ok := event["timestamp"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				timestamp = parsed
			}
		}

		distinctID := ""
		if did, ok := event["distinct_id"].(string); ok {
			distinctID = did
		}

		sessionID := ""
		if sid, ok := event["session_id"].(string); ok {
			sessionID = sid
		}

		traceID := ""
		if tid, ok := event["trace_id"].(string); ok {
			traceID = tid
		}

		propsJSON := "{}"
		if props, ok := event["properties"]; ok {
			if b, err := json.Marshal(props); err == nil {
				propsJSON = string(b)
			}
		}

		if err := batch.Append(projectID, eventName, timestamp, distinctID, sessionID, traceID, str(event, "span_id"), propsJSON); err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("failed to send batch: %w", err)
	}

	return nil
}

func (c *Client) InsertSpans(ctx context.Context, spans []map[string]interface{}) error {
	if len(spans) == 0 {
		return nil
	}

	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO spans (project_id, timestamp, trace_id, span_id, parent_span_id, span_name, span_kind, service_name, platform, duration_ns, status_code, status_message, http_route, http_status, db_system, db_statement, session_id, attributes, span_events)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}

	for _, span := range spans {
		projectID := uint64(1)
		if pid, ok := span["project_id"].(string); ok {
			if id, err := strconv.ParseUint(pid, 10, 64); err == nil {
				projectID = id
			}
		}

		timestamp := time.Now()
		if ts, ok := span["timestamp"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				timestamp = parsed
			}
		}

		var duration uint64
		if d, ok := span["duration_ns"].(float64); ok && d > 0 {
			duration = uint64(d)
		}

		if err := batch.Append(
			projectID,
			timestamp,
			str(span, "trace_id"),
			str(span, "span_id"),
			str(span, "parent_span_id"),
			str(span, "name"),
			str(span, "kind"),
			str(span, "service_name"),
			str(span, "platform"),
			duration,
			str(span, "status_code"),
			str(span, "status_message"),
			str(span, "http_route"),
			uint16(num(span, "http_status")),
			str(span, "db_system"),
			str(span, "db_statement"),
			str(span, "session_id"),
			strMap(span["attributes"]),
			jsonArray(span["events"]),
		); err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("failed to send batch: %w", err)
	}

	return nil
}

func num(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok && v > 0 {
		return v
	}
	return 0
}

func projectOf(m map[string]interface{}) uint64 {
	if pid, ok := m["project_id"].(string); ok {
		if id, err := strconv.ParseUint(pid, 10, 64); err == nil {
			return id
		}
	}
	return 1
}

func timeOf(m map[string]interface{}) time.Time {
	if ts, ok := m["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			return parsed
		}
	}
	return time.Now()
}

// ErrorRow is one error, already grouped into an issue.
type ErrorRow struct {
	ProjectID     uint64
	Timestamp     time.Time
	IssueID       uint64
	Fingerprint   string
	Title         string
	Culprit       string
	Platform      string
	ServiceName   string
	ExceptionType string
	Message       string
	Stack         string
	// Frames is the symbolicated stack as JSON; MappedFrames counts frames
	// resolved through a source map.
	Frames       string
	DebugIDs     string
	MappedFrames uint16
	Release      string
	Environment  string
	TraceID      string
	SpanID       string
	SessionID    string
	DistinctID   string
	URL          string
}

// InsertErrors writes grouped errors to the errors table.
func (c *Client) InsertErrors(ctx context.Context, rows []ErrorRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO errors (project_id, timestamp, issue_id, fingerprint, title, culprit, platform, service_name, exception_type, message, stack, frames, debug_ids, mapped_frames, release, environment, trace_id, span_id, session_id, distinct_id, url)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}
	for _, e := range rows {
		if err := batch.Append(e.ProjectID, e.Timestamp, e.IssueID, e.Fingerprint, e.Title, e.Culprit, e.Platform,
			e.ServiceName, e.ExceptionType, e.Message, e.Stack, e.Frames, e.DebugIDs, e.MappedFrames, e.Release, e.Environment, e.TraceID, e.SpanID,
			e.SessionID, e.DistinctID, e.URL); err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}
	return batch.Send()
}

// InsertLogs writes OTLP log records to the logs table.
func (c *Client) InsertLogs(ctx context.Context, logs []map[string]interface{}) error {
	if len(logs) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO logs (project_id, timestamp, trace_id, span_id, severity, service_name, body, attributes)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}
	for _, l := range logs {
		attrs := str(l, "attributes")
		if attrs == "" {
			attrs = "{}"
		}
		if err := batch.Append(projectOf(l), timeOf(l), str(l, "trace_id"), str(l, "span_id"), str(l, "severity"),
			str(l, "service_name"), str(l, "body"), attrs); err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}
	return batch.Send()
}

// ProjectOf and TimeOf parse the common fields of a decoded record.
func ProjectOf(m map[string]interface{}) uint64     { return projectOf(m) }
func TimeOf(m map[string]interface{}) time.Time     { return timeOf(m) }
func Str(m map[string]interface{}, k string) string { return str(m, k) }

func str(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func strMap(v interface{}) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]interface{}); ok {
		for k, x := range m {
			if s, ok := x.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func jsonArray(v interface{}) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

package ch

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
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

	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO events (project_id, event, timestamp, distinct_id, session_id, trace_id, properties)")
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

		if err := batch.Append(projectID, eventName, timestamp, distinctID, sessionID, traceID, propsJSON); err != nil {
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

	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO spans (project_id, timestamp, trace_id, span_id, parent_span_id, span_name, span_kind, service_name, duration_ns, status_code, status_message, http_route, http_status, db_system, db_statement, session_id)")
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
			duration,
			str(span, "status_code"),
			str(span, "status_message"),
			str(span, "http_route"),
			uint16(num(span, "http_status")),
			str(span, "db_system"),
			str(span, "db_statement"),
			str(span, "session_id"),
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

// Fingerprint groups errors into issues: exception type plus the first
// stack frame, falling back to the message when there is no stack.
func Fingerprint(excType, message, stack string) string {
	frame := ""
	for _, line := range strings.Split(stack, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "at ") || strings.Contains(line, "@") {
			frame = line
			break
		}
	}
	key := excType + "|" + frame
	if frame == "" {
		key = excType + "|" + message
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:])
}

// InsertErrors writes captured exceptions to the errors table.
func (c *Client) InsertErrors(ctx context.Context, errs []map[string]interface{}) error {
	if len(errs) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO errors (project_id, timestamp, issue_id, fingerprint, platform, exception_type, message, stack, release, environment, trace_id, span_id, session_id, distinct_id, url)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}
	for _, e := range errs {
		fp := str(e, "fingerprint")
		if fp == "" {
			fp = Fingerprint(str(e, "exception_type"), str(e, "message"), str(e, "stack"))
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(fp))
		if err := batch.Append(
			projectOf(e), timeOf(e), h.Sum64(), fp,
			str(e, "platform"), str(e, "exception_type"), str(e, "message"), str(e, "stack"),
			str(e, "release"), str(e, "environment"), str(e, "trace_id"), str(e, "span_id"),
			str(e, "session_id"), str(e, "distinct_id"), str(e, "url"),
		); err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("failed to send batch: %w", err)
	}
	return nil
}

func str(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (c *Client) Close() error {
	return c.conn.Close()
}

package ch

import (
	"context"
	"fmt"
	"time"

	"github.com/Mutiur03/pulse/internal/logquery"
)

// LogFilter selects logs in [From, To) that match Query.
type LogFilter struct {
	Project  uint64
	From, To time.Time
	Query    logquery.Query
	// Before pages back from an older result: only logs strictly older.
	Before time.Time
	// After returns logs strictly newer, for live tail.
	After time.Time
	Limit int
}

// LogRow is one stored log record.
type LogRow struct {
	Timestamp  time.Time `ch:"timestamp"`
	TraceID    string    `ch:"trace_id"`
	SpanID     string    `ch:"span_id"`
	Severity   string    `ch:"severity"`
	Service    string    `ch:"service_name"`
	Body       string    `ch:"body"`
	Attributes string    `ch:"attributes"`
}

// LogsWhere builds the condition shared by the logs queries.
func LogsWhere(f LogFilter) (string, []interface{}) {
	where := "project_id = ? AND timestamp >= ? AND timestamp < ?"
	args := []interface{}{f.Project, f.From, f.To}
	if !f.Before.IsZero() {
		where, args = where+" AND timestamp < ?", append(args, f.Before)
	}
	if !f.After.IsZero() {
		where, args = where+" AND timestamp > ?", append(args, f.After)
	}
	q, qa := f.Query.Where()
	return where + " AND " + q, append(args, qa...)
}

// SearchLogs returns the newest matching logs first.
func (c *Client) SearchLogs(ctx context.Context, f LogFilter) ([]LogRow, error) {
	where, args := LogsWhere(f)
	var out []LogRow
	err := c.conn.Select(ctx, &out, `SELECT timestamp, trace_id, span_id, severity, service_name, body, attributes
		FROM logs WHERE `+where+` ORDER BY timestamp DESC LIMIT ?`, append(args, f.Limit)...)
	return out, err
}

// LogBucket is the count of one severity in one time bucket.
type LogBucket struct {
	Start    time.Time `ch:"b"`
	Severity string    `ch:"severity"`
	Count    uint64    `ch:"n"`
}

// LogHistogram counts matching logs per bucket and severity.
func (c *Client) LogHistogram(ctx context.Context, f LogFilter, bucket time.Duration) ([]LogBucket, error) {
	where, args := LogsWhere(f)
	var out []LogBucket
	err := c.conn.Select(ctx, &out, fmt.Sprintf(`SELECT toDateTime(toStartOfInterval(timestamp, INTERVAL %d SECOND)) AS b, severity, count() AS n
		FROM logs WHERE %s GROUP BY b, severity ORDER BY b`, int64(bucket/time.Second), where), args...)
	return out, err
}

// LogFacet is a value of a field with its count.
type LogFacet struct {
	Field string `ch:"field"`
	Value string `ch:"value"`
	Count uint64 `ch:"n"`
}

// LogFacets counts matching logs by service and by severity.
func (c *Client) LogFacets(ctx context.Context, f LogFilter) ([]LogFacet, error) {
	where, args := LogsWhere(f)
	var out []LogFacet
	err := c.conn.Select(ctx, &out, `SELECT 'service' AS field, toString(service_name) AS value, count() AS n FROM logs WHERE `+where+`
		GROUP BY value ORDER BY n DESC LIMIT 20
		UNION ALL
		SELECT 'level' AS field, toString(severity) AS value, count() AS n FROM logs WHERE `+where+`
		GROUP BY value ORDER BY n DESC LIMIT 10`, append(args, args...)...)
	return out, err
}

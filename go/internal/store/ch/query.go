package ch

import (
	"context"
	"strings"
	"time"
)

// TraceFilter selects traces for the trace search page.
type TraceFilter struct {
	Project       uint64
	From, To      time.Time
	Service       string
	Status        string // "", "ok" or "error"
	MinDurationNs uint64
	MaxDurationNs uint64
	NameQuery     string
	Limit         int
}

// TraceSummaryRow is one row of the trace search.
type TraceSummaryRow struct {
	TraceID     string    `ch:"trace_id"`
	RootName    string    `ch:"root_name"`
	RootService string    `ch:"root_service"`
	HTTPStatus  uint16    `ch:"root_http_status"`
	Services    []string  `ch:"services"`
	StartTime   time.Time `ch:"trace_start"`
	DurationNs  int64     `ch:"trace_duration_ns"`
	SpanCount   uint64    `ch:"span_count"`
	ErrorCount  uint64    `ch:"error_count"`
	Browser     uint64    `ch:"browser_spans"`
}

// TracesSQL builds the trace search query. It reads trace_summaries, which a
// materialized view keeps with one row per trace and hour, and merges a
// trace's rows. The root is the earliest span without a parent (or the
// earliest span when the root was not received). Every user value is a bound
// parameter.
func TracesSQL(f TraceFilter) (string, []interface{}) {
	// Hours are truncated, so the hour filter is widened and the exact range
	// applied to the merged start time.
	args := []interface{}{f.Project, f.From, f.To}
	having := []string{"trace_start >= ?", "trace_start < ?"}
	args = append(args, f.From, f.To)
	if f.Service != "" {
		having = append(having, "has(services, ?)")
		args = append(args, f.Service)
	}
	switch f.Status {
	case "error":
		having = append(having, "error_count > 0")
	case "ok":
		having = append(having, "error_count = 0")
	}
	if f.MinDurationNs > 0 {
		having = append(having, "trace_duration_ns >= ?")
		args = append(args, f.MinDurationNs)
	}
	if f.MaxDurationNs > 0 {
		having = append(having, "trace_duration_ns <= ?")
		args = append(args, f.MaxDurationNs)
	}
	if f.NameQuery != "" {
		having = append(having, "arrayExists(n -> positionCaseInsensitive(n, ?) > 0, names)")
		args = append(args, f.NameQuery)
	}
	q := `SELECT trace_id, root_t.1 AS root_name, root_t.2 AS root_service, root_t.3 AS root_http_status,
		services, trace_start, trace_duration_ns, span_count, error_count, browser_spans
	FROM (
		SELECT trace_id,
			argMinMerge(root) AS root_t,
			arraySort(groupUniqArrayArray(services)) AS services,
			groupUniqArrayArray(span_names) AS names,
			min(start) AS trace_start,
			toInt64(max(end_ns) - toInt64(toUnixTimestamp64Nano(min(start)))) AS trace_duration_ns,
			sum(span_count) AS span_count,
			sum(error_count) AS error_count,
			sum(browser_spans) AS browser_spans
		FROM trace_summaries
		WHERE project_id = ? AND hour >= toStartOfHour(toDateTime(?, 'UTC')) AND hour < ?
		GROUP BY trace_id
		HAVING ` + strings.Join(having, " AND ") + `
		ORDER BY trace_start DESC LIMIT ?
	)
	ORDER BY trace_start DESC`
	args = append(args, f.Limit)
	return q, args
}

// recentTraceWindow is the end of the range searched first.
const recentTraceWindow = 2 * time.Hour

// ListTraces returns the newest traces matching f. For long ranges it first
// searches only the last recentTraceWindow: when that finds a full page, no
// older trace can rank above them, so the rest of the range is not read.
func (c *Client) ListTraces(ctx context.Context, f TraceFilter) ([]TraceSummaryRow, error) {
	if f.Limit > 0 && f.To.Sub(f.From) > 2*recentTraceWindow {
		recent := f
		recent.From = f.To.Add(-recentTraceWindow)
		q, args := TracesSQL(recent)
		var out []TraceSummaryRow
		if err := c.conn.Select(ctx, &out, q, args...); err != nil {
			return nil, err
		}
		if len(out) >= f.Limit {
			return out, nil
		}
	}
	q, args := TracesSQL(f)
	var out []TraceSummaryRow
	err := c.conn.Select(ctx, &out, q, args...)
	return out, err
}

func (c *Client) Services(ctx context.Context, project uint64, from, to time.Time) ([]string, error) {
	var rows []struct {
		Name string `ch:"service_name"`
	}
	err := c.conn.Select(ctx, &rows, `SELECT DISTINCT service_name FROM spans
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND service_name != ''
		ORDER BY service_name LIMIT 500`, project, from, to)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out, err
}

// EventFilter selects product events for the events explorer.
type EventFilter struct {
	Project           uint64
	From, To          time.Time
	Event, DistinctID string
	SessionID         string
	Limit             int
}

// ExploredEvent is an event with its trace id.
type ExploredEvent struct {
	EventRecord
	TraceID string `ch:"trace_id"`
}

// EventsSQL builds the events explorer query. Sorting every row in the
// range by time would read the wide properties column of all of them, so a
// subquery first finds the timestamp of the limit-th newest event reading
// only the timestamp (and filter) columns, and the outer query reads full
// rows from that point on.
func EventsSQL(f EventFilter) (string, []interface{}) {
	where := "project_id = ? AND timestamp >= ? AND timestamp < ?"
	args := []interface{}{f.Project, f.From, f.To}
	for _, c := range []struct{ col, v string }{{"event", f.Event}, {"distinct_id", f.DistinctID}, {"session_id", f.SessionID}} {
		if c.v != "" {
			where += " AND " + c.col + " = ?"
			args = append(args, c.v)
		}
	}
	all := append(append([]interface{}{}, args...), args...)
	all = append(all, f.Limit, f.Limit)
	return `SELECT event, timestamp, distinct_id, session_id, span_id, trace_id, properties FROM events
		WHERE ` + where + ` AND timestamp >= (
			SELECT ifNull(min(t), toDateTime64(0, 3, 'UTC')) FROM (
				SELECT timestamp AS t FROM events WHERE ` + where + ` ORDER BY timestamp DESC LIMIT ?
			)
		)
		ORDER BY timestamp DESC LIMIT ?`, all
}

func (c *Client) ListEvents(ctx context.Context, f EventFilter) ([]ExploredEvent, error) {
	q, args := EventsSQL(f)
	var out []ExploredEvent
	err := c.conn.Select(ctx, &out, q, args...)
	return out, err
}

// EventNameCount is an event name and how often it occurred.
type EventNameCount struct {
	Name  string `ch:"event"`
	Count uint64 `ch:"n"`
}

func (c *Client) EventNames(ctx context.Context, project uint64, from, to time.Time) ([]EventNameCount, error) {
	var out []EventNameCount
	err := c.conn.Select(ctx, &out, `SELECT event, count() AS n FROM events
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ?
		GROUP BY event ORDER BY n DESC LIMIT 200`, project, from, to)
	return out, err
}

// IssueTrends returns hourly occurrence counts for the last 24 hours ending
// at now, oldest first, for each issue id.
func (c *Client) IssueTrends(ctx context.Context, project uint64, ids []uint64, now time.Time) (map[uint64][]int64, error) {
	out := make(map[uint64][]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	end := now.UTC().Truncate(time.Hour).Add(time.Hour)
	start := end.Add(-24 * time.Hour)
	var rows []struct {
		IssueID uint64    `ch:"issue_id"`
		Hour    time.Time `ch:"h"`
		N       uint64    `ch:"n"`
	}
	err := c.conn.Select(ctx, &rows, `SELECT issue_id, toStartOfHour(timestamp) AS h, count() AS n FROM errors
		WHERE project_id = ? AND issue_id IN (?) AND timestamp >= ? AND timestamp < ?
		GROUP BY issue_id, h`, project, ids, start, end)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = make([]int64, 24)
	}
	for _, r := range rows {
		i := int(r.Hour.UTC().Sub(start) / time.Hour)
		if b, ok := out[r.IssueID]; ok && i >= 0 && i < 24 {
			b[i] += int64(r.N)
		}
	}
	return out, nil
}

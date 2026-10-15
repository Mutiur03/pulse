package ch

import (
	"context"
	"fmt"
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

// TracesSQL builds the trace search query. Spans are grouped by trace; the
// root is the earliest span without a parent (or the earliest span when the
// root was not received). Every user value is a bound parameter.
func TracesSQL(f TraceFilter) (string, []interface{}) {
	args := []interface{}{f.Project, f.From, f.To}
	var having []string
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
		having = append(having, "countIf(positionCaseInsensitive(span_name, ?) > 0) > 0")
		args = append(args, f.NameQuery)
	}
	q := `SELECT trace_id,
		argMin(span_name, (parent_span_id != '', timestamp)) AS root_name,
		argMin(service_name, (parent_span_id != '', timestamp)) AS root_service,
		argMin(http_status, (parent_span_id != '', timestamp)) AS root_http_status,
		arraySort(groupUniqArray(service_name)) AS services,
		min(timestamp) AS trace_start,
		toInt64(max(toUnixTimestamp64Nano(timestamp) + toInt64(duration_ns)) - min(toUnixTimestamp64Nano(timestamp))) AS trace_duration_ns,
		uniqExact(span_id) AS span_count,
		countIf(status_code = 'error') AS error_count,
		countIf(platform = 'webjs') AS browser_spans
	FROM spans
	WHERE project_id = ? AND timestamp >= ? AND timestamp < ?
	GROUP BY trace_id`
	if len(having) > 0 {
		q += "\n\tHAVING " + strings.Join(having, " AND ")
	}
	q += "\n\tORDER BY trace_start DESC LIMIT ?"
	args = append(args, f.Limit)
	return q, args
}

func (c *Client) ListTraces(ctx context.Context, f TraceFilter) ([]TraceSummaryRow, error) {
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

func (c *Client) ListEvents(ctx context.Context, f EventFilter) ([]ExploredEvent, error) {
	q := `SELECT event, timestamp, distinct_id, session_id, span_id, trace_id, properties FROM events
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ?`
	args := []interface{}{f.Project, f.From, f.To}
	for col, v := range map[string]string{"event": f.Event, "distinct_id": f.DistinctID, "session_id": f.SessionID} {
		if v != "" {
			q += " AND " + col + " = ?"
			args = append(args, v)
		}
	}
	q += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, f.Limit)
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

// FunnelQuery is a validated funnel definition.
type FunnelQuery struct {
	Project       uint64
	Steps         []string
	WindowSeconds int
	From, To      time.Time
}

// Validate checks the step count and window, which are interpolated as
// windowFunnel parameters (ClickHouse requires literals there).
func (f FunnelQuery) Validate() error {
	if len(f.Steps) < 2 || len(f.Steps) > 5 {
		return fmt.Errorf("a funnel needs 2 to 5 steps")
	}
	for _, s := range f.Steps {
		if s == "" {
			return fmt.Errorf("step event names must not be empty")
		}
	}
	if f.WindowSeconds < 1 || f.WindowSeconds > 30*24*3600 {
		return fmt.Errorf("window must be between 1 second and 30 days")
	}
	if !f.To.After(f.From) {
		return fmt.Errorf("time range is empty")
	}
	return nil
}

// funnelCTE defines, for the funnel:
//
//	levels:   furthest step (0..n) each user reached in order within the window
//	cohort:   one row per (step s, user) for users who reached s and s < n,
//	          kind 'dropped' if they stopped at s, else 'continued'
//	sessions: the sessions of every user who entered the funnel
//	cs:       cohort rows expanded to the users' sessions
func funnelCTE(f FunnelQuery) (string, []interface{}) {
	conds := make([]string, len(f.Steps))
	args := []interface{}{}
	for i, s := range f.Steps {
		conds[i] = "event = ?"
		args = append(args, s)
	}
	args = append(args, f.Project, f.From, f.To, f.Steps)
	q := fmt.Sprintf(`WITH
	levels AS (
		SELECT distinct_id, windowFunnel(%d)(toDateTime(timestamp), %s) AS level
		FROM events
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND distinct_id != '' AND event IN (?)
		GROUP BY distinct_id
	),
	cohort AS (
		SELECT distinct_id, s AS step, if(level = s, 'dropped', 'continued') AS kind
		FROM levels ARRAY JOIN range(1, %d) AS s
		WHERE level >= s
	),
	sessions AS (
		SELECT DISTINCT distinct_id, session_id FROM events
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND session_id != ''
			AND distinct_id IN (SELECT distinct_id FROM levels WHERE level >= 1)
	),
	cs AS (
		SELECT c.step AS step, c.kind AS kind, c.distinct_id AS distinct_id, s.session_id AS session_id
		FROM cohort AS c INNER JOIN sessions AS s ON c.distinct_id = s.distinct_id
	)
`, f.WindowSeconds, strings.Join(conds, ", "), len(f.Steps))
	args = append(args, f.Project, f.From, f.To)
	return q, args
}

// FunnelLevelsSQL counts users by the furthest step reached.
func FunnelLevelsSQL(f FunnelQuery) (string, []interface{}) {
	cte, args := funnelCTE(f)
	return cte + `SELECT level, count() AS users FROM levels GROUP BY level ORDER BY level`, args
}

// FunnelHealthSQL reports, per step and cohort, session counts, sessions
// with errors and the p95 duration of backend server spans.
func FunnelHealthSQL(f FunnelQuery) (string, []interface{}) {
	cte, args := funnelCTE(f)
	q := cte + `,
	errs AS (
		SELECT DISTINCT session_id FROM errors
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND session_id IN (SELECT session_id FROM sessions)
	),
	lat AS (
		SELECT session_id, duration_ns FROM spans
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ?
			AND span_kind = 'server' AND platform != 'webjs'
			AND session_id IN (SELECT session_id FROM sessions)
	),
	base AS (
		SELECT step, kind, uniqExact(distinct_id) AS users, uniqExact(session_id) AS sessions,
			uniqExactIf(session_id, session_id IN (SELECT session_id FROM errs)) AS error_sessions
		FROM cs GROUP BY step, kind
	),
	p AS (
		SELECT cs.step AS step, cs.kind AS kind, quantile(0.95)(lat.duration_ns) AS p95
		FROM (SELECT DISTINCT step, kind, session_id FROM cs) AS cs
		INNER JOIN lat ON cs.session_id = lat.session_id
		GROUP BY step, kind
	)
	SELECT base.step AS step, base.kind AS kind, base.users AS users, base.sessions AS sessions,
		base.error_sessions AS error_sessions, ifNull(p.p95, 0) AS p95_ns
	FROM base LEFT JOIN p ON base.step = p.step AND base.kind = p.kind
	ORDER BY step, kind
	SETTINGS join_use_nulls = 1`
	args = append(args, f.Project, f.From, f.To, f.Project, f.From, f.To)
	return q, args
}

// FunnelErrorsSQL lists the top issues per step and cohort, ranked by the
// number of affected users.
func FunnelErrorsSQL(f FunnelQuery, perCohort int) (string, []interface{}) {
	cte, args := funnelCTE(f)
	q := cte + `,
	errs AS (
		SELECT session_id, issue_id, title, exception_type, service_name FROM errors
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND issue_id != 0
			AND session_id IN (SELECT session_id FROM sessions)
	)
	SELECT cs.step AS step, cs.kind AS kind, errs.issue_id AS issue_id, any(errs.title) AS title,
		any(errs.exception_type) AS exception_type, any(errs.service_name) AS service_name,
		uniqExact(cs.distinct_id) AS users, count() AS n
	FROM cs INNER JOIN errs ON cs.session_id = errs.session_id
	GROUP BY step, kind, issue_id
	ORDER BY step, kind, users DESC, n DESC
	LIMIT ? BY step, kind`
	args = append(args, f.Project, f.From, f.To, perCohort)
	return q, args
}

// FunnelLevel is the number of users whose furthest step is Level.
type FunnelLevel struct {
	Level uint8  `ch:"level"`
	Users uint64 `ch:"users"`
}

// FunnelHealth is backend health for one step's cohort.
type FunnelHealth struct {
	Step          uint8   `ch:"step"`
	Kind          string  `ch:"kind"`
	Users         uint64  `ch:"users"`
	Sessions      uint64  `ch:"sessions"`
	ErrorSessions uint64  `ch:"error_sessions"`
	P95Ns         float64 `ch:"p95_ns"`
}

// FunnelError is an issue seen in one step's cohort.
type FunnelError struct {
	Step          uint8  `ch:"step"`
	Kind          string `ch:"kind"`
	IssueID       uint64 `ch:"issue_id"`
	Title         string `ch:"title"`
	ExceptionType string `ch:"exception_type"`
	ServiceName   string `ch:"service_name"`
	Users         uint64 `ch:"users"`
	Count         uint64 `ch:"n"`
}

// FunnelData is the raw result of a funnel run.
type FunnelData struct {
	Levels []FunnelLevel
	Health []FunnelHealth
	Errors []FunnelError
}

func (c *Client) RunFunnel(ctx context.Context, f FunnelQuery) (FunnelData, error) {
	var d FunnelData
	if err := f.Validate(); err != nil {
		return d, err
	}
	q, args := FunnelLevelsSQL(f)
	if err := c.conn.Select(ctx, &d.Levels, q, args...); err != nil {
		return d, fmt.Errorf("funnel levels: %w", err)
	}
	q, args = FunnelHealthSQL(f)
	if err := c.conn.Select(ctx, &d.Health, q, args...); err != nil {
		return d, fmt.Errorf("funnel health: %w", err)
	}
	q, args = FunnelErrorsSQL(f, 5)
	if err := c.conn.Select(ctx, &d.Errors, q, args...); err != nil {
		return d, fmt.Errorf("funnel errors: %w", err)
	}
	return d, nil
}

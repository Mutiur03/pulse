package ch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Trend math: what each bucket of a series counts.
const (
	MathTotal          = "total"
	MathUniqueUsers    = "unique_users"
	MathUniqueSessions = "unique_sessions"
	MathSum            = "sum"
	MathAvg            = "avg"
	MathMin            = "min"
	MathMax            = "max"
	MathP95            = "p95"
)

// Property filter operators.
const (
	OpEq          = "eq"
	OpNeq         = "neq"
	OpContains    = "contains"
	OpNotContains = "not_contains"
	OpIsSet       = "is_set"
	OpIsNotSet    = "is_not_set"
	OpGt          = "gt"
	OpLt          = "lt"
)

// Limits on a trend query.
const (
	MaxTrendSeries    = 10
	MaxTrendFilters   = 20
	MaxTrendBuckets   = 1000
	MaxBreakdownTerms = 10
	// OtherBreakdown labels events outside the top breakdown values.
	OtherBreakdown = "$other"
)

// eventColumns are properties read from their own columns; every other
// name is a key of the properties JSON (the SDK sends $current_url,
// $pathname and so on there).
var eventColumns = map[string]string{
	"distinct_id": "distinct_id", "session_id": "session_id", "event": "event",
}

// PropertyFilter narrows a series by an event property.
type PropertyFilter struct {
	Property string `json:"property"`
	Op       string `json:"op"`
	Value    string `json:"value,omitempty"`
}

// TrendSeries is one line of a trend: an event (or every event when
// empty), what to count and filters.
type TrendSeries struct {
	Event    string           `json:"event"`
	Math     string           `json:"math"`
	Property string           `json:"property,omitempty"` // for sum, avg, min, max and p95
	Filters  []PropertyFilter `json:"filters,omitempty"`
}

// TrendQuery charts series over time, optionally broken down by a property.
type TrendQuery struct {
	Project   uint64
	Series    []TrendSeries
	Filters   []PropertyFilter // apply to every series
	Breakdown string
	Interval  string // hour, day, week or month
	From, To  time.Time
}

var intervalExpr = map[string]string{
	"hour":  "toStartOfHour(timestamp)",
	"day":   "toStartOfDay(timestamp)",
	"week":  "toDateTime(toMonday(timestamp), 'UTC')",
	"month": "toDateTime(toStartOfMonth(timestamp), 'UTC')",
}

// IntervalStep is the nominal length of a bucket.
func IntervalStep(interval string) time.Duration {
	switch interval {
	case "hour":
		return time.Hour
	case "week":
		return 7 * 24 * time.Hour
	case "month":
		return 30 * 24 * time.Hour
	}
	return 24 * time.Hour
}

func validFilter(f PropertyFilter) error {
	if strings.TrimSpace(f.Property) == "" || len(f.Property) > 200 {
		return errors.New("filter property is required (at most 200 characters)")
	}
	if len(f.Value) > 1000 {
		return errors.New("filter value is too long")
	}
	switch f.Op {
	case OpEq, OpNeq, OpContains, OpNotContains, OpIsSet, OpIsNotSet, OpGt, OpLt:
		return nil
	}
	return fmt.Errorf("unknown filter operator %q", f.Op)
}

// Validate checks the query and fills in defaults.
func (q *TrendQuery) Validate() error {
	if len(q.Series) == 0 || len(q.Series) > MaxTrendSeries {
		return fmt.Errorf("a trend needs 1-%d series", MaxTrendSeries)
	}
	if q.Interval == "" {
		q.Interval = "day"
	}
	if _, ok := intervalExpr[q.Interval]; !ok {
		return errors.New("interval must be hour, day, week or month")
	}
	if !q.To.After(q.From) {
		return errors.New("from must be before to")
	}
	if q.To.Sub(q.From)/IntervalStep(q.Interval) > MaxTrendBuckets {
		return fmt.Errorf("too many %s buckets; pick a larger interval", q.Interval)
	}
	if len(q.Breakdown) > 200 {
		return errors.New("breakdown property is too long")
	}
	n := len(q.Filters)
	for _, f := range q.Filters {
		if err := validFilter(f); err != nil {
			return err
		}
	}
	for i := range q.Series {
		s := &q.Series[i]
		if len(s.Event) > 200 {
			return errors.New("event name is too long")
		}
		if s.Math == "" {
			s.Math = MathTotal
		}
		switch s.Math {
		case MathTotal, MathUniqueUsers, MathUniqueSessions:
		case MathSum, MathAvg, MathMin, MathMax, MathP95:
			if strings.TrimSpace(s.Property) == "" || len(s.Property) > 200 {
				return fmt.Errorf("%s needs a numeric property", s.Math)
			}
		default:
			return fmt.Errorf("unknown math %q", s.Math)
		}
		n += len(s.Filters)
		for _, f := range s.Filters {
			if err := validFilter(f); err != nil {
				return err
			}
		}
	}
	if n > MaxTrendFilters {
		return fmt.Errorf("at most %d filters", MaxTrendFilters)
	}
	return nil
}

// propExpr returns a String expression for a property: its column, or the
// value from the properties JSON (numbers and booleans as written).
func propExpr(p string, args *[]interface{}) string {
	if c, ok := eventColumns[p]; ok {
		return c
	}
	*args = append(*args, p, p, p)
	return "if(JSONType(properties, ?) = 'String', JSONExtractString(properties, ?), trim(BOTH '\"' FROM JSONExtractRaw(properties, ?)))"
}

func numExpr(p string, args *[]interface{}) string {
	if c, ok := eventColumns[p]; ok {
		return "toFloat64OrNull(" + c + ")"
	}
	*args = append(*args, p)
	return "toFloat64OrNull(JSONExtractRaw(properties, ?))"
}

func filterSQL(f PropertyFilter, args *[]interface{}) string {
	switch f.Op {
	case OpIsSet, OpIsNotSet:
		var e string
		if c, ok := eventColumns[f.Property]; ok {
			e = c + " != ''"
		} else {
			*args = append(*args, f.Property)
			e = "JSONHas(properties, ?)"
		}
		if f.Op == OpIsNotSet {
			return "NOT " + e
		}
		return e
	case OpGt, OpLt:
		e := numExpr(f.Property, args)
		*args = append(*args, f.Value)
		if f.Op == OpGt {
			return e + " > toFloat64OrNull(?)"
		}
		return e + " < toFloat64OrNull(?)"
	}
	e := propExpr(f.Property, args)
	*args = append(*args, f.Value)
	switch f.Op {
	case OpNeq:
		return e + " != ?"
	case OpContains:
		return "positionCaseInsensitive(" + e + ", ?) > 0"
	case OpNotContains:
		return "positionCaseInsensitive(" + e + ", ?) = 0"
	}
	return e + " = ?"
}

// seriesWhere is the WHERE clause of one series.
func seriesWhere(q TrendQuery, s TrendSeries) (string, []interface{}) {
	args := []interface{}{q.Project, q.From, q.To}
	conds := []string{"project_id = ?", "timestamp >= ?", "timestamp < ?"}
	if s.Event != "" {
		conds = append(conds, "event = ?")
		args = append(args, s.Event)
	}
	for _, f := range append(append([]PropertyFilter{}, q.Filters...), s.Filters...) {
		conds = append(conds, filterSQL(f, &args))
	}
	return strings.Join(conds, " AND "), args
}

func mathSQL(s TrendSeries, args *[]interface{}) string {
	switch s.Math {
	case MathUniqueUsers:
		// Counted as persons: ids linked by identify or alias count once.
		return "toFloat64(uniqIf(person, person != ''))"
	case MathUniqueSessions:
		return "toFloat64(uniqIf(session_id, session_id != ''))"
	case MathSum, MathAvg, MathMin, MathMax, MathP95:
		e := numExpr(s.Property, args)
		fn := map[string]string{MathSum: "sum", MathAvg: "avg", MathMin: "min", MathMax: "max", MathP95: "quantile(0.95)"}[s.Math]
		return "toFloat64(ifNull(" + fn + "(" + e + "), 0))"
	}
	return "toFloat64(count())"
}

// TrendSQL builds the query for one series. With bucketed it groups by time
// bucket too; without it returns one value per breakdown term over the whole
// range. Breakdowns keep the top terms by event count and fold the rest into
// OtherBreakdown.
func TrendSQL(q TrendQuery, s TrendSeries, bucketed bool) (string, []interface{}) {
	var args []interface{}
	cols := []string{}
	group := []string{}
	if bucketed {
		cols = append(cols, intervalExpr[q.Interval]+" AS bucket")
		group = append(group, "bucket")
	} else {
		cols = append(cols, "toDateTime(0, 'UTC') AS bucket")
	}
	if q.Breakdown != "" {
		// Arguments are appended in the order their placeholders appear.
		term := "if(" + propExpr(q.Breakdown, &args) + " IN (SELECT "
		term += propExpr(q.Breakdown, &args) + " AS t FROM events WHERE "
		where, wargs := seriesWhere(q, s)
		args = append(args, wargs...)
		term += where + fmt.Sprintf(" GROUP BY t ORDER BY count() DESC LIMIT %d), ", MaxBreakdownTerms)
		term += propExpr(q.Breakdown, &args) + ", '" + OtherBreakdown + "') AS term"
		cols = append(cols, term)
		group = append(group, "term")
	} else {
		cols = append(cols, "'' AS term")
	}
	cols = append(cols, mathSQL(s, &args)+" AS value")
	from := "events"
	if s.Math == MathUniqueUsers {
		from = PersonsFrom
		args = append(args, q.Project)
	}
	where, wargs := seriesWhere(q, s)
	args = append(args, wargs...)
	sql := "SELECT " + strings.Join(cols, ", ") + " FROM " + from + " WHERE " + where
	if len(group) > 0 {
		sql += " GROUP BY " + strings.Join(group, ", ")
	}
	sql += " ORDER BY bucket"
	return sql, args
}

// TrendRow is one bucket (or total) of one breakdown term.
type TrendRow struct {
	Bucket time.Time `ch:"bucket"`
	Term   string    `ch:"term"`
	Value  float64   `ch:"value"`
}

// TrendSeriesData holds a series' rows over time and its totals.
type TrendSeriesData struct {
	Buckets []TrendRow
	Totals  []TrendRow
}

// RunTrend queries every series.
func (c *Client) RunTrend(ctx context.Context, q TrendQuery) ([]TrendSeriesData, error) {
	out := make([]TrendSeriesData, len(q.Series))
	for i, s := range q.Series {
		for _, bucketed := range []bool{true, false} {
			sql, args := TrendSQL(q, s, bucketed)
			var rows []TrendRow
			if err := c.conn.Select(ctx, &rows, sql, args...); err != nil {
				return nil, err
			}
			if bucketed {
				out[i].Buckets = rows
			} else {
				out[i].Totals = rows
			}
		}
	}
	return out, nil
}

// TraceStatsQuery charts request volume, errors and latency of root spans.
type TraceStatsQuery struct {
	Project  uint64
	Service  string
	Interval string
	From, To time.Time
}

// TraceStatsRow is one bucket of trace stats.
type TraceStatsRow struct {
	Bucket   time.Time `ch:"bucket"`
	Requests uint64    `ch:"requests"`
	Errors   uint64    `ch:"errors"`
	P50Ns    float64   `ch:"p50_ns"`
	P95Ns    float64   `ch:"p95_ns"`
}

// TraceStatsSQL builds the trace stats query over root spans (no parent).
func TraceStatsSQL(q TraceStatsQuery) (string, []interface{}) {
	args := []interface{}{q.Project, q.From, q.To}
	where := "project_id = ? AND timestamp >= ? AND timestamp < ? AND parent_span_id = ''"
	if q.Service != "" {
		where += " AND service_name = ?"
		args = append(args, q.Service)
	}
	return "SELECT " + intervalExpr[q.Interval] + ` AS bucket, count() AS requests,
		countIf(status_code = 'error' OR http_status >= 500) AS errors,
		quantile(0.5)(duration_ns) AS p50_ns, quantile(0.95)(duration_ns) AS p95_ns
	FROM spans WHERE ` + where + " GROUP BY bucket ORDER BY bucket", args
}

// TraceStats runs TraceStatsSQL.
func (c *Client) TraceStats(ctx context.Context, q TraceStatsQuery) ([]TraceStatsRow, error) {
	sql, args := TraceStatsSQL(q)
	var out []TraceStatsRow
	err := c.conn.Select(ctx, &out, sql, args...)
	return out, err
}

package alerts

import (
	"fmt"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/logquery"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Query is a ClickHouse query for a threshold metric.
type Query = ch.AlertQuery

// MetricQuery builds the query for one evaluation over [to-window, to).
func MetricQuery(r *Rule, to time.Time) (Query, error) {
	return build(r, to.Add(-r.Window), to, 0)
}

// SeriesQuery builds a chart of the metric over [from, to) in buckets of the
// rule's window, with the bucket start selected as b.
func SeriesQuery(r *Rule, from, to time.Time, bucket time.Duration) (Query, error) {
	if bucket < time.Minute {
		bucket = time.Minute
	}
	return build(r, from, to, bucket)
}

// metricSQL is the per-metric part of a query: the table and selected
// expressions, plus filters appended to the shared project and time range.
type metricSQL struct {
	table, value, trace, issue string
	where                      []string
	args                       []interface{}
	empty                      bool
	// logCond is a log search condition that may contain %, so it is
	// substituted as %[2]s rather than written into the format.
	logCond string
}

func (m *metricSQL) filter(cond string, args ...interface{}) {
	m.where = append(m.where, cond)
	m.args = append(m.args, args...)
}

// filterIf adds "column = ?" when v is set.
func (m *metricSQL) filterIf(column, v string) {
	if v != "" {
		m.filter(column+" = ?", v)
	}
}

func build(r *Rule, from, to time.Time, bucket time.Duration) (Query, error) {
	minutes := r.Window.Minutes()
	if bucket > 0 {
		minutes = bucket.Minutes()
	}
	m := &metricSQL{
		where: []string{"project_id = ?", "%[1]s >= ?", "%[1]s < ?"},
		args:  []interface{}{uint64(r.ProjectID), from, to},
	}
	switch r.Metric {
	case ErrorCount, ErrorRate:
		errorSQL(m, r, from, to, minutes)
	case FailureRate, LatencyP95, LatencyP99, Throughput:
		spanSQL(m, r, minutes)
	case EventCount:
		m.table, m.value = "events", "toFloat64(count())"
		m.trace, m.issue = "argMax(trace_id, timestamp)", "toUInt64(0)"
		m.filter("event = ?", r.EventName)
		if r.Route != "" {
			m.filter("positionCaseInsensitive(current_url, ?) > 0", r.Route)
		}
	case LogCount:
		if err := logSQL(m, r); err != nil {
			return Query{}, err
		}
	default:
		return Query{}, fmt.Errorf("metric %q has no query", r.Metric)
	}
	return m.query(bucket), nil
}

func errorSQL(m *metricSQL, r *Rule, from, to time.Time, minutes float64) {
	m.table, m.value = "errors", "toFloat64(count())"
	if r.Metric == ErrorRate {
		m.value = fmt.Sprintf("count() / %g", minutes)
	}
	m.trace, m.issue = "argMax(trace_id, timestamp)", "argMax(issue_id, timestamp)"
	m.filterIf("service_name", r.Service)
	m.filterIf("environment", r.Environment)
	if r.Route != "" {
		// Browser errors carry a URL; backend errors join through the
		// trace to the server span's route.
		m.filter("(positionCaseInsensitive(url, ?) > 0 OR trace_id IN (SELECT trace_id FROM spans WHERE project_id = ? AND http_route = ? AND timestamp >= ? AND timestamp < ?))",
			r.Route, uint64(r.ProjectID), r.Route, from, to)
	}
}

func spanSQL(m *metricSQL, r *Rule, minutes float64) {
	m.table, m.issue = "spans", "toUInt64(0)"
	m.where = append(m.where, "span_kind = 'server'")
	switch r.Metric {
	case FailureRate:
		m.value = "if(count() = 0, 0, countIf(status_code = 'ERROR' OR http_status >= 500) * 100 / count())"
		m.trace = "argMaxIf(trace_id, timestamp, status_code = 'ERROR' OR http_status >= 500)"
		m.empty = true
	case LatencyP95, LatencyP99:
		q := "0.95"
		if r.Metric == LatencyP99 {
			q = "0.99"
		}
		m.value = "quantile(" + q + ")(duration_ns) / 1e6"
		m.trace = "argMax(trace_id, duration_ns)"
		m.empty = true
	default: // Throughput
		m.value = fmt.Sprintf("count() / %g", minutes)
		m.trace = "argMax(trace_id, timestamp)"
	}
	m.filterIf("service_name", r.Service)
	m.filterIf("http_route", r.Route)
	if r.Environment != "" {
		m.filter("(attributes['resource.deployment.environment.name'] = ? OR attributes['resource.deployment.environment'] = ?)",
			r.Environment, r.Environment)
	}
}

func logSQL(m *metricSQL, r *Rule) error {
	m.table, m.value = "logs", "toFloat64(count())"
	m.trace, m.issue = "argMax(trace_id, timestamp)", "toUInt64(0)"
	q, err := logquery.Parse(r.LogQuery)
	if err != nil {
		return err
	}
	cond, qargs := q.Where()
	m.filterIf("service_name", r.Service)
	m.filter("%[2]s", qargs...)
	m.logCond = cond
	return nil
}

// query assembles the statement, bucketed by time when bucket > 0.
func (m *metricSQL) query(bucket time.Duration) Query {
	const ts = "timestamp"
	cond := fmt.Sprintf(strings.Join(m.where, " AND "), ts, m.logCond)
	if m.logCond == "" {
		cond = fmt.Sprintf(strings.Join(m.where, " AND "), ts)
	}
	sel := fmt.Sprintf("toFloat64(%s) AS v, count() AS n, %s AS t, toUInt64(%s) AS i", m.value, m.trace, m.issue)
	if bucket == 0 {
		return Query{SQL: "SELECT " + sel + " FROM " + m.table + " WHERE " + cond, Args: m.args, Empty: m.empty}
	}
	b := fmt.Sprintf("toStartOfInterval(%s, INTERVAL %d SECOND)", ts, int64(bucket/time.Second))
	return Query{
		SQL:   "SELECT toDateTime(" + b + ") AS b, " + sel + " FROM " + m.table + " WHERE " + cond + " GROUP BY b ORDER BY b",
		Args:  m.args,
		Empty: m.empty,
	}
}

// SampleFrom turns a query row into a sample.
func SampleFrom(q Query, v float64, n uint64, trace string, issue uint64) Sample {
	return Sample{Value: v, HasData: !(q.Empty && n == 0), TraceID: trace, IssueID: int64(issue)}
}

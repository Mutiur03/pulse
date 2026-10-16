package alerts

import (
	"fmt"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/logquery"
)

// Query is a ClickHouse query for a threshold metric. It selects the value v,
// the number of rows behind it n, and an example trace t and issue i.
type Query struct {
	SQL  string
	Args []interface{}
	// Empty is true when zero rows means "no data" rather than a zero value.
	Empty bool
}

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

func build(r *Rule, from, to time.Time, bucket time.Duration) (Query, error) {
	minutes := r.Window.Minutes()
	if bucket > 0 {
		minutes = bucket.Minutes()
	}
	var (
		table, ts, value, trace, issue string
		where                          = []string{"project_id = ?", "%[1]s >= ?", "%[1]s < ?"}
		args                           = []interface{}{uint64(r.ProjectID), from, to}
		empty                          bool
		logCond                        string
	)
	switch r.Metric {
	case ErrorCount, ErrorRate:
		table, ts = "errors", "timestamp"
		value = "toFloat64(count())"
		if r.Metric == ErrorRate {
			value = fmt.Sprintf("count() / %g", minutes)
		}
		trace, issue = "argMax(trace_id, timestamp)", "argMax(issue_id, timestamp)"
		if r.Service != "" {
			where, args = append(where, "service_name = ?"), append(args, r.Service)
		}
		if r.Environment != "" {
			where, args = append(where, "environment = ?"), append(args, r.Environment)
		}
		if r.Route != "" {
			// Browser errors carry a URL; backend errors join through the
			// trace to the server span's route.
			where = append(where, "(positionCaseInsensitive(url, ?) > 0 OR trace_id IN (SELECT trace_id FROM spans WHERE project_id = ? AND http_route = ? AND timestamp >= ? AND timestamp < ?))")
			args = append(args, r.Route, uint64(r.ProjectID), r.Route, from, to)
		}
	case FailureRate, LatencyP95, LatencyP99, Throughput:
		table, ts = "spans", "timestamp"
		where = append(where, "span_kind = 'server'")
		issue = "toUInt64(0)"
		switch r.Metric {
		case FailureRate:
			value = "if(count() = 0, 0, countIf(status_code = 'ERROR' OR http_status >= 500) * 100 / count())"
			trace = "argMaxIf(trace_id, timestamp, status_code = 'ERROR' OR http_status >= 500)"
			empty = true
		case LatencyP95, LatencyP99:
			q := "0.95"
			if r.Metric == LatencyP99 {
				q = "0.99"
			}
			value = "quantile(" + q + ")(duration_ns) / 1e6"
			trace = "argMax(trace_id, duration_ns)"
			empty = true
		case Throughput:
			value = fmt.Sprintf("count() / %g", minutes)
			trace = "argMax(trace_id, timestamp)"
		}
		if r.Service != "" {
			where, args = append(where, "service_name = ?"), append(args, r.Service)
		}
		if r.Route != "" {
			where, args = append(where, "http_route = ?"), append(args, r.Route)
		}
		if r.Environment != "" {
			where = append(where, "(attributes['resource.deployment.environment.name'] = ? OR attributes['resource.deployment.environment'] = ?)")
			args = append(args, r.Environment, r.Environment)
		}
	case EventCount:
		table, ts = "events", "timestamp"
		value = "toFloat64(count())"
		trace, issue = "argMax(trace_id, timestamp)", "toUInt64(0)"
		where, args = append(where, "event = ?"), append(args, r.EventName)
		if r.Route != "" {
			where, args = append(where, "positionCaseInsensitive(current_url, ?) > 0"), append(args, r.Route)
		}
	case LogCount:
		table, ts = "logs", "timestamp"
		value = "toFloat64(count())"
		trace, issue = "argMax(trace_id, timestamp)", "toUInt64(0)"
		q, err := logquery.Parse(r.LogQuery)
		if err != nil {
			return Query{}, err
		}
		cond, qargs := q.Where()
		if r.Service != "" {
			where, args = append(where, "service_name = ?"), append(args, r.Service)
		}
		// The condition may contain %, so it is an argument of the format.
		where, args = append(where, "%[2]s"), append(args, qargs...)
		logCond = cond
	default:
		return Query{}, fmt.Errorf("metric %q has no query", r.Metric)
	}
	cond := fmt.Sprintf(strings.Join(where, " AND "), ts, logCond)
	if logCond == "" {
		cond = fmt.Sprintf(strings.Join(where, " AND "), ts)
	}
	sel := fmt.Sprintf("toFloat64(%s) AS v, count() AS n, %s AS t, toUInt64(%s) AS i", value, trace, issue)
	if bucket == 0 {
		return Query{SQL: "SELECT " + sel + " FROM " + table + " WHERE " + cond, Args: args, Empty: empty}, nil
	}
	b := fmt.Sprintf("toStartOfInterval(%s, INTERVAL %d SECOND)", ts, int64(bucket/time.Second))
	return Query{
		SQL:   "SELECT toDateTime(" + b + ") AS b, " + sel + " FROM " + table + " WHERE " + cond + " GROUP BY b ORDER BY b",
		Args:  args,
		Empty: empty,
	}, nil
}

// SampleFrom turns a query row into a sample.
func SampleFrom(q Query, v float64, n uint64, trace string, issue uint64) Sample {
	return Sample{Value: v, HasData: !(q.Empty && n == 0), TraceID: trace, IssueID: int64(issue)}
}

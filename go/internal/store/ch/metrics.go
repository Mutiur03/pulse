package ch

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// InsertMetrics writes decoded MetricPoint records (see ingest.MetricPoint).
func (c *Client) InsertMetrics(ctx context.Context, points []map[string]interface{}) error {
	if len(points) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, `INSERT INTO metrics (project_id, timestamp, service_name, name, description, unit,
		type, temporality, monotonic, value, count, sum, min, max, bounds, buckets, attributes)`)
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}
	for _, p := range points {
		mono, _ := p["monotonic"].(bool)
		if err := batch.Append(projectOf(p), timeOf(p), str(p, "service_name"), str(p, "name"), str(p, "description"), str(p, "unit"),
			str(p, "type"), str(p, "temporality"), mono, fnum(p, "value"), uint64(fnum(p, "count")), fnum(p, "sum"), fnum(p, "min"), fnum(p, "max"),
			floats(p["bounds"]), uints(p["buckets"]), strMap(p["attributes"])); err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}
	return batch.Send()
}

// fnum reads a number, negatives included.
func fnum(m map[string]interface{}, k string) float64 {
	v, _ := m[k].(float64)
	return v
}

func floats(v interface{}) []float64 {
	a, _ := v.([]interface{})
	out := make([]float64, 0, len(a))
	for _, x := range a {
		f, _ := x.(float64)
		out = append(out, f)
	}
	return out
}

func uints(v interface{}) []uint64 {
	a, _ := v.([]interface{})
	out := make([]uint64, 0, len(a))
	for _, x := range a {
		f, _ := x.(float64)
		out = append(out, uint64(f))
	}
	return out
}

// MetricName describes one metric seen in a time range.
type MetricName struct {
	Name     string   `ch:"name"`
	Type     string   `ch:"mtype"`
	Unit     string   `ch:"munit"`
	Services []string `ch:"services"`
	Points   uint64   `ch:"points"`
}

func (c *Client) MetricNames(ctx context.Context, project uint64, from, to time.Time) ([]MetricName, error) {
	var out []MetricName
	err := c.conn.Select(ctx, &out, `SELECT name, any(type) AS mtype, any(unit) AS munit,
		arraySort(groupUniqArray(20)(service_name)) AS services, count() AS points
		FROM metrics WHERE project_id = ? AND timestamp >= ? AND timestamp < ?
		GROUP BY name ORDER BY name LIMIT 1000`, project, from, to)
	return out, err
}

// MetricQuery selects one metric, bucketed by Step and split by GroupBy:
// "" for one series, "service.name", or an attribute key.
type MetricQuery struct {
	Project  uint64
	Name     string
	Service  string
	From, To time.Time
	Step     time.Duration
	GroupBy  string
	// Agg is avg, sum, min, max, count, rate, p50, p90, p95 or p99; empty
	// picks one that suits the metric type.
	Agg string
}

type MetricPoint struct {
	Time  time.Time `json:"t"`
	Value float64   `json:"v"`
}

type MetricSeries struct {
	Label  string        `json:"label"`
	Points []MetricPoint `json:"points"`
}

type MetricResult struct {
	Type, Unit, Agg string
	Series          []MetricSeries
}

// MaxMetricSeries bounds how many series a grouped query returns.
const MaxMetricSeries = 20

var metricAggs = map[string]bool{"avg": true, "sum": true, "min": true, "max": true, "count": true, "rate": true,
	"p50": true, "p90": true, "p95": true, "p99": true}

// ValidMetricAgg reports whether a is a known aggregation (or empty).
func ValidMetricAgg(a string) bool { return a == "" || metricAggs[a] }

type metricRow struct {
	T       time.Time `ch:"t"`
	Label   string    `ch:"lbl"`
	Avg     float64   `ch:"a_avg"`
	Sum     float64   `ch:"a_sum"`
	Min     float64   `ch:"a_min"`
	Max     float64   `ch:"a_max"`
	N       uint64    `ch:"a_n"`
	Count   uint64    `ch:"h_count"`
	HSum    float64   `ch:"h_sum"`
	Buckets []uint64  `ch:"h_buckets"`
	Bounds  []float64 `ch:"h_bounds"`
}

func defaultAgg(typ, temporality string) string {
	switch {
	case typ == "sum" && temporality == "cumulative":
		return "max"
	case typ == "sum":
		return "sum"
	case typ == "histogram":
		return "p95"
	}
	return "avg"
}

func (c *Client) QueryMetric(ctx context.Context, q MetricQuery) (MetricResult, error) {
	var meta []struct {
		Type        string `ch:"mtype"`
		Unit        string `ch:"munit"`
		Temporality string `ch:"mtemp"`
	}
	if err := c.conn.Select(ctx, &meta, `SELECT any(type) AS mtype, any(unit) AS munit, any(temporality) AS mtemp
		FROM metrics WHERE project_id = ? AND name = ? AND timestamp >= ? AND timestamp < ?`, q.Project, q.Name, q.From, q.To); err != nil {
		return MetricResult{}, err
	}
	res := MetricResult{Series: []MetricSeries{}}
	if len(meta) > 0 {
		res.Type, res.Unit = meta[0].Type, meta[0].Unit
	}
	res.Agg = q.Agg
	if res.Agg == "" {
		res.Agg = defaultAgg(res.Type, meta0(meta))
	}
	label := "''"
	args := []interface{}{int64(q.Step / time.Second)}
	switch q.GroupBy {
	case "":
	case "service.name", "service":
		label = "service_name"
	default:
		label = "attributes[?]"
		args = append(args, q.GroupBy)
	}
	args = append(args, q.Project, q.Name, q.From, q.To)
	where := ""
	if q.Service != "" {
		where = " AND service_name = ?"
		args = append(args, q.Service)
	}
	var rows []metricRow
	err := c.conn.Select(ctx, &rows, `SELECT toStartOfInterval(timestamp, toIntervalSecond(?)) AS t, `+label+` AS lbl,
		avg(value) AS a_avg, sum(value) AS a_sum, min(value) AS a_min, max(value) AS a_max, count() AS a_n,
		sum(count) AS h_count, sum(sum) AS h_sum, sumForEach(buckets) AS h_buckets, any(bounds) AS h_bounds
		FROM metrics WHERE project_id = ? AND name = ? AND timestamp >= ? AND timestamp < ?`+where+`
		GROUP BY t, lbl ORDER BY t LIMIT 20000`, args...)
	if err != nil {
		return res, err
	}
	// Keep the largest series when grouping yields too many.
	weight := map[string]float64{}
	for _, r := range rows {
		weight[r.Label] += float64(r.N)
	}
	labels := make([]string, 0, len(weight))
	for l := range weight {
		labels = append(labels, l)
	}
	sort.Slice(labels, func(i, j int) bool {
		if weight[labels[i]] != weight[labels[j]] {
			return weight[labels[i]] > weight[labels[j]]
		}
		return labels[i] < labels[j]
	})
	if len(labels) > MaxMetricSeries {
		labels = labels[:MaxMetricSeries]
	}
	idx := map[string]int{}
	for i, l := range labels {
		idx[l] = i
		res.Series = append(res.Series, MetricSeries{Label: l, Points: []MetricPoint{}})
	}
	step := q.Step.Seconds()
	for _, r := range rows {
		i, ok := idx[r.Label]
		if !ok {
			continue
		}
		v, ok := aggValue(res.Agg, res.Type, r, step)
		if !ok {
			continue
		}
		res.Series[i].Points = append(res.Series[i].Points, MetricPoint{Time: r.T, Value: v})
	}
	return res, nil
}

func meta0(meta []struct {
	Type        string `ch:"mtype"`
	Unit        string `ch:"munit"`
	Temporality string `ch:"mtemp"`
}) string {
	if len(meta) == 0 {
		return ""
	}
	return meta[0].Temporality
}

func aggValue(agg, typ string, r metricRow, step float64) (float64, bool) {
	dist := typ == "histogram" || typ == "exponential_histogram" || typ == "summary"
	switch agg {
	case "sum":
		if dist {
			return r.HSum, true
		}
		return r.Sum, true
	case "min":
		return r.Min, true
	case "max":
		return r.Max, true
	case "count":
		if dist {
			return float64(r.Count), true
		}
		return float64(r.N), true
	case "rate":
		if dist {
			return float64(r.Count) / step, true
		}
		return r.Sum / step, true
	case "p50", "p90", "p95", "p99":
		q, _ := strconv.ParseFloat(agg[1:], 64)
		return Quantile(r.Bounds, r.Buckets, q/100)
	}
	if dist && r.Count > 0 {
		return r.HSum / float64(r.Count), true
	}
	return r.Avg, true
}

// Quantile estimates a quantile from explicit-bucket histogram counts,
// interpolating linearly inside the bucket (as Prometheus does). The open
// first and last buckets report their finite bound.
func Quantile(bounds []float64, counts []uint64, q float64) (float64, bool) {
	if len(counts) == 0 || len(counts) != len(bounds)+1 {
		return 0, false
	}
	var total uint64
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return 0, false
	}
	rank := q * float64(total)
	var seen float64
	for i, c := range counts {
		if c == 0 {
			continue
		}
		if seen+float64(c) >= rank {
			switch {
			case i == 0:
				return bounds[0], true
			case i >= len(bounds):
				return bounds[len(bounds)-1], true
			}
			lo, hi := bounds[i-1], bounds[i] //nolint:gosec // 0 < i < len(bounds) after the switch
			frac := (rank - seen) / float64(c)
			return lo + (hi-lo)*math.Max(0, math.Min(1, frac)), true
		}
		seen += float64(c)
	}
	return bounds[len(bounds)-1], true
}

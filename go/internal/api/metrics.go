package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// MetricReader reads OTLP metrics from ClickHouse.
type MetricReader interface {
	MetricNames(ctx context.Context, project uint64, from, to time.Time) ([]ch.MetricName, error)
	QueryMetric(ctx context.Context, q ch.MetricQuery) (ch.MetricResult, error)
}

// MaxMetricPoints bounds the buckets per series.
const MaxMetricPoints = 1500

func (s *Server) ListMetricNames(w http.ResponseWriter, r *http.Request, params gen.ListMetricNamesParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	if s.Metrics == nil {
		writeJSON(w, http.StatusOK, []gen.MetricName{})
		return
	}
	from, to := s.timeRange(params.From, params.To)
	list, err := s.Metrics.MetricNames(r.Context(), p, from, to)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.MetricName, 0, len(list))
	for _, m := range list {
		out = append(out, gen.MetricName{Name: m.Name, Type: m.Type, Unit: m.Unit, Services: m.Services, Points: int64(m.Points)})
	}
	writeJSON(w, http.StatusOK, out)
}

// metricStep picks the bucket size: the requested one if it keeps the
// series under MaxMetricPoints, else about 60 buckets over the range.
func metricStep(from, to time.Time, want *int) time.Duration {
	span := to.Sub(from)
	if want != nil && *want > 0 {
		step := time.Duration(*want) * time.Second
		if span/step <= MaxMetricPoints {
			return step
		}
	}
	step := (span / 60).Truncate(time.Second)
	if step < 10*time.Second {
		step = 10 * time.Second
	}
	return step
}

func (s *Server) QueryMetric(w http.ResponseWriter, r *http.Request, params gen.QueryMetricParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	name := strings.TrimSpace(params.Name)
	if name == "" || len(name) > 255 {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	q := ch.MetricQuery{Project: p, Name: name}
	q.From, q.To = s.timeRange(params.From, params.To)
	if !q.To.After(q.From) {
		writeErr(w, http.StatusBadRequest, "from must be before to")
		return
	}
	q.Step = metricStep(q.From, q.To, params.Step)
	if params.Agg != nil {
		q.Agg = string(*params.Agg)
	}
	if !ch.ValidMetricAgg(q.Agg) {
		writeErr(w, http.StatusBadRequest, "unknown agg")
		return
	}
	if params.Service != nil {
		q.Service = strings.TrimSpace(*params.Service)
	}
	if params.GroupBy != nil {
		q.GroupBy = strings.TrimSpace(*params.GroupBy)
	}
	out := gen.MetricResult{Name: name, Agg: q.Agg, Step: int(q.Step / time.Second), Series: []gen.MetricSeries{}}
	if s.Metrics == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	res, err := s.Metrics.QueryMetric(r.Context(), q)
	if err != nil {
		internal(w, err)
		return
	}
	out.Type, out.Unit, out.Agg = res.Type, res.Unit, res.Agg
	for _, ser := range res.Series {
		pts := make([]gen.MetricPoint, len(ser.Points))
		for i, pt := range ser.Points {
			pts[i] = gen.MetricPoint{T: pt.Time, V: pt.Value}
		}
		out.Series = append(out.Series, gen.MetricSeries{Label: ser.Label, Points: pts})
	}
	writeJSON(w, http.StatusOK, out)
}

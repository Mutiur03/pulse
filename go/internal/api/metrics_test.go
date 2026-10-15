package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

type fakeMetrics struct{ last ch.MetricQuery }

func (f *fakeMetrics) MetricNames(context.Context, uint64, time.Time, time.Time) ([]ch.MetricName, error) {
	return []ch.MetricName{{Name: "cpu", Type: "gauge", Services: []string{"api"}, Points: 3}}, nil
}

func (f *fakeMetrics) QueryMetric(_ context.Context, q ch.MetricQuery) (ch.MetricResult, error) {
	f.last = q
	return ch.MetricResult{Type: "gauge", Agg: "avg", Series: []ch.MetricSeries{{Label: "api", Points: []ch.MetricPoint{{Time: q.From, Value: 0.5}}}}}, nil
}

func TestMetricEndpoints(t *testing.T) {
	m := &fakeMetrics{}
	s := &Server{Traces: &fakeTraces{}, Metrics: m}
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}

	rec := call(t, s, member, "GET", "/v1/metrics/query?project_id=1&name=cpu&group_by=service.name&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out gen.MetricResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Step != 60 || len(out.Series) != 1 || out.Series[0].Points[0].V != 0.5 || m.last.GroupBy != "service.name" || m.last.Project != 1 {
		t.Errorf("%+v %+v", out, m.last)
	}
	for path, want := range map[string]int{
		"/v1/metrics/query?project_id=1":                 http.StatusBadRequest,
		"/v1/metrics/query?project_id=1&name=cpu&agg=p7": http.StatusBadRequest,
		"/v1/metrics/query?project_id=2&name=cpu":        http.StatusNotFound,
		"/v1/metrics/names?project_id=1":                 http.StatusOK,
	} {
		if rec := call(t, s, member, "GET", path, ""); rec.Code != want {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestMetricStep(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ten := 10
	one := 1
	if got := metricStep(from, from.Add(time.Hour), &ten); got != 10*time.Second {
		t.Errorf("requested step: %v", got)
	}
	if got := metricStep(from, from.Add(24*time.Hour), &one); got != 24*time.Minute {
		t.Errorf("too fine a step falls back: %v", got)
	}
	if got := metricStep(from, from.Add(time.Minute), nil); got != 10*time.Second {
		t.Errorf("minimum step: %v", got)
	}
}

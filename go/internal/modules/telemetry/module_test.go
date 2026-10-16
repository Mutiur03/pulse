package telemetry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/modules/telemetry"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

type fakeTraces struct {
	telemetry.Traces
	lastFilter ch.TraceFilter
}

func (f *fakeTraces) ListTraces(_ context.Context, tf ch.TraceFilter) ([]ch.TraceSummaryRow, error) {
	f.lastFilter = tf
	return []ch.TraceSummaryRow{{TraceID: "abc", RootName: "click", Browser: 2, HTTPStatus: 402}}, nil
}

type fakeStats struct{}

func (fakeStats) TraceStats(_ context.Context, q ch.TraceStatsQuery) ([]ch.TraceStatsRow, error) {
	return []ch.TraceStatsRow{{Bucket: q.From.Truncate(time.Hour), Requests: 10, Errors: 2, P50Ns: 5e6, P95Ns: 2e7}}, nil
}

func call(t *testing.T, m *telemetry.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	m.Register(r)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if p != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTraceAccess(t *testing.T) {
	s := telemetry.New(telemetry.Deps{Traces: &fakeTraces{}})
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	for path, want := range map[string]int{
		"/v1/traces": 200, "/v1/traces?project_id=1": 200, "/v1/traces?project_id=2": 404,
		"/v1/traces/stats?project_id=1": 503,
	} {
		if rec := call(t, s, member, "GET", path, ""); rec.Code != want {
			t.Errorf("%s: got %d want %d (%s)", path, rec.Code, want, rec.Body)
		}
	}
	if rec := call(t, s, nil, "GET", "/v1/traces", ""); rec.Code != 401 {
		t.Errorf("no principal: %d", rec.Code)
	}
}

func TestTraceSearchParams(t *testing.T) {
	tr := &fakeTraces{}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s := telemetry.New(telemetry.Deps{Traces: tr, Now: func() time.Time { return now }})
	me := &auth.Principal{Projects: []auth.Project{{ID: 1}}}
	rec := call(t, s, me, "GET", "/v1/traces?service=api&status=error&min_duration_ms=250&query=pay", "")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	f := tr.lastFilter
	if f.Service != "api" || f.Status != "error" || f.MinDurationNs != 250e6 || f.NameQuery != "pay" ||
		!f.To.Equal(now) || !f.From.Equal(now.Add(-24*time.Hour)) || f.Limit != 50 {
		t.Errorf("filter %+v", f)
	}
	var out gen.TraceList
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Traces) != 1 || !out.Traces[0].HasBrowser || *out.Traces[0].HttpStatus != 402 {
		t.Errorf("summary %+v", out)
	}
	if rec := call(t, s, me, "GET", "/v1/traces?status=bogus", ""); rec.Code != 400 {
		t.Errorf("bad status: %d", rec.Code)
	}
}

func TestTraceStats(t *testing.T) {
	s := telemetry.New(telemetry.Deps{Traces: &fakeTraces{}, Stats: fakeStats{}})
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	rec := call(t, s, member, "GET", "/v1/traces/stats?project_id=1&interval=hour&from=2026-01-01T00:00:00Z&to=2026-01-01T03:00:00Z", "")
	var st gen.TraceStats
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || len(st.Points) != 3 || st.Requests != 10 || st.Points[0].ErrorRate != 0.2 || st.Points[0].P95Ms != 20 || st.Points[1].Requests != 0 {
		t.Errorf("trace stats %d %+v", rec.Code, st)
	}
}

type fakeLogs struct {
	filter ch.LogFilter
	step   time.Duration
	rows   []ch.LogRow
}

func (f *fakeLogs) SearchLogs(_ context.Context, q ch.LogFilter) ([]ch.LogRow, error) {
	f.filter = q
	if len(f.rows) > q.Limit {
		return f.rows[:q.Limit], nil
	}
	return f.rows, nil
}

func (f *fakeLogs) LogHistogram(_ context.Context, q ch.LogFilter, step time.Duration) ([]ch.LogBucket, error) {
	f.filter, f.step = q, step
	return []ch.LogBucket{{Start: q.From.Truncate(step), Severity: "ERROR", Count: 2}, {Start: q.From.Truncate(step), Severity: "", Count: 1}}, nil
}

func (f *fakeLogs) LogFacets(context.Context, ch.LogFilter) ([]ch.LogFacet, error) {
	return []ch.LogFacet{{Field: "service", Value: "api", Count: 3}, {Field: "level", Value: "ERROR", Count: 2}, {Field: "level", Value: "INFO", Count: 1}}, nil
}

func TestLogsEndpoints(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	l := &fakeLogs{rows: []ch.LogRow{
		{Timestamp: now.Add(-time.Minute), Severity: "ERROR", Service: "api", Body: "payment failed", TraceID: "t1", Attributes: `{"order_id":7}`},
		{Timestamp: now.Add(-2 * time.Minute), Severity: "INFO", Service: "api", Body: "ok", Attributes: `{}`},
	}}
	s := telemetry.New(telemetry.Deps{Traces: &fakeTraces{}, Logs: l, Now: func() time.Time { return now }})
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}

	rec := call(t, s, member, "GET", "/v1/logs?project_id=1&q=level:ERROR+payment&limit=1", "")
	var list gen.LogList
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list.Logs) != 1 || list.Logs[0].Attributes["order_id"] != float64(7) || *list.Logs[0].TraceId != "t1" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if list.Query != "level:error payment" || list.NextBefore == nil || !list.NextBefore.Equal(now.Add(-time.Minute)) {
		t.Errorf("query %q next %v", list.Query, list.NextBefore)
	}
	if l.filter.Limit != 1 || l.filter.To != now || l.filter.To.Sub(l.filter.From) != 24*time.Hour {
		t.Errorf("filter %+v", l.filter)
	}

	// Live tail asks for newer logs and never pages.
	rec = call(t, s, member, "GET", "/v1/logs?project_id=1&after=2026-05-10T11:58:00Z&limit=1", "")
	list = gen.LogList{}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || list.NextBefore != nil || !l.filter.After.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("tail %d %s %+v", rec.Code, rec.Body, l.filter)
	}

	if rec = call(t, s, member, "GET", "/v1/logs?project_id=1&q=level:loud", ""); rec.Code != 400 {
		t.Fatalf("bad query: %d", rec.Code)
	}

	rec = call(t, s, member, "GET", "/v1/logs/stats?project_id=1", "")
	var st gen.LogStats
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || st.IntervalSeconds != 900 || len(st.Buckets) != 96 || st.Total != 3 || len(st.Services) != 1 || len(st.Levels) != 2 {
		t.Fatalf("%d interval %d buckets %d total %d", rec.Code, st.IntervalSeconds, len(st.Buckets), st.Total)
	}
	if st.Buckets[0].Counts["ERROR"] != 2 || st.Buckets[0].Counts["NONE"] != 1 {
		t.Errorf("first bucket %+v", st.Buckets[0])
	}
}

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
	s := telemetry.New(telemetry.Deps{Traces: &fakeTraces{}, Metrics: m})
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

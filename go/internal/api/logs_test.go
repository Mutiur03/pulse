package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

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
	s := &Server{Traces: &fakeTraces{}, Logs: l, Now: func() time.Time { return now }}
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

func TestLogInterval(t *testing.T) {
	for span, want := range map[time.Duration]time.Duration{
		15 * time.Minute: 10 * time.Second, time.Hour: 30 * time.Second, 24 * time.Hour: 15 * time.Minute,
		3 * 24 * time.Hour: time.Hour, 60 * 24 * time.Hour: 12 * time.Hour,
	} {
		if got := logInterval(span); got != want {
			t.Errorf("%v: %v, want %v", span, got, want)
		}
	}
}

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

type fakeInsights struct{ last ch.TrendQuery }

func (f *fakeInsights) RunTrend(_ context.Context, q ch.TrendQuery) ([]ch.TrendSeriesData, error) {
	f.last = q
	out := make([]ch.TrendSeriesData, len(q.Series))
	for i := range out {
		out[i] = ch.TrendSeriesData{Buckets: []ch.TrendRow{{Bucket: q.From, Value: float64(i + 2)}}, Totals: []ch.TrendRow{{Value: float64(i + 2)}}}
	}
	return out, nil
}

func (f *fakeInsights) TraceStats(_ context.Context, q ch.TraceStatsQuery) ([]ch.TraceStatsRow, error) {
	return []ch.TraceStatsRow{{Bucket: q.From.Truncate(time.Hour), Requests: 10, Errors: 2, P50Ns: 5e6, P95Ns: 2e7}}, nil
}

func TestQueryTrend(t *testing.T) {
	ins := &fakeInsights{}
	s := &Server{Traces: &fakeTraces{}, Insights: ins}
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	body := `{"series":[{"event":"$pageview"},{"event":"buy","math":"unique_users","filters":[{"property":"plan","op":"eq","value":"pro"}]}],
		"breakdown":"","interval":"day","formula":"B / A * 100","from":"2026-01-01T00:00:00Z","to":"2026-01-03T00:00:00Z"}`
	rec := call(t, s, member, "POST", "/v1/insights/trends?project_id=1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var out gen.TrendResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Buckets) != 2 || len(out.Series) != 1 || out.Series[0].Index != -1 || out.Series[0].Data[0] != 150 || out.Series[0].Total != 150 {
		t.Errorf("%+v", out)
	}
	if ins.last.Project != 1 || ins.last.Series[1].Filters[0].Value != "pro" || ins.last.Series[1].Math != "unique_users" {
		t.Errorf("query %+v", ins.last)
	}
	for b, want := range map[string]int{
		`{"series":[]}`:                                      400,
		`{"series":[{"math":"sum"}]}`:                        400,
		`{"series":[{"event":"a"}],"formula":"A / B"}`:       400,
		`{"series":[{"event":"a"}],"interval":"minute"}`:     400,
		`{"series":[{"event":"a"}]}`:                         200,
		`{"series":[{"event":"a"}],"breakdown":"$pathname"}`: 200,
		`not json`: 400,
	} {
		if rec := call(t, s, member, "POST", "/v1/insights/trends?project_id=1", b); rec.Code != want {
			t.Errorf("%s: %d %s", b, rec.Code, rec.Body)
		}
	}
	if rec := call(t, s, member, "POST", "/v1/insights/trends?project_id=2", `{"series":[{}]}`); rec.Code != 404 {
		t.Errorf("other project: %d", rec.Code)
	}

	rec = call(t, s, member, "GET", "/v1/traces/stats?project_id=1&interval=hour&from=2026-01-01T00:00:00Z&to=2026-01-01T03:00:00Z", "")
	var st gen.TraceStats
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || len(st.Points) != 3 || st.Requests != 10 || st.Points[0].ErrorRate != 0.2 || st.Points[0].P95Ms != 20 || st.Points[1].Requests != 0 {
		t.Errorf("trace stats %d %+v", rec.Code, st)
	}
}

func TestCheckWidget(t *testing.T) {
	s := &Server{}
	for kind, q := range map[string]string{"trend": `{"series":[]}`, "funnel": `{"steps":["only"]}`, "metric": `{}`} {
		if err := s.CheckWidget(kind, json.RawMessage(q)); err == nil {
			t.Errorf("%s %s accepted", kind, q)
		}
	}
	if err := s.CheckWidget("trend", json.RawMessage(`{"series":[{"event":"$pageview"}],"display":"line"}`)); err != nil {
		t.Errorf("valid trend: %v", err)
	}
}

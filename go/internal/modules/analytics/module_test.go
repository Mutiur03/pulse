package analytics_test

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
	"github.com/Mutiur03/pulse/internal/modules/analytics"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

type fakeEvents struct {
	analytics.Events
	funnel ch.FunnelQuery
}

func (f *fakeEvents) RunFunnel(_ context.Context, q ch.FunnelQuery) (ch.FunnelData, error) {
	f.funnel = q
	return ch.FunnelData{}, nil
}

func call(t *testing.T, m *analytics.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
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

type fakeAnalytics struct {
	persons   ch.PersonFilter
	retention ch.RetentionQuery
	paths     ch.PathsQuery
}

func (f *fakeAnalytics) ListPersons(_ context.Context, q ch.PersonFilter) ([]ch.PersonRow, error) {
	f.persons = q
	return []ch.PersonRow{{Person: "ann", DistinctIDs: []string{"ann", "anon-1"}, Events: 3, Traits: `{"email":"ann@example.com","$lib":"pulse-browser"}`}}, nil
}

func (f *fakeAnalytics) ResolvePerson(_ context.Context, _ uint64, id string) (string, error) {
	if id == "anon-1" {
		return "ann", nil
	}
	return id, nil
}

func (f *fakeAnalytics) GetPerson(_ context.Context, _ uint64, person string, _, _ time.Time) (ch.PersonRow, bool, error) {
	if person != "ann" {
		return ch.PersonRow{}, false, nil
	}
	return ch.PersonRow{Person: "ann", Sessions: 1}, true, nil
}

func (f *fakeAnalytics) PersonSessions(context.Context, uint64, string, time.Time, time.Time, int) ([]ch.PersonSession, error) {
	return []ch.PersonSession{{SessionID: "s1", Pageviews: 2, Replay: 1, Errors: 1}}, nil
}

func (f *fakeAnalytics) PersonEvents(context.Context, uint64, string, time.Time, time.Time, int) ([]ch.EventRecord, error) {
	return []ch.EventRecord{{Event: "$pageview", DistinctID: "anon-1", Properties: `{"$pathname":"/"}`}}, nil
}

func (f *fakeAnalytics) RunRetention(_ context.Context, q ch.RetentionQuery) (ch.RetentionData, error) {
	f.retention = q
	return ch.RetentionData{Sizes: []ch.RetentionCell{{Cohort: q.From.Truncate(24 * time.Hour), Users: 5}}}, nil
}

func (f *fakeAnalytics) RunPaths(_ context.Context, q ch.PathsQuery) ([]ch.PathRow, error) {
	f.paths = q
	return []ch.PathRow{{Path: []string{"/", "/pricing"}, Sessions: 4}}, nil
}

func TestAnalyticsEndpoints(t *testing.T) {
	a := &fakeAnalytics{}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	s := analytics.New(analytics.Deps{Events: &fakeEvents{}, People: a, Now: func() time.Time { return now }})
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}

	rec := call(t, s, member, "GET", "/v1/persons?project_id=1&search=ann", "")
	var list gen.PersonList
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list.Persons) != 1 || list.Persons[0].Traits["email"] != "ann@example.com" || list.Persons[0].Traits["$lib"] != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if a.persons.Search != "ann" || a.persons.To.Sub(a.persons.From) != 30*24*time.Hour {
		t.Errorf("filter %+v", a.persons)
	}

	// A distinct id resolves to its person.
	rec = call(t, s, member, "GET", "/v1/persons/profile?project_id=1&id=anon-1", "")
	var d gen.PersonDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if rec.Code != 200 || d.Person.Id != "ann" || len(d.Sessions) != 1 || !d.Sessions[0].HasReplay || len(d.Events) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for path, want := range map[string]int{
		"/v1/persons/profile?project_id=1&id=nobody": 404,
		"/v1/persons/profile?project_id=1&id=":       400,
		"/v1/persons/profile?project_id=2&id=ann":    404,
	} {
		if rec := call(t, s, member, "GET", path, ""); rec.Code != want {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}

	rec = call(t, s, member, "POST", "/v1/insights/retention?project_id=1", `{"start_event":"signup","period":"week","periods":4}`)
	var ret gen.RetentionResult
	_ = json.Unmarshal(rec.Body.Bytes(), &ret)
	if rec.Code != 200 || ret.Period != "week" || len(ret.Cohorts) < 8 || a.retention.StartEvent != "signup" || a.retention.To.Sub(a.retention.From) != 56*24*time.Hour {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, a.retention)
	}
	for _, b := range []string{`{"period":"month"}`, `{"periods":99}`, `nope`} {
		if rec := call(t, s, member, "POST", "/v1/insights/retention?project_id=1", b); rec.Code != 400 {
			t.Errorf("%s: %d", b, rec.Code)
		}
	}

	rec = call(t, s, member, "POST", "/v1/insights/paths?project_id=1", `{"start":"/","steps":3}`)
	var paths gen.PathsResult
	_ = json.Unmarshal(rec.Body.Bytes(), &paths)
	if rec.Code != 200 || paths.TotalSessions != 4 || len(paths.Links) != 1 || paths.Mode != "pageview" || a.paths.Start != "/" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, b := range []string{`{"mode":"clicks"}`, `{"steps":1}`} {
		if rec := call(t, s, member, "POST", "/v1/insights/paths?project_id=1", b); rec.Code != 400 {
			t.Errorf("%s: %d", b, rec.Code)
		}
	}
}

type fakeInsights struct{ last ch.TrendQuery }

func (f *fakeInsights) RunTrend(_ context.Context, q ch.TrendQuery) ([]ch.TrendSeriesData, error) {
	f.last = q
	out := make([]ch.TrendSeriesData, len(q.Series))
	for i := range out {
		out[i] = ch.TrendSeriesData{Buckets: []ch.TrendRow{{Bucket: q.From, Value: float64(i + 2)}}, Totals: []ch.TrendRow{{Value: float64(i + 2)}}}
	}
	return out, nil
}

func TestQueryTrend(t *testing.T) {
	ins := &fakeInsights{}
	s := analytics.New(analytics.Deps{Events: &fakeEvents{}, Trends: ins})
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

}

func TestCheckWidget(t *testing.T) {
	for kind, q := range map[string]string{"trend": `{"series":[]}`, "funnel": `{"steps":["only"]}`, "metric": `{}`} {
		if err := analytics.CheckWidget(kind, json.RawMessage(q)); err == nil {
			t.Errorf("%s %s accepted", kind, q)
		}
	}
	if err := analytics.CheckWidget("trend", json.RawMessage(`{"series":[{"event":"$pageview"}],"display":"line"}`)); err != nil {
		t.Errorf("valid trend: %v", err)
	}
}
func TestFunnelValidation(t *testing.T) {
	tr := &fakeEvents{}
	s := analytics.New(analytics.Deps{Events: tr})
	me := &auth.Principal{Projects: []auth.Project{{ID: 1}}}
	if rec := call(t, s, me, "POST", "/v1/funnels/query", `{"steps":["a"]}`); rec.Code != 400 {
		t.Errorf("one step: %d", rec.Code)
	}
	if rec := call(t, s, me, "POST", "/v1/funnels/query", `{"steps":["a","b"],"window_seconds":120}`); rec.Code != 200 {
		t.Errorf("valid: %d %s", rec.Code, rec.Body)
	}
	if tr.funnel.WindowSeconds != 120 || tr.funnel.Project != 1 || tr.funnel.To.Sub(tr.funnel.From) != 7*24*time.Hour {
		t.Errorf("query %+v", tr.funnel)
	}
}

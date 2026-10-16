package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/dashboards"
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

type fakeDashboards struct {
	next int64
	all  map[int64]dashboards.Dashboard
}

func (f *fakeDashboards) visible(d dashboards.Dashboard, user int64) bool {
	return d.Shared || d.CreatedBy == user
}

func (f *fakeDashboards) List(_ context.Context, project, user int64) ([]dashboards.Dashboard, error) {
	var out []dashboards.Dashboard
	for _, d := range f.all {
		if d.ProjectID == project && f.visible(d, user) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeDashboards) Get(_ context.Context, project, id, user int64) (dashboards.Dashboard, error) {
	d, ok := f.all[id]
	if !ok || d.ProjectID != project || !f.visible(d, user) {
		return d, dashboards.ErrNotFound
	}
	return d, nil
}

func (f *fakeDashboards) Create(_ context.Context, d dashboards.Dashboard) (dashboards.Dashboard, error) {
	f.next++
	d.ID = f.next
	f.all[d.ID] = d
	return d, nil
}

func (f *fakeDashboards) Update(_ context.Context, d dashboards.Dashboard, _ int64) (dashboards.Dashboard, error) {
	cur := f.all[d.ID]
	d.CreatedBy = cur.CreatedBy
	f.all[d.ID] = d
	return d, nil
}

func (f *fakeDashboards) Delete(_ context.Context, _, id int64) error {
	delete(f.all, id)
	return nil
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

func TestDashboardsAccess(t *testing.T) {
	store := &fakeDashboards{all: map[int64]dashboards.Dashboard{}}
	s := &Server{Traces: &fakeTraces{}, Dashboards: store}
	alice := &auth.Principal{User: auth.User{ID: 1}, Projects: []auth.Project{{ID: 1, Role: "member"}}}
	bob := &auth.Principal{User: auth.User{ID: 2}, Projects: []auth.Project{{ID: 1, Role: "member"}}}
	owner := &auth.Principal{User: auth.User{ID: 3}, Projects: []auth.Project{{ID: 1, Role: "owner"}}}
	outsider := &auth.Principal{User: auth.User{ID: 4}, Projects: []auth.Project{{ID: 2, Role: "owner"}}}

	widgets := `[{"id":"w1","title":"Views","kind":"trend","width":2,"query":{"series":[{"event":"$pageview"}],"display":"line"}},
		{"id":"w2","kind":"funnel","query":{"steps":["a","b"]}},{"id":"w3","kind":"metric","query":{"name":"cpu"}},{"id":"w4","kind":"traces","query":{}}]`
	rec := call(t, s, alice, "POST", "/v1/projects/1/dashboards", `{"name":"Growth","widgets":`+widgets+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var d gen.Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if !d.CanEdit || d.Shared || len(d.Widgets) != 4 || *d.Widgets[0].Width != 2 || (*d.Widgets[0].Query)["display"] != "line" {
		t.Fatalf("%+v", d)
	}
	path := "/v1/projects/1/dashboards/1"
	// Private: only Alice sees it.
	if rec := call(t, s, bob, "GET", path, ""); rec.Code != 404 {
		t.Errorf("bob sees private: %d", rec.Code)
	}
	if rec := call(t, s, owner, "PUT", path, `{"name":"x"}`); rec.Code != 404 {
		t.Errorf("owner edits private: %d", rec.Code)
	}
	// Share it: Bob can read but not edit; the project owner can edit.
	if rec := call(t, s, alice, "PUT", path, `{"name":"Growth","shared":true,"widgets":`+widgets+`}`); rec.Code != 200 {
		t.Fatalf("share: %d %s", rec.Code, rec.Body)
	}
	rec = call(t, s, bob, "GET", path, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if rec.Code != 200 || d.CanEdit {
		t.Errorf("bob reads shared: %d %+v", rec.Code, d)
	}
	if rec := call(t, s, bob, "PUT", path, `{"name":"mine"}`); rec.Code != 403 {
		t.Errorf("bob edits: %d", rec.Code)
	}
	if rec := call(t, s, bob, "DELETE", path, ""); rec.Code != 403 {
		t.Errorf("bob deletes: %d", rec.Code)
	}
	if rec := call(t, s, owner, "PUT", path, `{"name":"Renamed","shared":true}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Renamed") {
		t.Errorf("owner edits shared: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, outsider, "GET", path, ""); rec.Code != 404 {
		t.Errorf("outsider: %d", rec.Code)
	}
	if rec := call(t, s, bob, "GET", "/v1/projects/1/dashboards", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Renamed") {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	for _, b := range []string{
		`{"name":""}`,
		`{"name":"x","widgets":[{"id":"a","kind":"trend","query":{"series":[]}}]}`,
		`{"name":"x","widgets":[{"id":"a","kind":"funnel","query":{"steps":["only"]}}]}`,
		`{"name":"x","widgets":[{"id":"a","kind":"metric","query":{}}]}`,
		`{"name":"x","widgets":[{"id":"a","kind":"pie"}]}`,
	} {
		if rec := call(t, s, alice, "POST", "/v1/projects/1/dashboards", b); rec.Code != 400 {
			t.Errorf("%s: %d %s", b, rec.Code, rec.Body)
		}
	}
	if rec := call(t, s, alice, "DELETE", path, ""); rec.Code != 204 {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := call(t, s, alice, "GET", path, ""); rec.Code != 404 {
		t.Errorf("after delete: %d", rec.Code)
	}
}

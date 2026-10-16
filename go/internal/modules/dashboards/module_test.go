package dashboards_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/modules/dashboards"
	"github.com/go-chi/chi/v5"
)

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

func TestDashboardsAccess(t *testing.T) {
	store := &fakeDashboards{all: map[int64]dashboards.Dashboard{}}
	m := dashboards.New(dashboards.Deps{Repository: store, CheckWidget: checkWidget})
	alice := &auth.Principal{User: auth.User{ID: 1}, Projects: []auth.Project{{ID: 1, Role: "member"}}}
	bob := &auth.Principal{User: auth.User{ID: 2}, Projects: []auth.Project{{ID: 1, Role: "member"}}}
	owner := &auth.Principal{User: auth.User{ID: 3}, Projects: []auth.Project{{ID: 1, Role: "owner"}}}
	outsider := &auth.Principal{User: auth.User{ID: 4}, Projects: []auth.Project{{ID: 2, Role: "owner"}}}

	widgets := `[{"id":"w1","title":"Views","kind":"trend","width":2,"query":{"series":[{"event":"$pageview"}],"display":"line"}},
		{"id":"w2","kind":"funnel","query":{"steps":["a","b"]}},{"id":"w3","kind":"metric","query":{"name":"cpu"}},{"id":"w4","kind":"traces","query":{}}]`
	rec := call(t, m, alice, "POST", "/v1/projects/1/dashboards", `{"name":"Growth","widgets":`+widgets+`}`)
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
	if rec := call(t, m, bob, "GET", path, ""); rec.Code != 404 {
		t.Errorf("bob sees private: %d", rec.Code)
	}
	if rec := call(t, m, owner, "PUT", path, `{"name":"x"}`); rec.Code != 404 {
		t.Errorf("owner edits private: %d", rec.Code)
	}
	// Share it: Bob can read but not edit; the project owner can edit.
	if rec := call(t, m, alice, "PUT", path, `{"name":"Growth","shared":true,"widgets":`+widgets+`}`); rec.Code != 200 {
		t.Fatalf("share: %d %s", rec.Code, rec.Body)
	}
	rec = call(t, m, bob, "GET", path, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if rec.Code != 200 || d.CanEdit {
		t.Errorf("bob reads shared: %d %+v", rec.Code, d)
	}
	if rec := call(t, m, bob, "PUT", path, `{"name":"mine"}`); rec.Code != 403 {
		t.Errorf("bob edits: %d", rec.Code)
	}
	if rec := call(t, m, bob, "DELETE", path, ""); rec.Code != 403 {
		t.Errorf("bob deletes: %d", rec.Code)
	}
	if rec := call(t, m, owner, "PUT", path, `{"name":"Renamed","shared":true}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Renamed") {
		t.Errorf("owner edits shared: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, m, outsider, "GET", path, ""); rec.Code != 404 {
		t.Errorf("outsider: %d", rec.Code)
	}
	if rec := call(t, m, bob, "GET", "/v1/projects/1/dashboards", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Renamed") {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	for _, b := range []string{
		`{"name":""}`,
		`{"name":"x","widgets":[{"id":"a","kind":"trend","query":{"series":[]}}]}`,
		`{"name":"x","widgets":[{"id":"a","kind":"funnel","query":{"steps":["only"]}}]}`,
		`{"name":"x","widgets":[{"id":"a","kind":"metric","query":{}}]}`,
		`{"name":"x","widgets":[{"id":"a","kind":"pie"}]}`,
	} {
		if rec := call(t, m, alice, "POST", "/v1/projects/1/dashboards", b); rec.Code != 400 {
			t.Errorf("%s: %d %s", b, rec.Code, rec.Body)
		}
	}
	if rec := call(t, m, alice, "DELETE", path, ""); rec.Code != 204 {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := call(t, m, alice, "GET", path, ""); rec.Code != 404 {
		t.Errorf("after delete: %d", rec.Code)
	}
}

// checkWidget stands in for the analytics query validation.
func checkWidget(kind string, q json.RawMessage) error {
	switch {
	case kind == "trend" && strings.Contains(string(q), `"series":[]`),
		kind == "funnel" && strings.Contains(string(q), `["only"]`),
		kind == "metric" && string(q) == "{}":
		return errors.New("invalid query")
	}
	return nil
}

func TestDashboardsDisabledAndLimits(t *testing.T) {
	alice := &auth.Principal{User: auth.User{ID: 1}, Projects: []auth.Project{{ID: 1, Role: "member"}}}
	if rec := call(t, dashboards.New(dashboards.Deps{}), alice, "GET", "/v1/projects/1/dashboards", ""); rec.Code != 503 {
		t.Errorf("unconfigured: %d", rec.Code)
	}
	m := dashboards.New(dashboards.Deps{Repository: &fakeDashboards{all: map[int64]dashboards.Dashboard{}}, CheckWidget: checkWidget})
	if rec := call(t, m, nil, "GET", "/v1/projects/1/dashboards", ""); rec.Code != 404 {
		t.Errorf("anonymous: %d", rec.Code)
	}
	big := `{"name":"` + strings.Repeat("x", 1<<18) + `"}`
	if rec := call(t, m, alice, "POST", "/v1/projects/1/dashboards", big); rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid JSON") {
		t.Errorf("oversized body: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, m, alice, "POST", "/v1/projects/1/dashboards", "{"); rec.Code != 400 || strings.TrimSpace(rec.Body.String()) != `{"error":"invalid JSON"}` {
		t.Errorf("bad JSON: %d %s", rec.Code, rec.Body)
	}
}

func call(t *testing.T, m *dashboards.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
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

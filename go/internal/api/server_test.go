package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/legacy"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/go-chi/chi/v5"
)

type fakeTraces struct {
	TraceReader
	lastFilter ch.TraceFilter
	funnel     ch.FunnelQuery
}

func (f *fakeTraces) ListTraces(_ context.Context, tf ch.TraceFilter) ([]ch.TraceSummaryRow, error) {
	f.lastFilter = tf
	return []ch.TraceSummaryRow{{TraceID: "abc", RootName: "click", Browser: 2, HTTPStatus: 402}}, nil
}

func (f *fakeTraces) IssueTrends(_ context.Context, _ uint64, ids []uint64, _ time.Time) (map[uint64][]int64, error) {
	out := map[uint64][]int64{}
	for _, id := range ids {
		out[id] = make([]int64, 24)
	}
	return out, nil
}

func (f *fakeTraces) RunFunnel(_ context.Context, q ch.FunnelQuery) (ch.FunnelData, error) {
	f.funnel = q
	return ch.FunnelData{}, nil
}

type fakeIssues struct{ IssueStore }

func (fakeIssues) Get(_ context.Context, id int64) (issues.Issue, error) {
	return issues.Issue{ID: id, ProjectID: 2, Status: issues.Unresolved}, nil
}

func (fakeIssues) SetStatus(_ context.Context, id int64, st issues.Status) (issues.Issue, error) {
	return issues.Issue{ID: id, ProjectID: 2, Status: st}, nil
}

func (fakeIssues) List(context.Context, issues.ListFilter) ([]issues.Issue, int64, error) {
	return []issues.Issue{{ID: 5, ProjectID: 1}}, 1, nil
}

type fakeKeys struct{ purged []string }

func (f *fakeKeys) List(context.Context, int64) ([]keys.APIKey, error) { return nil, nil }
func (f *fakeKeys) Rotate(_ context.Context, p int64) (keys.APIKey, []string, error) {
	return keys.APIKey{ID: 9, ProjectID: p, PublicKey: "pk_new"}, []string{"pk_old"}, nil
}
func (f *fakeKeys) Create(_ context.Context, p int64) (keys.APIKey, error) {
	return keys.APIKey{ID: 10, ProjectID: p, PublicKey: "pk_added"}, nil
}
func (f *fakeKeys) Revoke(_ context.Context, p, id int64) (keys.APIKey, error) {
	if id != 7 {
		return keys.APIKey{}, keys.ErrKeyNotFound
	}
	return keys.APIKey{ID: 7, ProjectID: p, PublicKey: "pk_revoked"}, nil
}
func (f *fakeKeys) Purge(_ context.Context, k string) error {
	f.purged = append(f.purged, k)
	return nil
}

func call(t *testing.T, s *Server, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h := legacy.HandlerFromMux(s, r)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if p != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestProjectAccess(t *testing.T) {
	tr := &fakeTraces{}
	s := &Server{Traces: tr, Issues: fakeIssues{}, Keys: &fakeKeys{}}
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/v1/traces", "", 200},
		{"GET", "/v1/traces?project_id=1", "", 200},
		{"GET", "/v1/traces?project_id=2", "", 404},
		{"GET", "/v1/issues?project_id=2", "", 404},
		{"GET", "/v1/issues/3", "", 404}, // issue belongs to project 2
		{"PATCH", "/v1/issues/3", `{"status":"resolved"}`, 404},
		{"GET", "/v1/projects/2/keys", "", 404},
		{"POST", "/v1/projects/1/keys/rotate", "", 403}, // members cannot rotate
	}
	for _, c := range cases {
		if rec := call(t, s, member, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %s: got %d want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body)
		}
	}
	if rec := call(t, s, nil, "GET", "/v1/traces", ""); rec.Code != 401 {
		t.Errorf("no principal: %d", rec.Code)
	}
}

func TestRotatePurgesOldKeys(t *testing.T) {
	k := &fakeKeys{}
	s := &Server{Keys: k}
	owner := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "owner"}}}
	rec := call(t, s, owner, "POST", "/v1/projects/1/keys/rotate", "")
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), "pk_new") {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body)
	}
	if len(k.purged) != 1 || k.purged[0] != "pk_old" {
		t.Errorf("old key must be purged from the gateway cache: %v", k.purged)
	}
}

func TestTraceSearchParams(t *testing.T) {
	tr := &fakeTraces{}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s := &Server{Traces: tr, Now: func() time.Time { return now }}
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

func TestFunnelValidation(t *testing.T) {
	tr := &fakeTraces{}
	s := &Server{Traces: tr}
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

func TestBuildFunnel(t *testing.T) {
	q := ch.FunnelQuery{Steps: []string{"view", "checkout", "paid"}}
	d := ch.FunnelData{
		Levels: []ch.FunnelLevel{{Level: 0, Users: 4}, {Level: 1, Users: 50}, {Level: 2, Users: 30}, {Level: 3, Users: 20}},
		Health: []ch.FunnelHealth{
			{Step: 2, Kind: "dropped", Users: 30, Sessions: 31, ErrorSessions: 18, P95Ns: 2.5e9},
			{Step: 2, Kind: "continued", Users: 20, Sessions: 20, ErrorSessions: 1, P95Ns: 1.2e8},
		},
		Errors: []ch.FunnelError{{Step: 2, Kind: "dropped", IssueID: 7, Title: "card declined", Users: 18, Count: 25}},
	}
	r := BuildFunnel(q, d)
	got := []int64{r.Steps[0].Users, r.Steps[1].Users, r.Steps[2].Users}
	if got[0] != 100 || got[1] != 50 || got[2] != 20 {
		t.Fatalf("reached %v", got)
	}
	if r.Steps[1].ConversionFromPrevious != 0.5 || r.Steps[2].ConversionOverall != 0.2 {
		t.Errorf("conversion %+v", r.Steps)
	}
	dr := r.Steps[1].Dropped
	if dr.Users != 30 || dr.ErrorSessions != 18 || dr.P95LatencyMs != 2500 || len(dr.TopErrors) != 1 || dr.TopErrors[0].IssueId != 7 {
		t.Errorf("dropped %+v", dr)
	}
	if r.Steps[0].Dropped.Users != 50 {
		t.Errorf("step 1 dropped users without sessions should fall back to level counts: %+v", r.Steps[0].Dropped)
	}
	if r.Steps[2].Dropped != nil {
		t.Error("the last step has no drop-off")
	}
}

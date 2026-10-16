package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/legacy"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/go-chi/chi/v5"
)

type fakeTraces struct {
	TraceReader
}

func (f *fakeTraces) IssueTrends(_ context.Context, _ uint64, ids []uint64, _ time.Time) (map[uint64][]int64, error) {
	out := map[uint64][]int64{}
	for _, id := range ids {
		out[id] = make([]int64, 24)
	}
	return out, nil
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
	if rec := call(t, s, nil, "GET", "/v1/issues", ""); rec.Code != 401 {
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

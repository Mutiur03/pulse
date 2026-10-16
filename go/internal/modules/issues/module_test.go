package issues_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/modules/issues"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

type fakeOccurrences struct{}

func (fakeOccurrences) IssueTrends(_ context.Context, _ uint64, ids []uint64, _ time.Time) (map[uint64][]int64, error) {
	out := map[uint64][]int64{}
	for _, id := range ids {
		out[id] = make([]int64, 24)
	}
	return out, nil
}

func (fakeOccurrences) ErrorsByIssue(context.Context, uint64, uint64, int) ([]ch.ErrorRecord, error) {
	return []ch.ErrorRecord{{Message: "boom"}}, nil
}

type fakeIssues struct{}

func (fakeIssues) Get(_ context.Context, id int64) (issues.Issue, error) {
	if id == 9 {
		return issues.Issue{}, issues.ErrNotFound
	}
	return issues.Issue{ID: id, ProjectID: id % 2, Status: issues.Unresolved}, nil
}

func (fakeIssues) SetStatus(_ context.Context, id int64, st issues.Status) (issues.Issue, error) {
	return issues.Issue{ID: id, ProjectID: id % 2, Status: st}, nil
}

func (fakeIssues) List(_ context.Context, f issues.ListFilter) ([]issues.Issue, int64, error) {
	return []issues.Issue{{ID: 5, ProjectID: f.ProjectID}}, 1, nil
}

func call(t *testing.T, m *issues.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
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

func TestIssueAccess(t *testing.T) {
	m := issues.New(issues.Deps{Repository: fakeIssues{}, Occurrences: fakeOccurrences{}})
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/v1/issues?project_id=2", "", 404},
		{"GET", "/v1/issues?project_id=1&status=bogus", "", 400},
		{"GET", "/v1/issues?project_id=1&limit=500", "", 400},
		{"GET", "/v1/issues?project_id=1&offset=-1", "", 400},
		{"GET", "/v1/issues/4", "", 404}, // issue belongs to project 0
		{"GET", "/v1/issues/9", "", 404},
		{"GET", "/v1/issues/3", "", 200},
		{"PATCH", "/v1/issues/4", `{"status":"resolved"}`, 404},
		{"PATCH", "/v1/issues/3", `{"status":"gone"}`, 400},
		{"PATCH", "/v1/issues/3", `{"status":"resolved"}`, 200},
	} {
		if rec := call(t, m, member, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %s: got %d want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body)
		}
	}
	if rec := call(t, m, nil, "GET", "/v1/issues", ""); rec.Code != 401 {
		t.Errorf("no principal: %d", rec.Code)
	}
	rec := call(t, m, member, "GET", "/v1/issues?project_id=1", "")
	var list gen.IssueList
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || list.Total != 1 || list.Limit != 25 || list.Issues[0].Trend == nil || len(*list.Issues[0].Trend) != 24 {
		t.Errorf("list %d %s", rec.Code, rec.Body)
	}
	rec = call(t, m, member, "GET", "/v1/issues/3", "")
	var d gen.IssueDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if len(d.RecentEvents) != 1 || d.RecentEvents[0].Message != "boom" {
		t.Errorf("detail %s", rec.Body)
	}
}

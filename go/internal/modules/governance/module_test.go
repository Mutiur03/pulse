package governance_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/governance"
	governancemod "github.com/Mutiur03/pulse/internal/modules/governance"
)

func call(t *testing.T, m *governancemod.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
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

type memAudit struct{ entries []audit.Entry }

func (m *memAudit) Record(_ context.Context, e audit.Entry) error {
	m.entries = append(m.entries, e)
	return nil
}

func (m *memAudit) List(context.Context, int64, int, int64) ([]audit.Entry, error) {
	return m.entries, nil
}

type fakeGovernance struct {
	saved    *governance.Settings
	requests []governance.Request
}

func (f *fakeGovernance) Settings(_ context.Context, p int64) (governance.Settings, error) {
	if f.saved != nil {
		return *f.saved, nil
	}
	return governance.Defaults(p), nil
}

func (f *fakeGovernance) SaveSettings(_ context.Context, s governance.Settings) (governance.Settings, error) {
	s.Saved = true
	f.saved = &s
	return s, nil
}

func (f *fakeGovernance) CreateRequest(_ context.Context, r governance.Request) (governance.Request, error) {
	r.ID, r.Status = int64(len(f.requests)+1), governance.Pending
	f.requests = append(f.requests, r)
	return r, nil
}

func (f *fakeGovernance) ListRequests(_ context.Context, p int64, _ int) ([]governance.Request, error) {
	return f.requests, nil
}

func (f *fakeGovernance) GetRequest(_ context.Context, p, id int64) (governance.Request, error) {
	for _, r := range f.requests {
		if r.ID == id && r.ProjectID == p {
			return r, nil
		}
	}
	return governance.Request{}, governance.ErrNotFound
}

type fakeLinker struct{}

func (fakeLinker) ResolvePerson(_ context.Context, _ uint64, id string) (string, error) {
	if id == "anon-1" {
		return "user-1", nil
	}
	return id, nil
}

func (fakeLinker) LinkedIDs(_ context.Context, _ uint64, ids []string) ([]string, error) {
	return []string{"anon-1", "user-1", "anon-2"}, nil
}

func TestDataGovernanceEndpoints(t *testing.T) {
	g, rec := &fakeGovernance{}, &memAudit{}
	s := governancemod.New(governancemod.Deps{Store: g, Persons: fakeLinker{}, Audit: rec})
	owner := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "owner"}}}
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}

	r := call(t, s, member, "GET", "/v1/projects/1/data-settings", "")
	var st gen.DataSettings
	_ = json.Unmarshal(r.Body.Bytes(), &st)
	if r.Code != 200 || st.Saved || !st.Scrubbing.Cards || st.Limits.LogsDays != 3 || st.Retention.SourcemapsDays != 0 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}

	body := `{"scrubbing":{"emails":true,"cards":true,"ips":false,"keys":["password"],"patterns":["ord-\\d+"]},
		"retention":{"events_days":1,"traces_days":7,"logs_days":3,"errors_days":90,"replays_days":30,"sourcemaps_days":0}}`
	if r = call(t, s, member, "PUT", "/v1/projects/1/data-settings", body); r.Code != 403 {
		t.Fatalf("member save: %d", r.Code)
	}
	if r = call(t, s, owner, "PUT", "/v1/projects/1/data-settings", strings.Replace(body, `"events_days":1`, `"events_days":31`, 1)); r.Code != 400 {
		t.Fatalf("over cap: %d", r.Code)
	}
	if r = call(t, s, owner, "PUT", "/v1/projects/1/data-settings", strings.Replace(body, `ord-\\d+`, `(`, 1)); r.Code != 400 {
		t.Fatalf("bad regex: %d", r.Code)
	}
	r = call(t, s, owner, "PUT", "/v1/projects/1/data-settings", body)
	if r.Code != 200 || g.saved == nil || g.saved.Retention.EventsDays != 1 || len(rec.entries) != 1 || rec.entries[0].Action != governance.ActionSettingsUpdate {
		t.Fatalf("save %d %s %+v", r.Code, r.Body, rec.entries)
	}

	r = call(t, s, member, "POST", "/v1/projects/1/data-settings/preview",
		`{"scrubbing":{"emails":true,"cards":false,"ips":false,"keys":["password"],"patterns":[]},"sample":"{\"email\":\"a@b.io\",\"password\":\"x\"}"}`)
	var pv gen.ScrubPreview
	_ = json.Unmarshal(r.Body.Bytes(), &pv)
	if r.Code != 200 || strings.Contains(pv.Scrubbed, "a@b.io") || strings.Contains(pv.Scrubbed, `"x"`) {
		t.Fatalf("preview %d %s", r.Code, r.Body)
	}

	if r = call(t, s, member, "POST", "/v1/projects/1/deletion-requests", `{"distinct_id":"anon-1"}`); r.Code != 403 {
		t.Fatalf("member delete: %d", r.Code)
	}
	if r = call(t, s, owner, "POST", "/v1/projects/1/deletion-requests", `{"distinct_id":" "}`); r.Code != 400 {
		t.Fatalf("empty id: %d", r.Code)
	}
	r = call(t, s, owner, "POST", "/v1/projects/1/deletion-requests", `{"distinct_id":"anon-1","reason":"GDPR request"}`)
	var dr gen.DeletionRequest
	_ = json.Unmarshal(r.Body.Bytes(), &dr)
	if r.Code != 202 || dr.PersonKey != "user-1" || len(dr.DistinctIds) != 3 || dr.Status != "pending" {
		t.Fatalf("create %d %s", r.Code, r.Body)
	}
	if last := rec.entries[len(rec.entries)-1]; last.Action != governance.ActionDeletionRequest || last.Target != "anon-1" {
		t.Errorf("audit %+v", last)
	}

	if r = call(t, s, member, "GET", "/v1/projects/1/deletion-requests/1", ""); r.Code != 200 {
		t.Fatalf("get %d", r.Code)
	}
	if r = call(t, s, member, "GET", "/v1/projects/1/deletion-requests/9", ""); r.Code != 404 {
		t.Fatalf("missing %d", r.Code)
	}
	var list []gen.DeletionRequest
	r = call(t, s, member, "GET", "/v1/projects/1/deletion-requests", "")
	_ = json.Unmarshal(r.Body.Bytes(), &list)
	if r.Code != 200 || len(list) != 1 {
		t.Fatalf("list %d %s", r.Code, r.Body)
	}
	if r = call(t, s, &auth.Principal{}, "GET", "/v1/projects/1/data-settings", ""); r.Code != 404 {
		t.Fatalf("outsider %d", r.Code)
	}
}

func TestAuditLog(t *testing.T) {
	al := &memAudit{entries: []audit.Entry{{ID: 1, Action: "key.create", ActorEmail: "o@example.com"}}}
	s := governancemod.New(governancemod.Deps{AuditLog: al})
	owner := &auth.Principal{Orgs: []auth.Org{{ID: 1, Role: "owner"}}}
	member := &auth.Principal{Orgs: []auth.Org{{ID: 1, Role: "member"}}}
	if rec := call(t, s, member, "GET", "/v1/orgs/1/audit-log", ""); rec.Code != 403 {
		t.Fatalf("member reads audit log: %d", rec.Code)
	}
	if rec := call(t, s, owner, "GET", "/v1/orgs/2/audit-log", ""); rec.Code != 404 {
		t.Fatalf("other org: %d", rec.Code)
	}
	if rec := call(t, s, owner, "GET", "/v1/orgs/1/audit-log", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "key.create") {
		t.Fatalf("owner audit log: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, governancemod.New(governancemod.Deps{}), owner, "GET", "/v1/orgs/1/audit-log", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("no audit log: %d %s", rec.Code, rec.Body)
	}
}

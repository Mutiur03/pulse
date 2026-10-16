package projects_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/keys"
	projectsmod "github.com/Mutiur03/pulse/internal/modules/projects"
	"github.com/Mutiur03/pulse/internal/projects"
)

func call(t *testing.T, m *projectsmod.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
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

func TestProjectAccess(t *testing.T) {
	s := projectsmod.New(projectsmod.Deps{Keys: &fakeKeys{}})
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/v1/projects/2/keys", "", 404},
		{"POST", "/v1/projects/1/keys/rotate", "", 403}, // members cannot rotate
	}
	for _, c := range cases {
		if rec := call(t, s, member, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %s: got %d want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body)
		}
	}
}

func TestRotatePurgesOldKeys(t *testing.T) {
	k := &fakeKeys{}
	s := projectsmod.New(projectsmod.Deps{Keys: k})
	owner := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "owner"}}}
	rec := call(t, s, owner, "POST", "/v1/projects/1/keys/rotate", "")
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), "pk_new") {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body)
	}
	if len(k.purged) != 1 || k.purged[0] != "pk_old" {
		t.Errorf("old key must be purged from the gateway cache: %v", k.purged)
	}
}

type fakeProjects struct {
	created []string
	renamed map[int64]string
}

func (f *fakeProjects) List(context.Context, int64) ([]auth.Project, error) {
	return []auth.Project{{ID: 1, Name: "One", Role: "owner"}}, nil
}

func (f *fakeProjects) Create(_ context.Context, _, _ int64, name string) (auth.Project, keys.APIKey, error) {
	f.created = append(f.created, name)
	return auth.Project{ID: 5, Name: name, Role: "owner"}, keys.APIKey{ID: 1, ProjectID: 5, PublicKey: "pk_first"}, nil
}

func (f *fakeProjects) Rename(_ context.Context, id int64, name string) (string, error) {
	f.renamed[id] = name
	return name, nil
}

func TestProjectsAndKeys(t *testing.T) {
	fp := &fakeProjects{renamed: map[int64]string{}}
	fk := &fakeKeys{}
	s := projectsmod.New(projectsmod.Deps{Projects: fp, Keys: fk})
	me := &auth.Principal{User: auth.User{ID: 1}, Projects: []auth.Project{{ID: 1, Role: "owner"}, {ID: 2, Role: "member"}}}
	cases := []struct {
		method, path, body string
		want               int
		contains           string
	}{
		{"GET", "/v1/projects", "", 200, `"name":"One"`},
		{"POST", "/v1/projects", `{"name":"  Shop  "}`, 201, `"public_key":"pk_first"`},
		{"POST", "/v1/projects", `{"name":""}`, 400, ""},
		{"POST", "/v1/projects", `{"name":"` + strings.Repeat("x", 81) + `"}`, 400, ""},
		{"PATCH", "/v1/projects/1", `{"name":"Renamed"}`, 200, `"name":"Renamed"`},
		{"PATCH", "/v1/projects/2", `{"name":"Nope"}`, 403, ""},
		{"PATCH", "/v1/projects/3", `{"name":"Nope"}`, 404, ""},
		{"POST", "/v1/projects/1/keys", "", 201, "pk_added"},
		{"POST", "/v1/projects/2/keys", "", 403, ""},
		{"POST", "/v1/projects/3/keys", "", 404, ""},
		{"POST", "/v1/projects/1/keys/7/revoke", "", 200, "pk_revoked"},
		{"POST", "/v1/projects/1/keys/8/revoke", "", 404, ""},
		{"POST", "/v1/projects/2/keys/7/revoke", "", 403, ""},
	}
	for _, c := range cases {
		rec := call(t, s, me, c.method, c.path, c.body)
		if rec.Code != c.want || !strings.Contains(rec.Body.String(), c.contains) {
			t.Errorf("%s %s: got %d %s, want %d containing %q", c.method, c.path, rec.Code, rec.Body, c.want, c.contains)
		}
	}
	if len(fp.created) != 1 || fp.created[0] != "Shop" {
		t.Errorf("created: %v", fp.created)
	}
	if fp.renamed[2] != "" || fp.renamed[1] != "Renamed" {
		t.Errorf("renamed: %v", fp.renamed)
	}
	if len(fk.purged) != 1 || fk.purged[0] != "pk_revoked" {
		t.Errorf("revoked key must be purged: %v", fk.purged)
	}
	if rec := call(t, s, nil, "POST", "/v1/projects", `{"name":"x"}`); rec.Code != 401 {
		t.Errorf("anonymous create: %d", rec.Code)
	}
}

func TestCleanName(t *testing.T) {
	if n, err := projects.CleanName("  a b "); err != nil || n != "a b" {
		t.Errorf("%q %v", n, err)
	}
}

func TestOriginsRBAC(t *testing.T) {
	s := projectsmod.New(projectsmod.Deps{})
	owner := &auth.Principal{Orgs: []auth.Org{{ID: 1, Role: "owner"}}, Projects: []auth.Project{{ID: 1, Role: "owner", OrgID: 1}}}
	member := &auth.Principal{Orgs: []auth.Org{{ID: 1, Role: "member"}}, Projects: []auth.Project{{ID: 1, Role: "member", OrgID: 1}}}
	if rec := call(t, s, member, "POST", "/v1/projects/1/allowed-origins", `{"origins":["https://a.example"]}`); rec.Code != 403 {
		t.Fatalf("member sets origins: %d", rec.Code)
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/allowed-origins", `{"origins":["not an origin"]}`); rec.Code != 400 {
		t.Fatalf("bad origin: %d", rec.Code)
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/allowed-origins", `{"origins":["https://a.example"]}`); rec.Code != 503 {
		t.Fatalf("unconfigured origins: %d", rec.Code)
	}
	if rec := call(t, s, member, "GET", "/v1/projects/1/allowed-origins", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"origins":[]`) {
		t.Fatalf("list origins: %d %s", rec.Code, rec.Body)
	}
}

func TestCreateProjectOrg(t *testing.T) {
	s := projectsmod.New(projectsmod.Deps{Projects: &fakeProjects{renamed: map[int64]string{}}})
	owner := &auth.Principal{User: auth.User{ID: 1}, Orgs: []auth.Org{{ID: 7, Role: "owner"}, {ID: 8, Role: "member"}}}
	member := &auth.Principal{User: auth.User{ID: 2}, Orgs: []auth.Org{{ID: 8, Role: "member"}}}
	for _, c := range []struct {
		p    *auth.Principal
		body string
		want int
	}{
		{owner, `{"name":"P","org_id":7}`, 201},
		{owner, `{"name":"P","org_id":8}`, 403},
		{owner, `{"name":"P","org_id":9}`, 404},
		{owner, `{"name":"P"}`, 201},
		{member, `{"name":"P"}`, 403},
	} {
		if rec := call(t, s, c.p, "POST", "/v1/projects", c.body); rec.Code != c.want {
			t.Errorf("%s: %d want %d (%s)", c.body, rec.Code, c.want, rec.Body)
		}
	}
}

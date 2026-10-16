package sourcemaps_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps"
	"github.com/go-chi/chi/v5"
)

type fakeReleases struct {
	arts    []sourcemaps.Artifact
	revoked int64
}

func (f *fakeReleases) ListReleases(context.Context, int64, int) ([]sourcemaps.Release, error) {
	return []sourcemaps.Release{{ID: 1, Version: "v1", Artifacts: int64(len(f.arts))}}, nil
}

func (f *fakeReleases) GetRelease(_ context.Context, _ int64, v string) (sourcemaps.Release, []sourcemaps.Artifact, error) {
	if v != "v1" {
		return sourcemaps.Release{}, nil, sourcemaps.ErrNotFound
	}
	return sourcemaps.Release{ID: 1, Version: v}, f.arts, nil
}

func (f *fakeReleases) EnsureRelease(_ context.Context, p int64, v string) (sourcemaps.Release, error) {
	return sourcemaps.Release{ID: 1, ProjectID: p, Version: v}, nil
}

func (f *fakeReleases) PutArtifact(_ context.Context, a sourcemaps.Artifact) (sourcemaps.Artifact, error) {
	a.ID = int64(len(f.arts) + 1)
	a.CreatedAt = time.Now()
	f.arts = append(f.arts, a)
	return a, nil
}

func (f *fakeReleases) CreateToken(_ context.Context, p int64, name string, _ int64) (sourcemaps.Token, string, error) {
	return sourcemaps.Token{ID: 9, ProjectID: p, Name: name, Hint: "abcd"}, "pul_secretabcd", nil
}

func (f *fakeReleases) ListTokens(context.Context, int64) ([]sourcemaps.Token, error) {
	return nil, nil
}

func (f *fakeReleases) RevokeToken(_ context.Context, _, id int64) (sourcemaps.Token, error) {
	f.revoked = id
	return sourcemaps.Token{ID: id}, nil
}

func (f *fakeReleases) Authenticate(_ context.Context, secret string) (int64, error) {
	if secret == "pul_good" {
		return 1, nil
	}
	return 0, sourcemaps.ErrNotFound
}

type memObjects map[string][]byte

func (m memObjects) Put(_ context.Context, k string, d []byte) error { m[k] = d; return nil }

func upload(t *testing.T, s *sourcemaps.Module, token, path string, files map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, data := range files {
		w, _ := mw.CreateFormFile(name, "x.map")
		_, _ = w.Write(data)
	}
	_ = mw.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r := chi.NewRouter()
	h := auth.Middleware(nil, auth.CSRF{})(register(s, r))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUploadSourcemaps(t *testing.T) {
	m, err := os.ReadFile("../../symbolicate/testdata/app.min.js.map")
	if err != nil {
		t.Fatal(err)
	}
	rel, objs := &fakeReleases{}, memObjects{}
	s := sourcemaps.New(sourcemaps.Deps{Repository: rel, Objects: objs})

	rec := upload(t, s, "pul_good", "/v1/projects/1/releases/web@1.0/sourcemaps", map[string][]byte{"~/static/app.min.js.map": m})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var out []gen.Artifact
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out) != 1 || out[0].Name != "~/static/app.min.js.map" || len(objs) != 1 {
		t.Fatalf("stored %+v %d", out, len(objs))
	}
	if _, ok := objs[sourcemaps.ObjectKey(1, sourcemaps.SHA256(m))]; !ok {
		t.Error("object key")
	}

	for name, c := range map[string]struct {
		token, path string
		files       map[string][]byte
		want        int
	}{
		"no token":      {"", "/v1/projects/1/releases/v1/sourcemaps", map[string][]byte{"a.js.map": m}, http.StatusUnsupportedMediaType},
		"bad token":     {"pul_bad", "/v1/projects/1/releases/v1/sourcemaps", map[string][]byte{"a.js.map": m}, http.StatusUnauthorized},
		"other project": {"pul_good", "/v1/projects/2/releases/v1/sourcemaps", map[string][]byte{"a.js.map": m}, http.StatusUnauthorized},
		"bad release":   {"pul_good", "/v1/projects/1/releases/a%20b/sourcemaps", map[string][]byte{"a.js.map": m}, http.StatusBadRequest},
		"not a map":     {"pul_good", "/v1/projects/1/releases/v1/sourcemaps", map[string][]byte{"a.js.map": []byte("{}")}, http.StatusBadRequest},
		"no files":      {"pul_good", "/v1/projects/1/releases/v1/sourcemaps", nil, http.StatusBadRequest},
	} {
		if rec := upload(t, s, c.token, c.path, c.files); rec.Code != c.want {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestUploadTokens(t *testing.T) {
	rel := &fakeReleases{}
	s := sourcemaps.New(sourcemaps.Deps{Repository: rel})
	owner := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "owner"}}}
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}

	rec := call(t, s, owner, "POST", "/v1/projects/1/upload-tokens", `{"name":"ci"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"secret":"pul_secretabcd"`) {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, member, "POST", "/v1/projects/1/upload-tokens", `{"name":"ci"}`); rec.Code != http.StatusForbidden {
		t.Errorf("member create: %d", rec.Code)
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/upload-tokens", `{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d", rec.Code)
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/upload-tokens/9/revoke", ""); rec.Code != http.StatusOK || rel.revoked != 9 {
		t.Errorf("revoke: %d", rec.Code)
	}
	if rec := call(t, s, member, "GET", "/v1/projects/1/upload-tokens", ""); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, member, "GET", "/v1/projects/1/releases/v1", ""); rec.Code != http.StatusOK {
		t.Errorf("release: %d", rec.Code)
	}
	if rec := call(t, s, member, "GET", "/v1/projects/1/releases/v9", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing release: %d", rec.Code)
	}
	if rec := call(t, s, member, "GET", "/v1/projects/2/releases", ""); rec.Code != http.StatusNotFound {
		t.Errorf("other project: %d", rec.Code)
	}
}

func register(m *sourcemaps.Module, r chi.Router) http.Handler {
	m.Register(r)
	return r
}

func call(t *testing.T, m *sourcemaps.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
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

func TestReleasesDisabled(t *testing.T) {
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	m := sourcemaps.New(sourcemaps.Deps{})
	if rec := call(t, m, member, "GET", "/v1/projects/1/releases", ""); rec.Code != 404 || !strings.Contains(rec.Body.String(), "releases are not enabled") {
		t.Errorf("disabled: %d %s", rec.Code, rec.Body)
	}
	owner := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "owner"}}}
	m = sourcemaps.New(sourcemaps.Deps{Repository: &fakeReleases{}})
	big := `{"name":"` + strings.Repeat("x", 1<<14) + `"}`
	if rec := call(t, m, owner, "POST", "/v1/projects/1/upload-tokens", big); rec.Code != 400 || strings.TrimSpace(rec.Body.String()) != `{"error":"invalid JSON"}` {
		t.Errorf("oversized: %d %s", rec.Code, rec.Body)
	}
}

package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/api/legacy"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/go-chi/chi/v5"
)

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
	s := &Server{Keys: &fakeKeys{}}
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

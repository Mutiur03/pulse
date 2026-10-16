package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/api/legacy"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/go-chi/chi/v5"
)

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

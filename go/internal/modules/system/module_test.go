package system_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/modules/system"
)

func TestHealth(t *testing.T) {
	r := chi.NewRouter()
	system.New("query-api").Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"service":"query-api","status":"healthy"}` {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

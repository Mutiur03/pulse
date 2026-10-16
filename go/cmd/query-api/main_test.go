package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/config"
)

func TestRunRejectsBadAllowlist(t *testing.T) {
	cfg := config.Load()
	cfg.OutboundAllowlist = "not a cidr/99"
	if err := run(context.Background(), cfg); err == nil {
		t.Fatal("run accepted an invalid allowlist")
	}
}

func TestServeReportsListenErrors(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := serve(context.Background(), l.Addr().String(), http.NotFoundHandler()); err == nil {
		t.Fatal("expected an error for a busy port")
	}
}

func TestRouterRequiresSessionAndSetsHeaders(t *testing.T) {
	r := newRouter(config.Load(), &auth.Store{})
	r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request got %d", rec.Code)
	}
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}

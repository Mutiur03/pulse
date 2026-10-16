package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/keys"
)

type originAuth struct {
	fakeAuth
	allowed []string
}

func (o *originAuth) OriginAllowed(_ context.Context, _, origin string) bool {
	return keys.OriginAllowed(o.allowed, origin)
}

func send(h http.Handler, method, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/v1/events?key="+testKey, strings.NewReader(`{"event":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAllowedOrigins(t *testing.T) {
	p := &fakeProducer{}
	h := NewHandler(p, &originAuth{allowed: []string{"https://shop.example"}})

	rec := send(h, http.MethodPost, "https://shop.example")
	if rec.Code != http.StatusAccepted || rec.Header().Get("Access-Control-Allow-Origin") != "https://shop.example" {
		t.Fatalf("allowed origin: %d %v", rec.Code, rec.Header())
	}
	rec = send(h, http.MethodPost, "https://evil.example")
	if rec.Code != http.StatusForbidden || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign origin: %d %v", rec.Code, rec.Header())
	}
	if len(p.topics) != 1 {
		t.Fatalf("foreign origin produced data: %v", p.topics)
	}
	// Preflight with the key in the URL is answered per origin.
	if rec = send(h, http.MethodOptions, "https://evil.example"); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("preflight for foreign origin allowed: %v", rec.Header())
	}
	// Servers send no Origin and are not restricted.
	if rec = send(h, http.MethodPost, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("no origin: %d", rec.Code)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}

func TestAnyOriginWhenUnset(t *testing.T) {
	h := NewHandler(&fakeProducer{}, &originAuth{})
	rec := send(h, http.MethodPost, "https://anything.example")
	if rec.Code != http.StatusAccepted || rec.Header().Get("Access-Control-Allow-Origin") != "https://anything.example" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
}

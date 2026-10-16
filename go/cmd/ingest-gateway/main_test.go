package main

import (
	"net/http/httptest"
	"testing"
)

func TestStartGRPCDisabled(t *testing.T) {
	t.Setenv("OTLP_GRPC_PORT", "off")
	s, lis, err := startGRPC(nil, nil, nil, make(chan error, 1))
	if s != nil || lis != nil || err != nil {
		t.Fatalf("got %v %v %v", s, lis, err)
	}
}

func TestStartGRPCBadTLS(t *testing.T) {
	t.Setenv("OTLP_GRPC_PORT", "14317")
	t.Setenv("OTLP_GRPC_TLS_CERT", "/nonexistent/cert.pem")
	if _, _, err := startGRPC(nil, nil, nil, make(chan error, 1)); err == nil {
		t.Fatal("expected a TLS error")
	}
}

func TestIngestRoute(t *testing.T) {
	for path, want := range map[string]string{"/v1/events": "/v1/events", "/health": "/health", "/x/123": "other"} {
		if got := ingestRoute(httptest.NewRequest("POST", path, nil)); got != want {
			t.Errorf("%s: %s", path, got)
		}
	}
}

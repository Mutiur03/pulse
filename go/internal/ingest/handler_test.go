package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeProducer struct{ topics []string }

func (f *fakeProducer) Produce(_ context.Context, topic string, _, _ []byte) error {
	f.topics = append(f.topics, topic)
	return nil
}

func do(t *testing.T, h http.Handler, method, path, ct, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	p := &fakeProducer{}
	h := NewHandler(p)
	cases := []struct {
		path, ct, body string
		want           int
	}{
		{"/v1/events", "application/json", `{"event":"x"}`, 202},
		{"/v1/events", "text/plain", `[{"event":"a"},{"event":"b"}]`, 202},
		{"/v1/events", "", `{"properties":{}}`, 400},
		{"/v1/traces", "application/json", sample, 202},
		{"/v1/traces", "application/json", `[{"trace_id":"a","span_id":"b"}]`, 202},
		{"/v1/traces", "application/x-protobuf", "\x0a", 415},
		{"/v1/errors", "", `{"message":"boom","stack":"at x"}`, 202},
		{"/v1/errors", "", `{}`, 400},
	}
	for _, c := range cases {
		if rec := do(t, h, http.MethodPost, c.path, c.ct, c.body); rec.Code != c.want {
			t.Errorf("%s %s: got %d want %d (%s)", c.path, c.body, rec.Code, c.want, rec.Body)
		}
	}
	want := []string{TopicEvents, TopicEvents, TopicEvents, TopicSpans, TopicSpans, TopicErrors}
	if strings.Join(p.topics, ",") != strings.Join(want, ",") {
		t.Errorf("topics %v", p.topics)
	}
}

func TestCORSPreflight(t *testing.T) {
	rec := do(t, NewHandler(&fakeProducer{}), http.MethodOptions, "/v1/events", "", "")
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight %d %v", rec.Code, rec.Header())
	}
}

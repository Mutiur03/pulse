package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeProducer struct {
	topics []string
	vals   []string
}

func (f *fakeProducer) Produce(_ context.Context, topic string, _, v []byte) error {
	f.topics = append(f.topics, topic)
	f.vals = append(f.vals, string(v))
	return nil
}

const testKey = "pk_test_0123456789abcdef"

type fakeAuth struct {
	limited bool
}

func (f *fakeAuth) Authenticate(_ context.Context, key string) (string, error) {
	if key != testKey {
		return "", ErrUnauthorized
	}
	return "42", nil
}

func (f *fakeAuth) Allow(context.Context, string) (bool, time.Duration) {
	return !f.limited, 300 * time.Millisecond
}

func do(t *testing.T, h http.Handler, method, path, ct, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if !strings.Contains(path, "key=") {
		req.Header.Set("X-Pulse-Key", testKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutes(t *testing.T) {
	p := &fakeProducer{}
	h := NewHandler(p, &fakeAuth{})
	cases := []struct {
		path, ct, body string
		want           int
	}{
		{"/v1/events", "application/json", `{"event":"x"}`, 202},
		{"/v1/events", "text/plain", `[{"event":"a"},{"event":"b"}]`, 202},
		{"/v1/events", "", `{"properties":{}}`, 400},
		{"/v1/traces", "application/json", sample, 202},
		{"/v1/traces", "application/json", `[{"trace_id":"a","span_id":"b"}]`, 202},
		{"/v1/traces", "application/x-protobuf", "\xff\xff", 400},
		{"/v1/traces", "application/x-protobuf", "", 200},
		{"/v1/logs", "application/json", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"hi"}}]}]}]}`, 202},
		{"/v1/errors", "", `{"message":"boom","stack":"at x"}`, 202},
		{"/v1/errors", "", `{}`, 400},
	}
	for _, c := range cases {
		if rec := do(t, h, http.MethodPost, c.path, c.ct, c.body); rec.Code != c.want {
			t.Errorf("%s %s: got %d want %d (%s)", c.path, c.body, rec.Code, c.want, rec.Body)
		}
	}
	want := []string{TopicEvents, TopicEvents, TopicEvents, TopicSpans, TopicSpans, TopicErrors, TopicSpans, TopicLogs, TopicErrors}
	if strings.Join(p.topics, ",") != strings.Join(want, ",") {
		t.Errorf("topics %v", p.topics)
	}
}

func TestCORSPreflight(t *testing.T) {
	rec := do(t, NewHandler(&fakeProducer{}, &fakeAuth{}), http.MethodOptions, "/v1/events", "", "")
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight %d %v", rec.Code, rec.Header())
	}
}

func TestKeyAuth(t *testing.T) {
	p := &fakeProducer{}
	auth := &fakeAuth{}
	h := NewHandler(p, auth)
	body := `{"event":"x","project_id":"999"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("missing key: got %d", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/v1/events?key=pk_wrong_0000000000000", "", body); rec.Code != 401 {
		t.Errorf("bad key: got %d", rec.Code)
	}
	// Query parameter (sendBeacon) works, and the payload's project is overridden.
	if rec := do(t, h, http.MethodPost, "/v1/events?key="+testKey, "text/plain", body); rec.Code != 202 {
		t.Fatalf("query key: got %d", rec.Code)
	}
	if len(p.vals) != 1 || !strings.Contains(p.vals[0], `"project_id":"42"`) {
		t.Errorf("project must come from the key: %v", p.vals)
	}
	if rec := do(t, h, http.MethodPost, "/v1/traces", "application/json", sample); rec.Code != 202 {
		t.Fatalf("otlp: %d", rec.Code)
	}
	for _, v := range p.vals[1:] {
		if !strings.Contains(v, `"project_id":"42"`) {
			t.Errorf("OTLP resource pulse.project_id must be ignored: %s", v)
		}
	}
	auth.limited = true
	rec = do(t, h, http.MethodPost, "/v1/events", "", body)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "1" {
		t.Errorf("rate limited: %d %v", rec.Code, rec.Header())
	}
}

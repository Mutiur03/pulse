package monitor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareAndHandler(t *testing.T) {
	Service("test")
	h := Middleware("test", func(*http.Request) string { return "/v1/things" })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/things?id=7", nil))
	IngestRecords.WithLabelValues("raw.events").Add(3)

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{
		`pulse_http_requests_total{code="418",route="/v1/things",service="test"} 1`,
		`pulse_ingest_records_total{topic="raw.events"} 3`,
		`pulse_build_info{service="test"} 1`,
		`go_goroutines`,
		`pulse_http_request_duration_seconds_bucket`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %s", want)
		}
	}
}

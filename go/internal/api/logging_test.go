package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestRedactURLAndHeaders(t *testing.T) {
	u, _ := url.Parse("/v1/x?token=abc&key=pk_1&limit=5&upload_secret=s")
	got := RedactURL(u)
	if strings.Contains(got, "abc") || strings.Contains(got, "pk_1") || strings.Contains(got, "=s&") || !strings.Contains(got, "limit=5") {
		t.Fatal(got)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	h := SecurityHeaders(31536000)(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/invites/lookup?token=secret-token", nil))
	if strings.Contains(buf.String(), "secret-token") || !strings.Contains(buf.String(), "token=REDACTED") {
		t.Fatalf("log: %s", buf.String())
	}
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains"} {
		if rec.Header().Get(k) != v {
			t.Fatalf("%s = %q", k, rec.Header().Get(k))
		}
	}
}

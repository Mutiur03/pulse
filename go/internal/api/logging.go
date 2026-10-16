package api

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// sensitiveParams are query parameters whose values never reach the logs.
var sensitiveParams = []string{"token", "key", "secret", "password", "code", "signature", "sig"}

// RedactURL returns the path and query of u with sensitive values replaced.
func RedactURL(u *url.URL) string {
	if u.RawQuery == "" {
		return u.Path
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return u.Path + "?[unparsable query]"
	}
	for name := range q {
		l := strings.ToLower(name)
		for _, s := range sensitiveParams {
			if strings.Contains(l, s) {
				q[name] = []string{"REDACTED"}
				break
			}
		}
	}
	return u.Path + "?" + q.Encode()
}

// RequestLogger logs one line per request without cookies, headers or
// sensitive query values.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		// Attribute values are escaped by the handler, so a request cannot
		// forge log lines.
		slog.Info("request", "method", r.Method, "path", RedactURL(r.URL), "status", ww.Status(),
			"bytes", ww.BytesWritten(), "duration", time.Since(start).Round(time.Microsecond),
			"request_id", middleware.GetReqID(r.Context()))
	})
}

// SecurityHeaders sets headers for every API response. HSTS is sent when
// hstsMaxAge > 0.
func SecurityHeaders(hstsMaxAge int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cache-Control", "no-store")
			if hstsMaxAge > 0 {
				h.Set("Strict-Transport-Security", "max-age="+strconv.Itoa(hstsMaxAge)+"; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

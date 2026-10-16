package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/auth"
)

func TestQueryLimiter(t *testing.T) {
	l := &QueryLimiter{PerOrg: 1, Wait: 20 * time.Millisecond}
	release := make(chan struct{})
	started := make(chan struct{})
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("block") != "" {
			close(started)
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := func(q string, pr *auth.Principal) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/v1/traces?"+q, nil)
		if pr != nil {
			r = r.WithContext(auth.WithPrincipal(r.Context(), pr))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	a := &auth.Principal{Projects: []auth.Project{{ID: 1, OrgID: 7}, {ID: 2, OrgID: 7}}}
	b := &auth.Principal{Projects: []auth.Project{{ID: 3}}}
	done := make(chan int)
	go func() { done <- req("project_id=1&block=1", a).Code }()
	<-started
	if c := req("project_id=2", a).Code; c != http.StatusTooManyRequests {
		t.Errorf("same org while busy: got %d, want 429", c)
	}
	if c := req("project_id=3", b).Code; c != http.StatusOK {
		t.Errorf("other org: got %d, want 200", c)
	}
	if !limited("/v1/traces/abc") || limited("/v1/auth/me") || limited("/v1/tracesx") {
		t.Error("limited paths")
	}
	if c := req("", nil).Code; c != http.StatusOK {
		t.Errorf("anonymous: got %d", c)
	}
	close(release)
	if c := <-done; c != http.StatusOK {
		t.Errorf("first request: got %d", c)
	}
	if c := req("project_id=2", a).Code; c != http.StatusOK {
		t.Errorf("after release: got %d", c)
	}
}

func TestQueryLimiterTimeout(t *testing.T) {
	l := &QueryLimiter{Timeout: time.Minute}
	var deadline bool
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, deadline = r.Context().Deadline()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/events", nil))
	if !deadline {
		t.Error("analytical requests get a deadline")
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil))
	if deadline {
		t.Error("other requests keep theirs")
	}
}

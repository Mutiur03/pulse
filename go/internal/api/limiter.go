package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mutiur03/pulse/internal/auth"
)

// QueryLimiter caps how many requests of one organization run at the same
// time in this process, so one busy team cannot use every ClickHouse
// connection. A request waits up to Wait for a slot and then gets a 429.
// Projects outside an organization are limited on their own.
//
// Timeout, when set, is the deadline of each analytical request. The
// ClickHouse driver turns the remaining time into max_execution_time, so
// this is what bounds query time.
type QueryLimiter struct {
	PerOrg  int
	Wait    time.Duration
	Timeout time.Duration

	mu    sync.Mutex
	slots map[string]chan struct{}
}

func (l *QueryLimiter) slot(key string) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.slots == nil {
		l.slots = map[string]chan struct{}{}
	}
	c, ok := l.slots[key]
	if !ok {
		c = make(chan struct{}, l.PerOrg)
		l.slots[key] = c
	}
	return c
}

// limitKey is the organization of the requested project (or of the user's
// first project), or the project itself when it has no organization.
func limitKey(r *http.Request) string {
	pr := auth.FromContext(r.Context())
	if pr == nil || len(pr.Projects) == 0 {
		return ""
	}
	p := pr.Projects[0]
	if id, err := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64); err == nil {
		for _, x := range pr.Projects {
			if x.ID == id {
				p = x
			}
		}
	}
	if p.OrgID != 0 {
		return "org:" + strconv.FormatInt(p.OrgID, 10)
	}
	return "project:" + strconv.FormatInt(p.ID, 10)
}

// limitedPrefixes are the paths that run analytical ClickHouse reads.
var limitedPrefixes = []string{
	"/v1/traces", "/v1/events", "/v1/funnels", "/v1/insights", "/v1/logs",
	"/v1/metrics", "/v1/persons", "/v1/replays", "/v1/services", "/v1/issues",
}

func limited(path string) bool {
	for _, p := range limitedPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// Middleware limits the analytical requests it wraps. Requests without a logged-in
// user pass through (the handlers reject them).
func (l *QueryLimiter) Middleware(next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limited(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if l.Timeout > 0 {
			ctx, cancel := context.WithTimeout(r.Context(), l.Timeout)
			defer cancel()
			r = r.WithContext(ctx)
		}
		key := ""
		if l.PerOrg > 0 {
			key = limitKey(r)
		}
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		c := l.slot(key)
		t := time.NewTimer(l.Wait)
		defer t.Stop()
		select {
		case c <- struct{}{}:
		case <-t.C:
			w.Header().Set("Retry-After", "2")
			writeErr(w, http.StatusTooManyRequests, "Too many queries are running for your organization. Try again in a moment.")
			return
		case <-r.Context().Done():
			return
		}
		defer func() { <-c }()
		next.ServeHTTP(w, r)
	})
}

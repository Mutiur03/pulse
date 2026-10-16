package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/governance"
)

type staticScrub struct{ s *governance.Scrubber }

func (f staticScrub) Scrubber(context.Context, string) *governance.Scrubber { return f.s }

func TestRecordsScrubbedBeforeQueueing(t *testing.T) {
	s, _ := governance.Compile(governance.Rules{Emails: true, Cards: true, Keys: []string{"password"}})
	p := &fakeProducer{}
	h := NewHandler(p, &fakeAuth{}, WithScrubbing(staticScrub{s}))
	post := func(path, body string) {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("X-Pulse-Key", testKey)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	post("/v1/events", `{"event":"signup","distinct_id":"ana@shop.example","properties":{"email":"ana@shop.example","password":"x","$anon_distinct_id":"anon"}}`)
	post("/v1/errors", `{"message":"card 4242 4242 4242 4242 declined for ana@shop.example","url":"https://shop.example/?e=ana@shop.example"}`)
	all := strings.Join(p.vals, "\n")
	if strings.Contains(all, "4242") || strings.Contains(all, `"x"`) || strings.Count(all, "ana@shop.example") != 1 {
		t.Fatalf("not scrubbed: %s", all)
	}
	// The distinct id is an identifier and is kept.
	if !strings.Contains(p.vals[0], `"distinct_id":"ana@shop.example"`) || !strings.Contains(p.vals[0], `"$anon_distinct_id":"anon"`) {
		t.Fatalf("ids changed: %s", p.vals[0])
	}
	if !strings.Contains(p.vals[1], "card [card] declined for [email]") {
		t.Fatalf("error: %s", p.vals[1])
	}
}

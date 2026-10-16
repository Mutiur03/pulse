package ingest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventsEnrichedFromRequest(t *testing.T) {
	p := &fakeProducer{}
	h := NewHandler(p, &fakeAuth{}, WithCountryHeader("CF-IPCountry"))
	send := func(body, ua, country string) map[string]interface{} {
		req := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(body))
		req.Header.Set("X-Pulse-Key", testKey)
		req.Header.Set("User-Agent", ua)
		if country != "" {
			req.Header.Set("CF-IPCountry", country)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		var e Event
		_ = json.Unmarshal([]byte(p.vals[len(p.vals)-1]), &e)
		return e.Properties
	}
	ua := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
	got := send(`{"event":"$pageview"}`, ua, "bd")
	for k, want := range map[string]string{"$browser": "Safari", "$browser_version": "17", "$os": "iOS", "$device_type": "Mobile", "$country": "BD"} {
		if got[k] != want {
			t.Errorf("%s = %v, want %s", k, got[k], want)
		}
	}
	// Client-set values win; unknown agents and bogus countries add nothing.
	got = send(`{"event":"x","properties":{"$browser":"MyApp","$country":"DE"}}`, "Go-http-client/1.1", "XX")
	if got["$browser"] != "MyApp" || got["$country"] != "DE" || got["$os"] != nil {
		t.Errorf("%v", got)
	}
	// Without the option the header is ignored.
	p2 := &fakeProducer{}
	h = NewHandler(p2, &fakeAuth{})
	req := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(`{"event":"x"}`))
	req.Header.Set("X-Pulse-Key", testKey)
	req.Header.Set("CF-IPCountry", "US")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(p2.vals[0], "$country") {
		t.Errorf("country header trusted without option: %s", p2.vals[0])
	}
}

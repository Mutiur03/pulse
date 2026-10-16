package governance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestScrubString(t *testing.T) {
	s, err := Compile(Rules{Emails: true, Cards: true, IPs: true, Patterns: []string{`order-\d+`}})
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"mail ana@shop.example now":      "mail [email] now",
		"card 4111 1111 1111 1111 ok":    "card [card] ok",
		"card 4111-1111-1111-1111":       "card [card]",
		"not a card 1234567890123":       "not a card 1234567890123",
		"from 192.168.10.4 and 10.0.0.1": "from [ip] and [ip]",
		"v6 2001:db8::1 at 12:30:45":     "v6 [ip] at 12:30:45",
		"version 1.2.3 is fine":          "version 1.2.3 is fine",
		"see order-1234 for details":     "see [redacted] for details",
		"":                               "",
	} {
		if got := s.String(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	var nilScrubber *Scrubber
	if nilScrubber.String("a@b.co") != "a@b.co" {
		t.Error("nil scrubber changed a value")
	}
}

func TestScrubMap(t *testing.T) {
	s, _ := Compile(DefaultRules())
	m := map[string]interface{}{
		"Password":          "hunter2",
		"x_auth_token":      "abc",
		"note":              "paid with 4242424242424242",
		"email":             "ana@shop.example",
		"nested":            map[string]interface{}{"cvv": 123, "list": []interface{}{"4242 4242 4242 4242"}},
		"$anon_distinct_id": "4242424242424242",
		"count":             3.0,
	}
	s.Map(m)
	if m["Password"] != Redacted || m["x_auth_token"] != Redacted || m["note"] != "paid with [card]" || m["email"] != "ana@shop.example" {
		t.Fatalf("map %v", m)
	}
	n := m["nested"].(map[string]interface{})
	if n["cvv"] != Redacted || n["list"].([]interface{})[0] != CardTag || m["$anon_distinct_id"] != "4242424242424242" || m["count"] != 3.0 {
		t.Fatalf("nested %v", m)
	}
	attrs := map[string]string{"http.request.header.authorization": "Bearer x", "db.statement": "card 4242424242424242"}
	s.StringMap(attrs)
	if attrs["http.request.header.authorization"] != Redacted || attrs["db.statement"] != "card [card]" {
		t.Fatalf("attrs %v", attrs)
	}
	if got := string(s.JSON([]byte(`{"token":"t","text":"4242424242424242"}`))); got != `{"text":"[card]","token":"[redacted]"}` {
		t.Fatalf("json %s", got)
	}
	if got := string(s.JSON([]byte(`plain 4242424242424242`))); got != "plain [card]" {
		t.Fatalf("text %s", got)
	}
}

func TestRulesClean(t *testing.T) {
	r, err := Rules{Keys: []string{" Token ", "token", ""}, Patterns: []string{"", `\d+`}}.Clean()
	if err != nil || len(r.Keys) != 1 || r.Keys[0] != "token" || len(r.Patterns) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := (Rules{Patterns: []string{"("}}).Clean(); err == nil {
		t.Error("bad pattern accepted")
	}
	if _, err := (Rules{Keys: []string{strings.Repeat("k", 101)}}).Clean(); err == nil {
		t.Error("long key accepted")
	}
}

type fakeSettings struct {
	n   int
	err error
	s   Settings
}

func (f *fakeSettings) Settings(_ context.Context, p int64) (Settings, error) {
	f.n++
	f.s.ProjectID = p
	return f.s, f.err
}

func TestCache(t *testing.T) {
	now := time.Unix(1000, 0)
	src := &fakeSettings{s: Settings{Rules: Rules{Emails: true}}}
	c := &Cache{Source: src, Now: func() time.Time { return now }}
	if got := c.Scrubber(context.Background(), "7").String("a@b.co"); got != EmailTag || src.n != 1 {
		t.Fatalf("%q %d", got, src.n)
	}
	c.Scrubber(context.Background(), "7")
	if src.n != 1 {
		t.Fatal("not cached")
	}
	// After expiry a failing load keeps the last rules.
	now = now.Add(CacheTTL + time.Second)
	src.err = errors.New("db down")
	if got := c.Scrubber(context.Background(), "7").String("a@b.co"); got != EmailTag {
		t.Fatalf("kept rules: %q", got)
	}
	// A project never loaded falls back to the defaults (cards on).
	if got := c.Scrubber(context.Background(), "8").String("4242424242424242"); got != CardTag {
		t.Fatalf("defaults: %q", got)
	}
}

package keys

import "testing"

func TestCleanOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://App.Example.com/": "https://app.example.com",
		"http://localhost:5173":    "http://localhost:5173",
		"https://*.example.com":    "https://*.example.com",
	} {
		got, err := CleanOrigin(in)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "example.com", "ftp://x.com", "https://x.com/path", "https://a.*.com", "https://*", "https://x.com?q", "javascript://x"} {
		if _, err := CleanOrigin(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	list := []string{"https://app.example.com", "https://*.example.org", "http://localhost:5173"}
	for origin, want := range map[string]bool{
		"https://app.example.com": true,
		"https://APP.example.com": true,
		"http://app.example.com":  false,
		"https://evil.com":        false,
		"https://a.example.org":   true,
		"https://a.b.example.org": true,
		"https://example.org":     false,
		"https://evilexample.org": false,
		"http://localhost:5173":   true,
		"http://localhost:5174":   false,
		"null":                    false,
	} {
		if got := OriginAllowed(list, origin); got != want {
			t.Fatalf("%s: got %v", origin, got)
		}
	}
	if !OriginAllowed(nil, "https://anything.test") {
		t.Fatal("empty list must allow all")
	}
}

func TestEncodeOrigins(t *testing.T) {
	k := Key{ProjectID: 3, RateLimitPerSec: 9, Origins: []string{"https://a.test", "https://*.b.test"}}
	got, ok := decode(encode(k))
	if !ok || got.ProjectID != 3 || got.RateLimitPerSec != 9 || len(got.Origins) != 2 || got.Origins[1] != "https://*.b.test" {
		t.Fatalf("%+v", got)
	}
	if old, ok := decode("4:500"); !ok || old.ProjectID != 4 || old.Origins != nil {
		t.Fatalf("%+v", old)
	}
}

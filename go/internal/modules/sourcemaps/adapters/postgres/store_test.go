package postgres

import (
	"errors"
	"testing"

	"github.com/Mutiur03/pulse/internal/symbolicate"
)

func TestBestMatch(t *testing.T) {
	cands := [][2]string{
		{"~/static/js/main.js.map", "k-static"},
		{"~/other/main.js.map", "k-other"},
		{"~/js/main.js.map", "k-js"},
	}
	cases := map[string]string{
		"https://shop.example.com/static/js/main.js":    "k-static",
		"https://cdn.example.com/app/v2/js/main.js?x=1": "k-js",
		"https://x/other/main.js":                       "k-other",
	}
	for u, want := range cases {
		got, err := bestMatch(urlPath(u), u, cands)
		if err != nil || got != want {
			t.Errorf("%s: %s %v", u, got, err)
		}
	}
	if _, err := bestMatch("/a/none.js", "https://x/a/none.js", [][2]string{{"~/b/other.js.map", "k"}}); !errors.Is(err, symbolicate.ErrNoMap) {
		t.Errorf("expected no map, got %v", err)
	}
	full := [][2]string{{"https://cdn.example.com/a.js.map", "k-full"}, {"~/a.js.map", "k-tilde"}}
	if got, _ := bestMatch("/a.js", "https://cdn.example.com/a.js", full); got != "k-full" {
		t.Errorf("full URL name: %s", got)
	}
}

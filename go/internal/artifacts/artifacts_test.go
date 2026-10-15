package artifacts

import (
	"errors"
	"testing"

	"github.com/Mutiur03/pulse/internal/symbolicate"
)

func TestArtifactName(t *testing.T) {
	for in, want := range map[string]string{
		"dist/main.js.map":                 "~/dist/main.js.map",
		"~/assets/app.js.map":              "~/assets/app.js.map",
		"/assets//x/../app.js.map":         "~/assets/app.js.map",
		"https://cdn.example.com/a.js.map": "https://cdn.example.com/a.js.map",
		`assets\win.js.map`:                "~/assets/win.js.map",
	} {
		got, err := ArtifactName(in)
		if err != nil || got != want {
			t.Errorf("%s: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "a\nb", "x\x00"} {
		if _, err := ArtifactName(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

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

func TestVersionsAndKeys(t *testing.T) {
	for _, v := range []string{"1.2.3", "web@2026.10.16+abc", "browser-demo@dev"} {
		if !ValidVersion(v) {
			t.Errorf("rejected %s", v)
		}
	}
	for _, v := range []string{"", "a b", "../x", "a/b"} {
		if ValidVersion(v) {
			t.Errorf("accepted %q", v)
		}
	}
	sha := SHA256([]byte("x"))
	if ObjectKey(3, sha) != "sourcemaps/3/2d/"+sha+".map" {
		t.Error(ObjectKey(3, sha))
	}
}

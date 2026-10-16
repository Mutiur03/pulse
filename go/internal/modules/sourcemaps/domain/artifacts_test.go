package domain

import "testing"

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

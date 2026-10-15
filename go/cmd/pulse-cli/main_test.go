package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/sourcemap"
)

func TestInject(t *testing.T) {
	js, _ := os.ReadFile("../../internal/symbolicate/testdata/app.min.js")
	m, _ := os.ReadFile("../../internal/symbolicate/testdata/app.min.js.map")
	src := strings.TrimRight(string(js), "\n") + "\n"
	if !strings.Contains(src, "sourceMappingURL") {
		src += "//# sourceMappingURL=app.min.js.map\n"
	}
	newJS, newMap, id, err := Inject([]byte(src), m)
	if err != nil {
		t.Fatal(err)
	}
	if sourcemap.NormalizeDebugID(id) != id || id != DebugID([]byte(src)) {
		t.Fatalf("id %q", id)
	}
	if got := sourcemap.DebugIDFromSource(newJS); got != id {
		t.Errorf("debugId comment %q", got)
	}
	if !strings.HasSuffix(string(newJS), "//# sourceMappingURL=app.min.js.map\n") {
		t.Error("sourceMappingURL must stay last")
	}
	if !strings.HasPrefix(string(newJS), src[:strings.Index(src, "//# sourceMappingURL")]) {
		t.Error("original code must be untouched so mappings hold")
	}
	pm, err := sourcemap.Parse(newMap)
	if err != nil || pm.DebugID != id {
		t.Fatalf("map debugId %q %v", pm.DebugID, err)
	}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(newMap, &obj)
	if _, ok := obj["sourcesContent"]; !ok {
		t.Error("map fields lost")
	}
	// The mapping still resolves.
	if pos, ok := pm.Lookup(1, 498); !ok || !strings.HasSuffix(pos.Source, "src/app.ts") {
		t.Errorf("lookup %+v", pos)
	}
	again, _, id2, _ := Inject(newJS, newMap)
	if id2 != id || string(again) != string(newJS) {
		t.Error("injecting twice must be a no-op")
	}
}

func TestArtifactName(t *testing.T) {
	for _, c := range [][3]string{
		{"dist/", "main.js.map", "~/dist/main.js.map"},
		{"~/assets", "a/b.js.map", "~/assets/a/b.js.map"},
		{"", "main.js.map", "~/main.js.map"},
		{"~/", "main.js.map", "~/main.js.map"},
		{"https://cdn.example.com/app/", "x.js.map", "https://cdn.example.com/app/x.js.map"},
	} {
		if got := ArtifactName(c[0], c[1]); got != c[2] {
			t.Errorf("%q %q: %s", c[0], c[1], got)
		}
	}
}

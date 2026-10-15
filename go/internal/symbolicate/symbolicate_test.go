package symbolicate

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/grouping"
)

// testdata/app.min.js is src/app.ts bundled with esbuild --minify --sourcemap;
// this is what Node printed when checkout() threw.
const stack = `TypeError: Cannot read properties of undefined (reading 'items')
    at b (https://shop.example.com/static/app.min.js:1:498)
    at c (https://shop.example.com/static/app.min.js:1:545)
    at Object.s (https://shop.example.com/static/app.min.js:1:621)
    at https://shop.example.com/index.html:1:5`

type fakeArtifacts struct {
	byDebug map[string]string
	byURL   map[string]string
	calls   int
}

func (f *fakeArtifacts) MapByDebugID(_ context.Context, _ int64, id string) (string, error) {
	f.calls++
	if k, ok := f.byDebug[id]; ok {
		return k, nil
	}
	return "", ErrNoMap
}

func (f *fakeArtifacts) MapByURL(_ context.Context, _ int64, release, u string) (string, error) {
	f.calls++
	if k, ok := f.byURL[release+" "+u]; ok {
		return k, nil
	}
	return "", ErrNoMap
}

type files struct{ reads int }

func (f *files) Get(_ context.Context, key string) ([]byte, error) {
	f.reads++
	if key != "maps/app" {
		return nil, errors.New("missing")
	}
	return os.ReadFile("testdata/app.min.js.map")
}

func TestParseJS(t *testing.T) {
	fr := ParseJS(stack + "\nfoo@https://x/y.js:3:4\n    at native code")
	if len(fr) != 5 {
		t.Fatalf("%d frames: %+v", len(fr), fr)
	}
	if fr[2] != (RawFrame{Function: "Object.s", URL: "https://shop.example.com/static/app.min.js", Line: 1, Column: 621}) {
		t.Errorf("v8 frame %+v", fr[2])
	}
	if fr[3].Function != "" || fr[3].Line != 1 || fr[4] != (RawFrame{Function: "foo", URL: "https://x/y.js", Line: 3, Column: 4}) {
		t.Errorf("anonymous / gecko %+v %+v", fr[3], fr[4])
	}
}

func TestResolveByURL(t *testing.T) {
	art := &fakeArtifacts{byURL: map[string]string{"v1 https://shop.example.com/static/app.min.js": "maps/app"}}
	obj := &files{}
	s := &Symbolicator{Artifacts: art, Objects: obj}
	res := s.Resolve(context.Background(), 1, "v1", nil, stack)
	if res.Mapped != 3 || len(res.Frames) != 4 {
		t.Fatalf("mapped %d of %d", res.Mapped, len(res.Frames))
	}
	want := []struct {
		fn, file string
		line     int
		ctx      string
	}{
		{"total", "src/app.ts", 7, "  return cart.items.reduce((a, b) => a + b, 0);"},
		{"chargeCard", "src/app.ts", 11, "  const amount = total(cart as Cart);"},
		{"", "src/app.ts", 19, "  return chargeCard(undefined);"},
	}
	for i, w := range want {
		f := res.Frames[i]
		if f.Function != w.fn || f.File != w.file || f.Line != w.line || f.ContextLine != w.ctx || !f.Mapped || !f.InApp {
			t.Errorf("frame %d: %+v", i, f)
		}
	}
	if f := res.Frames[0]; len(f.PreContext) != 5 || f.PreContext[4] != "function total(cart: Cart): number {" || f.PostContext[0] != "}" || f.RawFunction != "b" || f.RawColumn != 498 {
		t.Errorf("context/raw: %+v", f)
	}
	if f := res.Frames[3]; f.Mapped || f.File != "/index.html" || f.RawURL != "https://shop.example.com/index.html" {
		t.Errorf("unmapped frame: %+v", f)
	}
	// The parsed map is cached; the object is read once.
	s.Resolve(context.Background(), 1, "v1", nil, stack)
	if obj.reads != 1 {
		t.Errorf("read %d times", obj.reads)
	}
}

func TestResolveByDebugID(t *testing.T) {
	art := &fakeArtifacts{byDebug: map[string]string{"85314830-023f-4cf1-a267-535f4d3f2d8a": "maps/app"}}
	s := &Symbolicator{Artifacts: art, Objects: &files{}}
	ids := map[string]string{"https://shop.example.com/static/app.min.js": "85314830-023f-4cf1-a267-535f4d3f2d8a"}
	// Debug ids need no release.
	if res := s.Resolve(context.Background(), 1, "", ids, stack); res.Mapped != 3 {
		t.Fatalf("mapped %d", res.Mapped)
	}
	// Misses are remembered briefly instead of hitting storage per frame.
	none := &fakeArtifacts{}
	s2 := &Symbolicator{Artifacts: none, Objects: &files{}}
	s2.Resolve(context.Background(), 1, "v2", nil, stack)
	calls := none.calls
	s2.Resolve(context.Background(), 1, "v2", nil, stack)
	if none.calls != calls {
		t.Errorf("misses not cached: %d then %d", calls, none.calls)
	}
	s2.Forget()
	s2.Resolve(context.Background(), 1, "v2", nil, stack)
	if none.calls == calls {
		t.Error("Forget kept misses")
	}
}

// Two builds of the same source: different minified names, columns and
// bundle hashes must give one fingerprint once symbolicated.
func TestFingerprintStableAcrossBuilds(t *testing.T) {
	art := &fakeArtifacts{byURL: map[string]string{
		"v1 https://shop.example.com/static/app.min.js":      "maps/app",
		"v2 https://shop.example.com/static/app.9f8e7d6c.js": "maps/app",
	}}
	s := &Symbolicator{Artifacts: art, Objects: &files{}}
	build2 := strings.NewReplacer("app.min.js", "app.9f8e7d6c.js", "at b ", "at x ", "at c ", "at q ", "Object.s", "Object.z").Replace(stack)
	r1 := s.Resolve(context.Background(), 1, "v1", nil, stack)
	r2 := s.Resolve(context.Background(), 1, "v2", nil, build2)
	g1 := grouping.GroupFrames("TypeError", "Cannot read properties of undefined (reading 'items')", GroupingFrames(r1.Frames))
	g2 := grouping.GroupFrames("TypeError", "Cannot read properties of undefined (reading 'items')", GroupingFrames(r2.Frames))
	if g1.Fingerprint != g2.Fingerprint {
		t.Fatalf("fingerprints differ: %s vs %s", g1.Fingerprint, g2.Fingerprint)
	}
	if g1.Culprit != "total (src/app.ts)" {
		t.Errorf("culprit %q", g1.Culprit)
	}
	// Unsymbolicated, the renamed minified functions split the issue.
	if grouping.Group("TypeError", "x", stack).Fingerprint == grouping.Group("TypeError", "x", build2).Fingerprint {
		t.Error("expected raw stacks to differ")
	}
	// Without any mapped frame, grouping matches plain stack grouping.
	plain := (&Symbolicator{Artifacts: &fakeArtifacts{}, Objects: &files{}}).Resolve(context.Background(), 1, "v9", nil, stack)
	if got, want := grouping.GroupFrames("TypeError", "m", GroupingFrames(plain.Frames)).Fingerprint, grouping.Group("TypeError", "m", stack).Fingerprint; got != want {
		t.Errorf("unmapped grouping changed: %s vs %s", got, want)
	}
}

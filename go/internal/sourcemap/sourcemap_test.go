package sourcemap

import (
	"strings"
	"testing"
)

func TestVLQRoundTrip(t *testing.T) {
	for _, v := range []int{0, 1, -1, 15, 16, -16, 31, 32, 1000, -1000, 123456, -987654} {
		var b strings.Builder
		EncodeVLQ(&b, v)
		got, next, err := DecodeVLQ(b.String(), 0)
		if err != nil || got != v || next != b.Len() {
			t.Errorf("%d: got %d next %d err %v (enc %q)", v, got, next, err, b.String())
		}
	}
	// Known encodings from the spec examples.
	for enc, want := range map[string]int{"A": 0, "C": 1, "D": -1, "gB": 16, "2H": 123} {
		if got, _, _ := DecodeVLQ(enc, 0); got != want {
			t.Errorf("%s: got %d want %d", enc, got, want)
		}
	}
	for _, bad := range []string{"", "g", "!", "gggggggggA"} {
		if _, _, err := DecodeVLQ(bad, 0); err == nil {
			t.Errorf("%q decoded", bad)
		}
	}
}

// build makes a map from segments of [genLine, genCol, src, srcLine, srcCol, name?] (0-based).
func build(t *testing.T, segs [][]int) string {
	t.Helper()
	var b strings.Builder
	prev := [5]int{}
	line := 0
	first := true
	for _, s := range segs {
		for line < s[0] {
			b.WriteByte(';')
			line++
			prev[0] = 0
			first = true
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		for k := 1; k < len(s); k++ {
			EncodeVLQ(&b, s[k]-prev[k-1])
			prev[k-1] = s[k]
		}
	}
	return `{"version":3,"sources":["webpack://shop/./src/a.ts","b.ts"],"sourceRoot":"","names":["pay","total"],` +
		`"sourcesContent":["l1\nl2\nl3 pay()\nl4\nl5",null],"debugId":"85314830023F4CF1A267535F4D3F2D8A","mappings":"` + b.String() + `"}`
}

func TestLookup(t *testing.T) {
	src := build(t, [][]int{
		{0, 0, 0, 0, 0},
		{0, 10, 0, 2, 3, 0},
		{0, 20, 1, 4, 0, 1},
		{2, 5, 0, 1, 1},
	})
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if m.DebugID != "85314830-023f-4cf1-a267-535f4d3f2d8a" {
		t.Errorf("debug id %q", m.DebugID)
	}
	cases := []struct {
		line, col int
		want      Position
		ok        bool
	}{
		{1, 1, Position{Source: "webpack://shop/./src/a.ts", Line: 1, Column: 1}, true},
		{1, 11, Position{Source: "webpack://shop/./src/a.ts", Line: 3, Column: 4, Name: "pay"}, true},
		{1, 15, Position{Source: "webpack://shop/./src/a.ts", Line: 3, Column: 4, Name: "pay"}, true}, // between segments
		{1, 21, Position{Source: "b.ts", Line: 5, Column: 1, Name: "total", SourceIndex: 1}, true},
		{2, 1, Position{}, false}, // empty line
		{3, 3, Position{}, false}, // before the first segment
		{3, 6, Position{Source: "webpack://shop/./src/a.ts", Line: 2, Column: 2}, true},
		{9, 1, Position{}, false},
	}
	for _, c := range cases {
		got, ok := m.Lookup(c.line, c.col)
		if ok != c.ok || got != c.want {
			t.Errorf("%d:%d got %+v %v want %+v", c.line, c.col, got, ok, c.want)
		}
	}
	pre, line, post, ok := m.Context(0, 3, 1)
	if !ok || line != "l3 pay()" || len(pre) != 1 || pre[0] != "l2" || len(post) != 1 || post[0] != "l4" {
		t.Errorf("context %v %q %v %v", pre, line, post, ok)
	}
	if _, _, _, ok := m.Context(1, 1, 1); ok {
		t.Error("source without content gave context")
	}
}

func TestParseRejects(t *testing.T) {
	for _, bad := range []string{`nope`, `{"version":2,"mappings":""}`, `{"version":3,"sections":[{}]}`,
		`{"version":3,"sources":["a"],"mappings":"AAAA,ZZZZZZ"}`, `{"version":3,"sources":["a"],"mappings":"ACAA"}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestDebugIDs(t *testing.T) {
	js := []byte("var a=1;\n//# sourceMappingURL=a.js.map\n//# debugId=85314830-023F-4CF1-A267-535F4D3F2D8A\n")
	if got := DebugIDFromSource(js); got != "85314830-023f-4cf1-a267-535f4d3f2d8a" {
		t.Errorf("got %q", got)
	}
	if DebugIDFromSource([]byte("var debugId=1")) != "" || NormalizeDebugID("not-a-uuid") != "" {
		t.Error("accepted junk")
	}
}

func TestCleanSource(t *testing.T) {
	for in, want := range map[string]string{
		"webpack://shop/./src/a.ts": "src/a.ts",
		"../../src/b.ts":            "src/b.ts",
		"/abs/src/c.ts":             "abs/src/c.ts",
		"src/d.ts":                  "src/d.ts",
	} {
		if got := CleanSource(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

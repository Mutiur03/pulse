package processor

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues"
	"github.com/Mutiur03/pulse/internal/symbolicate"
)

type oneMap struct{}

func (oneMap) MapByDebugID(context.Context, int64, string) (string, error) {
	return "", symbolicate.ErrNoMap
}

func (oneMap) MapByURL(_ context.Context, _ int64, release, _ string) (string, error) {
	if release == "v1" || release == "v2" {
		return "k", nil
	}
	return "", symbolicate.ErrNoMap
}

type mapFile struct{}

func (mapFile) Get(context.Context, string) ([]byte, error) {
	return os.ReadFile("../symbolicate/testdata/app.min.js.map")
}

const minified = `TypeError: Cannot read properties of undefined (reading 'items')
    at b (https://shop.example.com/static/app.min.js:1:498)
    at c (https://shop.example.com/static/app.min.js:1:545)
    at Object.s (https://shop.example.com/static/app.min.js:1:621)`

func jsErr(release, stack string) map[string]interface{} {
	return map[string]interface{}{
		"project_id": "1", "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"exception_type": "TypeError", "message": "Cannot read properties of undefined (reading 'items')",
		"stack": stack, "release": release, "platform": "browser",
		"debug_ids": map[string]interface{}{"https://shop.example.com/static/app.min.js": "not-a-uuid"},
	}
}

func TestGroupErrorsSymbolicates(t *testing.T) {
	ctx := context.Background()
	sym := &symbolicate.Symbolicator{Artifacts: oneMap{}, Objects: mapFile{}}
	// The second build renamed the file and its minified identifiers.
	build2 := strings.NewReplacer("app.min.js", "app.9f8e7d6c.js", "at b ", "at x ", "at c ", "at q ").Replace(minified)
	rows, err := GroupErrorsWith(ctx, []map[string]interface{}{jsErr("v1", minified), jsErr("v2", build2), jsErr("none", minified)}, &memIssues{byFP: map[string]*issues.Issue{}}, &capture{}, sym)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Fingerprint != rows[1].Fingerprint {
		t.Errorf("builds grouped apart: %s vs %s", rows[0].Fingerprint, rows[1].Fingerprint)
	}
	if rows[2].Fingerprint == rows[0].Fingerprint {
		t.Error("unmapped stack should keep minified grouping")
	}
	if rows[0].MappedFrames != 3 || rows[2].MappedFrames != 0 {
		t.Errorf("mapped %d / %d", rows[0].MappedFrames, rows[2].MappedFrames)
	}
	var frames []symbolicate.Frame
	if err := json.Unmarshal([]byte(rows[0].Frames), &frames); err != nil || len(frames) != 3 {
		t.Fatalf("frames %v %s", err, rows[0].Frames)
	}
	if f := frames[0]; f.File != "src/app.ts" || f.RawURL == "" || f.ContextLine == "" {
		t.Errorf("frame %+v", f)
	}
	if rows[0].DebugIDs != "" {
		t.Errorf("invalid debug id kept: %s", rows[0].DebugIDs)
	}
}

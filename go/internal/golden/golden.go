// Package golden compares values against JSON snapshots in testdata. Run
// tests with UPDATE_GOLDEN=1 to rewrite the snapshots from current output.
package golden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// JSON marshals v (indented) and compares it with testdata/<name>.golden.json.
func JSON(t testing.TB, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name+".golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // test snapshot path
	if err != nil {
		t.Fatalf("read %s (run with UPDATE_GOLDEN=1 to create): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s differs from snapshot\n--- got ---\n%s", path, got)
	}
}

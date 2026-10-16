package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNew(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "query-api", "warn")
	l.Info("hidden")
	l.Warn("shown", "n", 1)
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("%q: %v", buf.String(), err)
	}
	if m["msg"] != "shown" || m["service"] != "query-api" || m["level"] != "WARN" || m["n"] != 1.0 {
		t.Fatalf("%v", m)
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"debug": slog.LevelDebug, "WARN": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, "": slog.LevelInfo, "bogus": slog.LevelInfo} {
		if got := ParseLevel(in); got != want {
			t.Errorf("%q = %v", in, got)
		}
	}
}

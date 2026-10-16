package ch

import (
	"strings"
	"testing"
)

func TestReplaysSQL(t *testing.T) {
	q, args := ReplaysSQL(ReplayFilter{Project: 1, HasErrors: true, DistinctID: "u", URL: "x", MinDurationMs: 10, Limit: 5})
	for _, want := range []string{"HAVING error_total > 0 AND has(users, ?) AND arrayExists", "duration_ms >= ?", "GROUP BY session_id", "LIMIT ?"} {
		if !strings.Contains(q, want) {
			t.Errorf("missing %q in %s", want, q)
		}
	}
	if strings.Count(q, "?") != len(args) {
		t.Errorf("placeholders %d args %d", strings.Count(q, "?"), len(args))
	}
}

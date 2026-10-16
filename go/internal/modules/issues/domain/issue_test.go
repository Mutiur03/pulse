package domain

import (
	"testing"
	"time"
)

func TestTransition(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	before, after := t0.Add(-time.Minute), t0.Add(time.Minute)
	cases := []struct {
		name       string
		exists     bool
		cur        Status
		resolvedAt *time.Time
		at         time.Time
		want       Status
		kind       Kind
	}{
		{"first occurrence", false, "", nil, after, Unresolved, KindNew},
		{"unresolved stays", true, Unresolved, nil, after, Unresolved, KindNone},
		{"ignored stays ignored", true, Ignored, nil, after, Ignored, KindNone},
		{"resolved reopens on new event", true, Resolved, &t0, after, Unresolved, KindRegression},
		{"late event from before the fix does not reopen", true, Resolved, &t0, before, Resolved, KindNone},
		{"resolved without timestamp reopens", true, Resolved, nil, before, Unresolved, KindRegression},
	}
	for _, c := range cases {
		got, kind := Transition(c.exists, c.cur, c.resolvedAt, c.at)
		if got != c.want || kind != c.kind {
			t.Errorf("%s: got %s/%q want %s/%q", c.name, got, kind, c.want, c.kind)
		}
	}
}

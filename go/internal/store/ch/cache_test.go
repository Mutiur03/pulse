package ch

import (
	"testing"
	"time"
)

func TestCacheRange(t *testing.T) {
	from := time.Date(2026, 1, 2, 3, 4, 59, 900, time.UTC)
	to := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
	f, tt := cacheRange(from, to)
	if !f.Equal(time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)) || !tt.Equal(time.Date(2026, 1, 3, 3, 5, 0, 0, time.UTC)) {
		t.Errorf("got %v %v", f, tt)
	}
	exact := time.Date(2026, 1, 3, 3, 4, 0, 0, time.UTC)
	if _, tt := cacheRange(from, exact); !tt.Equal(exact) {
		t.Errorf("an aligned end stays: %v", tt)
	}
}

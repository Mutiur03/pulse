package domain

import (
	"testing"
	"time"
)

func TestRange(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	from, to := Range(now, nil, nil, TrendWindow)
	if !to.Equal(now) || !from.Equal(now.Add(-TrendWindow)) {
		t.Errorf("%v %v", from, to)
	}
	end := now.Add(-time.Hour)
	if from, _ := Range(now, nil, &end, time.Hour); !from.Equal(end.Add(-time.Hour)) {
		t.Errorf("from %v", from)
	}
	if RetentionWindow("week") != 56*24*time.Hour || RetentionWindow("day") != 14*24*time.Hour {
		t.Error("retention window")
	}
}

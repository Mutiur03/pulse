package domain

import (
	"testing"
	"time"
)

func TestLogInterval(t *testing.T) {
	for span, want := range map[time.Duration]time.Duration{
		15 * time.Minute: 10 * time.Second, time.Hour: 30 * time.Second, 24 * time.Hour: 15 * time.Minute,
		3 * 24 * time.Hour: time.Hour, 60 * 24 * time.Hour: 12 * time.Hour,
	} {
		if got := LogInterval(span); got != want {
			t.Errorf("%v: %v, want %v", span, got, want)
		}
	}
}

func TestMetricStep(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ten := 10
	one := 1
	if got := MetricStep(from, from.Add(time.Hour), &ten); got != 10*time.Second {
		t.Errorf("requested step: %v", got)
	}
	if got := MetricStep(from, from.Add(24*time.Hour), &one); got != 24*time.Minute {
		t.Errorf("too fine a step falls back: %v", got)
	}
	if got := MetricStep(from, from.Add(time.Minute), nil); got != 10*time.Second {
		t.Errorf("minimum step: %v", got)
	}
}

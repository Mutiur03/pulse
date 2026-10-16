// Package domain holds the telemetry query rules: bucket sizes and the
// errors the module reports.
package domain

import (
	"errors"
	"time"
)

// Errors reported by the telemetry service.
var (
	ErrTraceNotFound = errors.New("trace not found")
	ErrLogsOff       = errors.New("logs are not configured")
	ErrStatsOff      = errors.New("insights are not configured")
	ErrBadRange      = errors.New("from must be before to")
)

// MaxMetricPoints bounds the buckets per series.
const MaxMetricPoints = 1500

// LogInterval picks a bucket size giving at most about 120 bars.
func LogInterval(span time.Duration) time.Duration {
	for _, d := range []time.Duration{
		10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute,
		30 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	} {
		if span/d <= 120 {
			return d
		}
	}
	return 24 * time.Hour
}

// MetricStep picks the bucket size: the requested one if it keeps the
// series under MaxMetricPoints, else about 60 buckets over the range.
func MetricStep(from, to time.Time, want *int) time.Duration {
	span := to.Sub(from)
	if want != nil && *want > 0 {
		step := time.Duration(*want) * time.Second
		if span/step <= MaxMetricPoints {
			return step
		}
	}
	step := (span / 60).Truncate(time.Second)
	if step < 10*time.Second {
		step = 10 * time.Second
	}
	return step
}

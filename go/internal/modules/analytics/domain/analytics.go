// Package domain holds the product analytics rules.
package domain

import (
	"errors"
	"time"
)

// Errors the service reports.
var (
	ErrAnalyticsOff   = errors.New("analytics are not configured")
	ErrInsightsOff    = errors.New("insights are not configured")
	ErrPersonNotFound = errors.New("person not found")
)

// Default query windows.
const (
	TrendWindow  = 7 * 24 * time.Hour
	FunnelWindow = 7 * 24 * time.Hour
	PathsWindow  = 7 * 24 * time.Hour
	// PersonWindow is the event retention.
	PersonWindow = 30 * 24 * time.Hour
	// FunnelSeconds is the default conversion window.
	FunnelSeconds = 3600
	// PersonSessions and PersonEvents cap the person detail lists.
	PersonSessions = 50
	PersonEvents   = 100
)

// RetentionWindow is as many periods back as the matrix is wide, plus one.
func RetentionWindow(period string) time.Duration {
	if period == "week" {
		return 8 * 7 * 24 * time.Hour
	}
	return 14 * 24 * time.Hour
}

// Range returns [to-window, to], with to defaulting to now and from
// overriding the start.
func Range(now time.Time, from, to *time.Time, window time.Duration) (time.Time, time.Time) {
	end := now
	if to != nil {
		end = *to
	}
	start := end.Add(-window)
	if from != nil {
		start = *from
	}
	return start, end
}

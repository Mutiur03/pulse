// Package domain holds the alert API rules. Rule evaluation itself lives in
// internal/alerts, shared with the alerting worker.
package domain

import (
	"errors"
	"time"
)

// Errors the service reports.
var (
	ErrAlertsOff = errors.New("alerting is not configured")
	ErrSenderOff = errors.New("notifications are not configured")
	ErrBadMute   = errors.New("minutes must be between 0 and 43200")
)

// PreviewPoints is how many buckets the preview chart shows.
const PreviewPoints = 24

// MaxMuteMinutes is the longest mute, 30 days.
const MaxMuteMinutes = 30 * 24 * 60

// HistoryLimit caps the rule history in the detail view.
const HistoryLimit = 200

// PreviewBucket is the chart bucket for a rule window: the window, capped so
// the chart spans at most a week.
func PreviewBucket(window time.Duration) time.Duration {
	if max := 7 * 24 * time.Hour / PreviewPoints; window > max {
		return max
	}
	return window
}

// MuteUntil returns when a mute of minutes from now ends; zero unmutes.
func MuteUntil(now time.Time, minutes int) (*time.Time, error) {
	if minutes < 0 || minutes > MaxMuteMinutes {
		return nil, ErrBadMute
	}
	if minutes == 0 {
		return nil, nil
	}
	t := now.Add(time.Duration(minutes) * time.Minute).UTC()
	return &t, nil
}

package domain

import (
	"testing"
	"time"
)

func TestPreviewBucketAndMute(t *testing.T) {
	if PreviewBucket(time.Minute) != time.Minute || PreviewBucket(24*time.Hour) != 7*time.Hour {
		t.Error("bucket")
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if u, err := MuteUntil(now, 0); u != nil || err != nil {
		t.Error("unmute")
	}
	if u, _ := MuteUntil(now, 90); !u.Equal(now.Add(90 * time.Minute)) {
		t.Error("mute")
	}
	if _, err := MuteUntil(now, MaxMuteMinutes+1); err != ErrBadMute {
		t.Error("too long")
	}
}

// Package domain holds the replay rules that do not depend on storage.
package domain

import (
	"errors"
	"regexp"
)

// DefaultRetentionDays matches the TTL of the replay_chunks table.
const DefaultRetentionDays = 30

// ErrNotFound means the session has no recording the user can see.
var ErrNotFound = errors.New("replay not found")

// ErrStorageOff means chunk storage is not configured.
var ErrStorageOff = errors.New("replay storage is not configured")

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidSessionID reports whether id can name a recorded session.
func ValidSessionID(id string) bool { return sessionIDRe.MatchString(id) }

// RetentionDays returns days, or the default when it is not positive.
func RetentionDays(days int) int {
	if days > 0 {
		return days
	}
	return DefaultRetentionDays
}

// Package domain holds the data governance API rules. Scrubbing and
// retention themselves live in internal/governance, shared with the ingest
// gateway and the governance worker.
package domain

import (
	"errors"
	"strings"
)

// Errors the service reports.
var (
	ErrOff          = errors.New("data governance is not configured")
	ErrSampleLong   = errors.New("sample is longer than 10000 characters")
	ErrBadDistinct  = errors.New("distinct_id must be 1 to 200 characters")
	ErrReasonLong   = errors.New("reason is longer than 500 characters")
	ErrNoSuchDelete = errors.New("deletion request not found")
)

// Limits.
const (
	MaxSample   = 10000
	MaxDistinct = 200
	MaxReason   = 500
	// RequestList caps the deletion request list.
	RequestList = 200
)

// DeletionInput validates and trims a deletion request.
func DeletionInput(distinctID string, reason *string) (string, string, error) {
	id := strings.TrimSpace(distinctID)
	r := ""
	if reason != nil {
		r = strings.TrimSpace(*reason)
	}
	if id == "" || len(id) > MaxDistinct {
		return "", "", ErrBadDistinct
	}
	if len(r) > MaxReason {
		return "", "", ErrReasonLong
	}
	return id, r, nil
}

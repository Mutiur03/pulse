// Package issues groups errors into issues and tracks their lifecycle.
package issues

import "time"

type Status string

const (
	Unresolved Status = "unresolved"
	Resolved   Status = "resolved"
	Ignored    Status = "ignored"
)

func (s Status) Valid() bool { return s == Unresolved || s == Resolved || s == Ignored }

// Kind says what a new occurrence did to its issue.
type Kind string

const (
	KindNone       Kind = ""
	KindNew        Kind = "new_issue"
	KindRegression Kind = "regression"
)

// Transition decides the issue status after a new occurrence.
//
//   - no issue yet: a new unresolved issue
//   - resolved: the bug came back, so it reopens as a regression, but only
//     if the event happened after it was resolved (late or replayed events
//     from before the fix do not reopen it)
//   - ignored: stays ignored, still counted
//   - unresolved: unchanged
func Transition(exists bool, cur Status, resolvedAt *time.Time, eventAt time.Time) (Status, Kind) {
	if !exists {
		return Unresolved, KindNew
	}
	if cur == Resolved {
		if resolvedAt != nil && !eventAt.After(*resolvedAt) {
			return Resolved, KindNone
		}
		return Unresolved, KindRegression
	}
	return cur, KindNone
}

// Issue is a group of errors sharing a fingerprint.
type Issue struct {
	ID             int64      `json:"id"`
	ProjectID      int64      `json:"project_id"`
	Fingerprint    string     `json:"fingerprint"`
	Title          string     `json:"title"`
	ExceptionType  string     `json:"exception_type"`
	Culprit        string     `json:"culprit"`
	Platform       string     `json:"platform"`
	ServiceName    string     `json:"service_name"`
	Status         Status     `json:"status"`
	FirstSeen      time.Time  `json:"first_seen"`
	LastSeen       time.Time  `json:"last_seen"`
	Count          int64      `json:"count"`
	SampleTraceID  string     `json:"sample_trace_id"`
	SampleSpanID   string     `json:"sample_span_id"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	TimesRegressed int        `json:"times_regressed"`
}

// Occurrence is one error being recorded against an issue.
type Occurrence struct {
	ProjectID     int64
	Fingerprint   string
	Title         string
	ExceptionType string
	Culprit       string
	Platform      string
	ServiceName   string
	TraceID       string
	SpanID        string
	Timestamp     time.Time
}

// Event is published to the issue.events topic for new issues and regressions.
type Event struct {
	Type        Kind      `json:"type"`
	IssueID     int64     `json:"issue_id"`
	ProjectID   int64     `json:"project_id"`
	Fingerprint string    `json:"fingerprint"`
	Title       string    `json:"title"`
	ServiceName string    `json:"service_name,omitempty"`
	TraceID     string    `json:"trace_id,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

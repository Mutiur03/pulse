// Package alerts defines alert rules, how they are evaluated and how their
// notifications are delivered. The query API manages rules and channels and
// the alerting worker evaluates them.
package alerts

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/logquery"
)

// Metric is what a rule measures.
type Metric string

const (
	ErrorCount      Metric = "error_count"      // errors in the window
	ErrorRate       Metric = "error_rate"       // errors per minute
	FailureRate     Metric = "failure_rate"     // percent of requests that failed
	LatencyP95      Metric = "latency_p95"      // milliseconds, server spans
	LatencyP99      Metric = "latency_p99"      // milliseconds, server spans
	Throughput      Metric = "throughput"       // requests per minute
	EventCount      Metric = "event_count"      // product events named by the rule
	LogCount        Metric = "log_count"        // logs matching the rule's log query
	NewIssue        Metric = "new_issue"        // fires on each new issue
	IssueRegression Metric = "issue_regression" // fires on each regression
)

// Metrics lists every metric in the order the UI shows them.
var Metrics = []Metric{ErrorCount, ErrorRate, FailureRate, LatencyP95, LatencyP99, Throughput, EventCount, LogCount, NewIssue, IssueRegression}

// EventDriven reports whether the metric fires from issue events rather than
// from a scheduled query.
func (m Metric) EventDriven() bool { return m == NewIssue || m == IssueRegression }

// Unit is shown next to values.
func (m Metric) Unit() string {
	switch m {
	case ErrorRate, Throughput:
		return "/min"
	case FailureRate:
		return "%"
	case LatencyP95, LatencyP99:
		return "ms"
	}
	return ""
}

func (m Metric) Valid() bool {
	for _, v := range Metrics {
		if v == m {
			return true
		}
	}
	return false
}

// Comparison decides when a value breaches the threshold.
type Comparison string

const (
	Above        Comparison = "gt"
	AboveOrEqual Comparison = "gte"
	Below        Comparison = "lt"
	BelowOrEqual Comparison = "lte"
)

func (c Comparison) Symbol() string {
	switch c {
	case Above:
		return ">"
	case AboveOrEqual:
		return "≥"
	case Below:
		return "<"
	case BelowOrEqual:
		return "≤"
	}
	return string(c)
}

// Breached reports whether value breaches the threshold.
func (c Comparison) Breached(value, threshold float64) bool {
	switch c {
	case Above:
		return value > threshold
	case AboveOrEqual:
		return value >= threshold
	case Below:
		return value < threshold
	case BelowOrEqual:
		return value <= threshold
	}
	return false
}

// State is a rule's current condition.
type State string

const (
	OK       State = "ok"
	Alerting State = "alerting"
	NoData   State = "no_data"
)

// Window and interval limits.
const (
	MinWindow   = time.Minute
	MaxWindow   = 24 * time.Hour
	MinInterval = 10 * time.Second
	MaxInterval = time.Hour
)

// Rule is an alert rule.
type Rule struct {
	ID         int64
	ProjectID  int64
	Name       string
	Metric     Metric
	Comparison Comparison
	Threshold  float64
	Window     time.Duration
	Interval   time.Duration
	// Filters; empty means any.
	Service     string
	Route       string
	Environment string
	EventName   string
	// LogQuery is the logs search (see package logquery) for log_count.
	LogQuery string
	// Renotify repeats the alert while it keeps firing, and also stops a rule
	// that flaps from notifying again within that time. Zero disables both.
	Renotify   time.Duration
	ChannelIDs []int64
	Enabled    bool
	MutedUntil *time.Time

	State           State
	StateSince      time.Time
	LastValue       *float64
	LastEvaluatedAt *time.Time
	LastNotifiedAt  *time.Time
	LastTriggeredAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Muted reports whether notifications are paused at now.
func (r *Rule) Muted(now time.Time) bool { return r.MutedUntil != nil && now.Before(*r.MutedUntil) }

// Condition describes the rule in words, e.g. "error_rate > 5/min over 5m".
func (r *Rule) Condition() string {
	if r.Metric.EventDriven() {
		return strings.ReplaceAll(string(r.Metric), "_", " ")
	}
	return fmt.Sprintf("%s %s %s%s over %s", r.Metric, r.Comparison.Symbol(), formatValue(r.Threshold), r.Metric.Unit(), shortDuration(r.Window))
}

// Validate checks a rule before it is saved and fills defaults.
func (r *Rule) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	switch {
	case r.Name == "" || len(r.Name) > 200:
		return errors.New("name must be 1 to 200 characters")
	case !r.Metric.Valid():
		return fmt.Errorf("unknown metric %q", r.Metric)
	case r.Metric == EventCount && strings.TrimSpace(r.EventName) == "":
		return errors.New("event_count needs an event name")
	}
	r.LogQuery = strings.TrimSpace(r.LogQuery)
	if r.Metric == LogCount {
		q, err := logquery.Parse(r.LogQuery)
		if err != nil {
			return fmt.Errorf("log query: %w", err)
		}
		r.LogQuery = q.String()
	} else {
		r.LogQuery = ""
	}
	if r.Comparison == "" {
		r.Comparison = Above
	}
	if !r.Comparison.Breached(1, 0) && !r.Comparison.Breached(0, 1) {
		return fmt.Errorf("unknown comparison %q", r.Comparison)
	}
	if r.Window == 0 {
		r.Window = 5 * time.Minute
	}
	if r.Interval == 0 {
		r.Interval = time.Minute
	}
	switch {
	case r.Window < MinWindow || r.Window > MaxWindow:
		return errors.New("window must be between 1m and 24h")
	case r.Interval < MinInterval || r.Interval > MaxInterval:
		return errors.New("interval must be between 10s and 1h")
	case r.Renotify < 0 || r.Renotify > 7*24*time.Hour:
		return errors.New("renotify must be between 0 and 7 days")
	case r.Threshold < 0:
		return errors.New("threshold must not be negative")
	}
	for _, f := range []string{r.Service, r.Route, r.Environment, r.EventName} {
		if len(f) > 200 {
			return errors.New("filters must be at most 200 characters")
		}
	}
	return nil
}

func formatValue(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

// FormatValue renders a metric value with its unit.
func FormatValue(m Metric, v float64) string { return formatValue(v) + m.Unit() }

func shortDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

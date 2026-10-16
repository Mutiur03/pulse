package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues"
	"github.com/Mutiur03/pulse/internal/monitor"
)

// RuleStore is the storage the engine needs.
type RuleStore interface {
	Rules(ctx context.Context) ([]Rule, error)
	EventRules(ctx context.Context, project int64, m Metric) ([]Rule, error)
	Channels(ctx context.Context, project int64, ids []int64) ([]Channel, error)
	Record(ctx context.Context, r Rule, out Outcome, value *float64, e Entry, notified, triggered bool, now time.Time) error
}

// MetricReader runs a metric query and returns its single row.
type MetricReader interface {
	AlertSample(ctx context.Context, q Query) (v float64, n uint64, trace string, issue uint64, err error)
}

// Sender delivers a payload to a channel.
type Sender interface {
	Send(ctx context.Context, c Channel, p Payload) error
}

// Locker takes a lease so one worker replica handles each evaluation or
// issue event. It returns false when another holder has the key.
type Locker interface {
	Lock(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// Engine evaluates rules and sends notifications.
type Engine struct {
	Store   RuleStore
	Metrics MetricReader
	Sender  Sender
	Locks   Locker
	AppURL  string
	Now     func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) lock(ctx context.Context, key string, ttl time.Duration) bool {
	if e.Locks == nil {
		return true
	}
	ok, err := e.Locks.Lock(ctx, key, ttl)
	if err != nil {
		slog.Error("alerts: lock failed", "key", key, "err", err)
		return false
	}
	return ok
}

// Tick evaluates every rule that is due. It returns how many it evaluated.
func (e *Engine) Tick(ctx context.Context) (int, error) {
	rules, err := e.Store.Rules(ctx)
	if err != nil {
		return 0, err
	}
	now := e.now()
	n := 0
	for i := range rules {
		r := &rules[i]
		if !Due(r, now) {
			continue
		}
		// The lease lasts slightly less than the interval so the next
		// evaluation is never blocked by this one's key.
		ttl := r.Interval - time.Second
		if r.Metric.EventDriven() {
			ttl = 5 * time.Second
		}
		if !e.lock(ctx, fmt.Sprintf("alert:eval:%d", r.ID), ttl) {
			continue
		}
		if err := e.Evaluate(ctx, r, now); err != nil {
			slog.Error("alerts: rule failed", "rule_id", r.ID, "err", err)
			continue
		}
		n++
	}
	return n, nil
}

// Evaluate runs one rule at now and records the result.
func (e *Engine) Evaluate(ctx context.Context, r *Rule, now time.Time) error {
	err := e.evaluate(ctx, r, now)
	result := "ok"
	if err != nil {
		result = "error"
	}
	monitor.AlertEvaluations.WithLabelValues(result).Inc()
	return err
}

func (e *Engine) evaluate(ctx context.Context, r *Rule, now time.Time) error {
	if r.Metric.EventDriven() {
		out, ok := Settle(r, now)
		if !ok {
			return nil
		}
		return e.apply(ctx, r, out, nil, Entry{Message: "no new events for " + shortDuration(r.Window)}, false, now)
	}
	q, err := MetricQuery(r, now)
	if err != nil {
		return err
	}
	v, n, trace, issue, err := e.Metrics.AlertSample(ctx, q)
	if err != nil {
		return err
	}
	s := SampleFrom(q, v, n, trace, issue)
	out := Decide(r, s, now)
	var value *float64
	if s.HasData {
		value = &s.Value
	}
	entry := Entry{TraceID: s.TraceID}
	if s.IssueID > 0 {
		entry.IssueID = &s.IssueID
	}
	return e.apply(ctx, r, out, value, entry, false, now)
}

// MaxEventAge is how old an issue event may be and still fire rules.
const MaxEventAge = 15 * time.Minute

// HandleIssueEvent fires the project's new-issue or regression rules.
func (e *Engine) HandleIssueEvent(ctx context.Context, ev issues.Event) error {
	var m Metric
	switch ev.Type {
	case issues.KindNew:
		m = NewIssue
	case issues.KindRegression:
		m = IssueRegression
	default:
		return nil
	}
	now := e.now()
	// A backlog replayed after downtime should not page about old issues.
	if !ev.Timestamp.IsZero() && now.Sub(ev.Timestamp) > MaxEventAge {
		return nil
	}
	rules, err := e.Store.EventRules(ctx, ev.ProjectID, m)
	if err != nil {
		return err
	}
	for i := range rules {
		r := &rules[i]
		if r.Service != "" && r.Service != ev.ServiceName {
			continue
		}
		// Kafka may deliver an event twice; each issue event fires once.
		if !e.lock(ctx, fmt.Sprintf("alert:issue:%d:%d:%s:%d", r.ID, ev.IssueID, ev.Type, ev.Timestamp.UnixMilli()), 24*time.Hour) {
			continue
		}
		id := ev.IssueID
		entry := Entry{IssueID: &id, TraceID: ev.TraceID, Message: ev.Title}
		if err := e.apply(ctx, r, DecideEvent(r, now), nil, entry, true, now); err != nil {
			slog.Error("alerts: rule failed", "rule_id", r.ID, "err", err)
		}
	}
	return nil
}

// apply sends the outcome's notice, if any, and records it.
func (e *Engine) apply(ctx context.Context, r *Rule, out Outcome, value *float64, entry Entry, triggered bool, now time.Time) error {
	entry.Previous = r.State
	if entry.Previous == "" {
		entry.Previous = OK
	}
	notified := false
	if out.Notice != NoticeNone && out.Suppressed == "" {
		p := e.Payload(r, out, value, entry, now)
		var failures []string
		channels, err := e.Store.Channels(ctx, r.ProjectID, r.ChannelIDs)
		if err != nil {
			return err
		}
		for _, c := range channels {
			if err := e.Sender.Send(ctx, c, p); err != nil {
				failures = append(failures, c.Name+": "+err.Error())
			}
		}
		entry.Failures = strings.Join(failures, "; ")
		notified = true
	}
	return e.Store.Record(ctx, *r, out, value, entry, notified, triggered, now)
}

// Payload builds the notification body for an outcome.
func (e *Engine) Payload(r *Rule, out Outcome, value *float64, entry Entry, now time.Time) Payload {
	p := Payload{
		Notice: out.Notice, RuleID: r.ID, RuleName: r.Name, ProjectID: r.ProjectID,
		State: out.State, Previous: entry.Previous, Metric: r.Metric, Condition: r.Condition(),
		Value: value, Threshold: r.Threshold, Comparison: r.Comparison, WindowSec: int64(r.Window / time.Second),
		TraceID: entry.TraceID, At: now.UTC(),
		URL: fmt.Sprintf("%s/alerts/%d", e.AppURL, r.ID),
	}
	if entry.IssueID != nil {
		p.IssueID = *entry.IssueID
		p.Title = entry.Message
	}
	return p
}

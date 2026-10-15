package alerts

import "time"

// Notice is the kind of notification a transition sends.
type Notice string

const (
	NoticeNone     Notice = ""
	NoticeFiring   Notice = "firing"
	NoticeRepeat   Notice = "still_firing"
	NoticeResolved Notice = "resolved"
)

// Sample is one evaluation of a threshold rule. HasData is false when the
// metric cannot be computed, such as a latency with no requests.
type Sample struct {
	Value   float64
	HasData bool
	// Example links for the history and notifications.
	TraceID string
	IssueID int64
}

// Outcome is what an evaluation decided.
type Outcome struct {
	State State
	// Changed is true when the state differs from before.
	Changed bool
	Notice  Notice
	// Suppressed names why a notice was not sent: "muted" or "cooldown".
	Suppressed string
}

// Decide applies one sample to the rule's current state. It is pure, so the
// state machine can be tested without databases:
//
//	ok|no_data --breach--> alerting   (firing)
//	alerting   --breach--> alerting   (still firing, every Renotify)
//	alerting   --clear---> ok         (resolved)
//	any        --no data-> no_data    (resolved if it was alerting)
func Decide(r *Rule, s Sample, now time.Time) Outcome {
	prev := r.State
	if prev == "" {
		prev = OK
	}
	next := OK
	switch {
	case !s.HasData:
		next = NoData
	case r.Comparison.Breached(s.Value, r.Threshold):
		next = Alerting
	}
	out := Outcome{State: next, Changed: next != prev}
	switch {
	case next == Alerting && prev != Alerting:
		out.Notice = NoticeFiring
		// A rule that resolved and fires again inside the renotify interval
		// is flapping; record it, but don't page again.
		if r.Renotify > 0 && r.LastNotifiedAt != nil && now.Sub(*r.LastNotifiedAt) < r.Renotify {
			out.Suppressed = "cooldown"
		}
	case next == Alerting && r.Renotify > 0 && r.LastNotifiedAt != nil && now.Sub(*r.LastNotifiedAt) >= r.Renotify:
		out.Notice = NoticeRepeat
	case prev == Alerting && next != Alerting:
		out.Notice = NoticeResolved
	}
	if out.Notice != NoticeNone && out.Suppressed == "" && r.Muted(now) {
		out.Suppressed = "muted"
	}
	return out
}

// DecideEvent handles an issue event for an event-driven rule. It always
// fires; repeats inside the renotify interval are recorded without a notice
// so a burst of new issues sends one message.
func DecideEvent(r *Rule, now time.Time) Outcome {
	out := Outcome{State: Alerting, Changed: r.State != Alerting, Notice: NoticeFiring}
	if r.Renotify > 0 && r.LastNotifiedAt != nil && now.Sub(*r.LastNotifiedAt) < r.Renotify {
		out.Suppressed = "cooldown"
	} else if r.Muted(now) {
		out.Suppressed = "muted"
	}
	return out
}

// Settle resolves an event-driven rule once a full window has passed since it
// last fired. It returns false when nothing changes.
func Settle(r *Rule, now time.Time) (Outcome, bool) {
	if r.State != Alerting || r.LastTriggeredAt == nil || now.Sub(*r.LastTriggeredAt) < r.Window {
		return Outcome{}, false
	}
	out := Outcome{State: OK, Changed: true, Notice: NoticeResolved}
	if r.Muted(now) {
		out.Suppressed = "muted"
	}
	return out, true
}

// Due reports whether a threshold rule should be evaluated at now.
func Due(r *Rule, now time.Time) bool {
	if !r.Enabled || r.Metric.EventDriven() {
		return r.Enabled && r.Metric.EventDriven() && r.State == Alerting
	}
	return r.LastEvaluatedAt == nil || now.Sub(*r.LastEvaluatedAt) >= r.Interval
}

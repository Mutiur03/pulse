package processor

import (
	"context"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues"
)

// memIssues is an in-memory IssueRecorder using the real Transition logic.
type memIssues struct {
	byFP map[string]*issues.Issue
	next int64
	// events stands in for the outbox rows Record queues.
	events []issues.Event
}

func (m *memIssues) Record(_ context.Context, o issues.Occurrence) (issues.Issue, issues.Kind, error) {
	cur, ok := m.byFP[o.Fingerprint]
	var status issues.Status
	var resolvedAt *time.Time
	if ok {
		status, resolvedAt = cur.Status, cur.ResolvedAt
	}
	next, kind := issues.Transition(ok, status, resolvedAt, o.Timestamp)
	if !ok {
		m.next++
		cur = &issues.Issue{ID: m.next, Fingerprint: o.Fingerprint, Title: o.Title, FirstSeen: o.Timestamp}
		m.byFP[o.Fingerprint] = cur
	}
	cur.Status, cur.Count, cur.LastSeen = next, cur.Count+1, o.Timestamp
	if kind == issues.KindRegression {
		cur.ResolvedAt = nil
		cur.TimesRegressed++
	}
	if kind != issues.KindNone {
		m.events = append(m.events, issues.NewEvent(*cur, kind, o))
	}
	return *cur, kind, nil
}

func errAt(t time.Time, orderID string) map[string]interface{} {
	return map[string]interface{}{
		"project_id": "1", "timestamp": t.Format(time.RFC3339Nano), "exception_type": "PaymentError",
		"message": "order " + orderID + " declined", "stack": "main.pay()\n\t/app/main.go:10 +0x1",
		"service_name": "api", "trace_id": "t-" + orderID,
	}
}

func TestGroupErrorsNewThenRegression(t *testing.T) {
	ctx := context.Background()
	mem := &memIssues{byFP: map[string]*issues.Issue{}}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	rows, err := GroupErrors(ctx, []map[string]interface{}{errAt(t0, "1"), errAt(t0.Add(time.Second), "2")}, mem)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].IssueID == 0 || rows[0].IssueID != rows[1].IssueID {
		t.Fatalf("both occurrences should land on one issue: %+v", rows)
	}
	if rows[0].Culprit != "main.pay (app/main.go)" || rows[0].Title != "PaymentError: order 1 declined" {
		t.Errorf("culprit %q title %q", rows[0].Culprit, rows[0].Title)
	}
	if len(mem.events) != 1 || mem.events[0].Type != issues.KindNew {
		t.Fatalf("want one new_issue event, got %+v", mem.events)
	}

	// Resolve, then the bug comes back.
	iss := mem.byFP[rows[0].Fingerprint]
	resolved := t0.Add(time.Hour)
	iss.Status, iss.ResolvedAt = issues.Resolved, &resolved

	if _, err := GroupErrors(ctx, []map[string]interface{}{errAt(t0.Add(30*time.Minute), "3")}, mem); err != nil {
		t.Fatal(err)
	}
	if iss.Status != issues.Resolved || len(mem.events) != 1 {
		t.Fatal("an event from before the fix must not reopen the issue")
	}
	if _, err := GroupErrors(ctx, []map[string]interface{}{errAt(t0.Add(2*time.Hour), "4")}, mem); err != nil {
		t.Fatal(err)
	}
	if iss.Status != issues.Unresolved || iss.TimesRegressed != 1 || iss.Count != 4 {
		t.Fatalf("regression not applied: %+v", iss)
	}
	if len(mem.events) != 2 || mem.events[1].Type != issues.KindRegression || mem.events[1].TraceID != "t-4" {
		t.Fatalf("want regression event, got %+v", mem.events)
	}
}

package ch

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	t0 = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	fq = FunnelQuery{Project: 3, Steps: []string{"$pageview", "checkout_started", "payment_succeeded"},
		WindowSeconds: 600, From: t0, To: t0.Add(time.Hour)}
)

func placeholders(q string) int { return strings.Count(q, "?") }

func TestFunnelSQLBindsEveryValue(t *testing.T) {
	for name, build := range map[string]func() (string, []interface{}){
		"health": func() (string, []interface{}) { return FunnelHealthSQL(fq) },
		"errors": func() (string, []interface{}) { return FunnelErrorsSQL(fq, 5) },
	} {
		q, args := build()
		if placeholders(q) != len(args) {
			t.Errorf("%s: %d placeholders, %d args", name, placeholders(q), len(args))
		}
		if !strings.Contains(q, "windowFunnel(600)(toDateTime(timestamp), event = ?, event = ?, event = ?)") {
			t.Errorf("%s: windowFunnel not built as expected:\n%s", name, q)
		}
		if !strings.Contains(q, "range(1, ") {
			t.Errorf("%s: cohorts should cover steps 1..n-1", name)
		}
		for _, s := range fq.Steps {
			if strings.Contains(q, s) {
				t.Errorf("%s: step name %q must be bound, not interpolated", name, s)
			}
		}
	}
}

func TestFunnelValidate(t *testing.T) {
	bad := []FunnelQuery{
		{Steps: []string{"a"}, WindowSeconds: 60, From: t0, To: t0.Add(time.Hour)},
		{Steps: []string{"a", "b", "c", "d", "e", "f"}, WindowSeconds: 60, From: t0, To: t0.Add(time.Hour)},
		{Steps: []string{"a", ""}, WindowSeconds: 60, From: t0, To: t0.Add(time.Hour)},
		{Steps: []string{"a", "b"}, WindowSeconds: 0, From: t0, To: t0.Add(time.Hour)},
		{Steps: []string{"a", "b"}, WindowSeconds: 31 * 24 * 3600, From: t0, To: t0.Add(time.Hour)},
		{Steps: []string{"a", "b"}, WindowSeconds: 60, From: t0, To: t0},
	}
	for i, f := range bad {
		if f.Validate() == nil {
			t.Errorf("case %d should be invalid", i)
		}
	}
	if err := fq.Validate(); err != nil {
		t.Error(err)
	}
}

func TestTracesSQL(t *testing.T) {
	q, args := TracesSQL(TraceFilter{Project: 1, From: t0, To: t0.Add(time.Hour), Service: "api", Status: "error",
		MinDurationNs: 5, MaxDurationNs: 9, NameQuery: "pay", Limit: 10})
	if placeholders(q) != len(args) {
		t.Fatalf("%d placeholders, %d args", placeholders(q), len(args))
	}
	for _, want := range []string{"has(services, ?)", "error_count > 0", "trace_duration_ns >= ?", "trace_duration_ns <= ?", "positionCaseInsensitive"} {
		if !strings.Contains(q, want) {
			t.Errorf("missing %q", want)
		}
	}
	q, args = TracesSQL(TraceFilter{Project: 1, From: t0, To: t0, Limit: 1})
	if strings.Contains(q, "has(services") || len(args) != 6 {
		t.Errorf("no filters should mean only the time range: %s %v", q, args)
	}
}

// TestFunnelClickHouse runs the funnel against a real ClickHouse with the
// Pulse schema when PULSE_TEST_CLICKHOUSE is set (e.g. localhost:9000).
func TestFunnelClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	f := fq
	f.Project = uint64(1_000_000 + rand.Intn(1_000_000))

	// u1: completes. u2: drops after checkout and hit a slow failing payment.
	// u3: drops after the pageview. u4: does checkout outside the window.
	// u5: does the steps out of order.
	type ev struct {
		user, event string
		at          time.Duration
	}
	evs := []ev{
		{"u1", "$pageview", 0}, {"u1", "checkout_started", time.Minute}, {"u1", "payment_succeeded", 2 * time.Minute},
		{"u2", "$pageview", 0}, {"u2", "checkout_started", time.Minute},
		{"u3", "$pageview", 0},
		{"u4", "$pageview", 0}, {"u4", "checkout_started", 20 * time.Minute},
		{"u5", "checkout_started", 0}, {"u5", "$pageview", time.Minute},
	}
	for _, e := range evs {
		if err := c.conn.Exec(ctx, `INSERT INTO events (project_id, event, timestamp, distinct_id, session_id) VALUES (?, ?, ?, ?, ?)`,
			f.Project, e.event, t0.Add(e.at), e.user, "s_"+e.user); err != nil {
			t.Fatal(err)
		}
	}
	span := func(session string, ms int) {
		if err := c.conn.Exec(ctx, `INSERT INTO spans (project_id, timestamp, trace_id, span_id, span_name, span_kind, service_name, platform, duration_ns, session_id)
			VALUES (?, ?, ?, ?, 'POST /api/pay', 'server', 'go-demo-api', 'go', ?, ?)`,
			f.Project, t0.Add(time.Minute), fmt.Sprintf("t%d", rand.Int63()), fmt.Sprintf("s%d", rand.Int63()), uint64(ms)*1e6, session); err != nil {
			t.Fatal(err)
		}
	}
	span("s_u1", 100)
	span("s_u2", 2000)
	span("s_u2", 3000)
	if err := c.conn.Exec(ctx, `INSERT INTO errors (project_id, timestamp, issue_id, title, exception_type, service_name, session_id, distinct_id, message)
		VALUES (?, ?, 77, 'card declined', 'PaymentError', 'go-demo-api', 's_u2', 'u2', 'card declined')`, f.Project, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	d, err := c.RunFunnel(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	levels := map[uint8]uint64{}
	for _, l := range d.Levels {
		levels[l.Level] = l.Users
	}
	// u5 reaches step 1 via the later pageview; u4's checkout is outside the window.
	want := map[uint8]uint64{1: 3, 2: 1, 3: 1}
	for k, v := range want {
		if levels[k] != v {
			t.Errorf("level %d: got %d users want %d (all %v)", k, levels[k], v, levels)
		}
	}
	var dropped2 *FunnelHealth
	for i := range d.Health {
		if d.Health[i].Step == 2 && d.Health[i].Kind == "dropped" {
			dropped2 = &d.Health[i]
		}
	}
	if dropped2 == nil || dropped2.Users != 1 || dropped2.ErrorSessions != 1 || dropped2.P95Ns < 2e9 {
		t.Errorf("step 2 dropped cohort: %+v (all %+v)", dropped2, d.Health)
	}
	found := false
	for _, e := range d.Errors {
		if e.Step == 2 && e.Kind == "dropped" && e.IssueID == 77 && e.Users == 1 {
			found = true
		}
		if e.Kind == "continued" && e.Step == 2 {
			t.Errorf("u1 had no errors, got %+v", e)
		}
	}
	if !found {
		t.Errorf("payment error should be the top error for step 2 drop-offs: %+v", d.Errors)
	}
}

func TestEventsSQL(t *testing.T) {
	q, args := EventsSQL(EventFilter{Project: 1, From: t0, To: t0.Add(time.Hour), Event: "signup", SessionID: "s1", Limit: 50})
	if placeholders(q) != len(args) || len(args) != 12 {
		t.Fatalf("%d placeholders, %d args", placeholders(q), len(args))
	}
	if strings.Count(q, "event = ?") != 2 || strings.Contains(q, "signup") {
		t.Errorf("filters must be bound in both the cutoff and the outer query:\n%s", q)
	}
}

// TestTracesClickHouse checks the trace list read from trace_summaries,
// which the materialized view fills from spans, and the events cutoff.
func TestTracesClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p := uint64(2_000_000 + rand.Intn(1_000_000))
	start := time.Now().UTC().Truncate(time.Hour).Add(-90 * time.Minute)
	span := func(trace, id, parent, name, service, status, platform string, at, ms int) {
		if err := c.conn.Exec(ctx, `INSERT INTO spans (project_id, timestamp, trace_id, span_id, parent_span_id, span_name, service_name, status_code, platform, duration_ns, http_status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 200)`,
			p, start.Add(time.Duration(at)*time.Millisecond), trace, id, parent, name, service, status, platform, uint64(ms)*1e6); err != nil {
			t.Fatal(err)
		}
	}
	// t1 crosses an hour boundary; its child arrives in another insert.
	span("t1", "a", "", "GET /", "web", "ok", "webjs", 0, 31*60*1000)
	span("t1", "b", "a", "SELECT", "api", "error", "go", 30*60*1000+5, 10)
	span("t2", "c", "", "POST /pay", "api", "ok", "go", 1000, 20)

	f := TraceFilter{Project: p, From: start.Add(-time.Minute), To: time.Now().UTC(), Limit: 10}
	rows, err := c.ListTraces(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].TraceID != "t2" || rows[1].TraceID != "t1" {
		t.Fatalf("want t2, t1 newest first: %+v", rows)
	}
	t1 := rows[1]
	if t1.RootName != "GET /" || t1.RootService != "web" || t1.SpanCount != 2 || t1.ErrorCount != 1 || t1.Browser != 1 ||
		len(t1.Services) != 2 || t1.DurationNs != int64(31*60*1000)*1e6 {
		t.Errorf("t1 summary: %+v", t1)
	}
	for _, c2 := range []struct {
		f    TraceFilter
		want int
	}{
		{TraceFilter{Status: "error"}, 1},
		{TraceFilter{Service: "web"}, 1},
		{TraceFilter{NameQuery: "pay"}, 1},
		{TraceFilter{MinDurationNs: 1e9}, 1},
	} {
		q := f
		q.Status, q.Service, q.NameQuery, q.MinDurationNs = c2.f.Status, c2.f.Service, c2.f.NameQuery, c2.f.MinDurationNs
		got, err := c.ListTraces(ctx, q)
		if err != nil || len(got) != c2.want {
			t.Errorf("filter %+v: got %d rows (%v), want %d", c2.f, len(got), err, c2.want)
		}
	}

	for i := 0; i < 5; i++ {
		if err := c.conn.Exec(ctx, `INSERT INTO events (project_id, event, timestamp, distinct_id) VALUES (?, ?, ?, 'u')`,
			p, fmt.Sprintf("e%d", i), start.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	// A long range with a full page in the recent window, and one without.
	long := f
	long.From, long.Limit = start.Add(-48*time.Hour), 1
	if got, err := c.ListTraces(ctx, long); err != nil || len(got) != 1 || got[0].TraceID != "t2" {
		t.Errorf("long range, limit 1: %v %+v", err, got)
	}
	long.Limit = 10
	if got, err := c.ListTraces(ctx, long); err != nil || len(got) != 2 {
		t.Errorf("long range, limit 10: %v %+v", err, got)
	}

	evs, err := c.ListEvents(ctx, EventFilter{Project: p, From: start, To: time.Now().UTC(), Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 || evs[0].Event != "e4" || evs[2].Event != "e2" {
		t.Errorf("want the 3 newest events: %+v", evs)
	}
	evs, err = c.ListEvents(ctx, EventFilter{Project: p, From: start, To: time.Now().UTC(), Event: "e1", Limit: 3})
	if err != nil || len(evs) != 1 {
		t.Errorf("event filter: %v %+v", err, evs)
	}
}

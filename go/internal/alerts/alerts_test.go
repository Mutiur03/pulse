package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/safehttp"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func rule() *Rule {
	return &Rule{ID: 7, ProjectID: 1, Name: "Errors", Metric: ErrorCount, Comparison: Above, Threshold: 5,
		Window: 5 * time.Minute, Interval: time.Minute, Enabled: true, State: OK, ChannelIDs: []int64{1}}
}

func at(d time.Duration) *time.Time { t := t0.Add(d); return &t }

func TestComparison(t *testing.T) {
	cases := []struct {
		c    Comparison
		v    float64
		want bool
	}{{Above, 5, false}, {Above, 6, true}, {AboveOrEqual, 5, true}, {Below, 4, true}, {Below, 5, false}, {BelowOrEqual, 5, true}}
	for _, c := range cases {
		if got := c.c.Breached(c.v, 5); got != c.want {
			t.Errorf("%s %v: got %v", c.c, c.v, got)
		}
	}
}

func TestDecideTransitions(t *testing.T) {
	r := rule()
	out := Decide(r, Sample{Value: 3, HasData: true}, t0)
	if out.State != OK || out.Changed || out.Notice != NoticeNone {
		t.Fatalf("below threshold: %+v", out)
	}
	out = Decide(r, Sample{Value: 9, HasData: true}, t0)
	if out.State != Alerting || !out.Changed || out.Notice != NoticeFiring || out.Suppressed != "" {
		t.Fatalf("breach: %+v", out)
	}
	r.State, r.LastNotifiedAt = Alerting, at(0)
	out = Decide(r, Sample{Value: 9, HasData: true}, t0.Add(time.Minute))
	if out.Changed || out.Notice != NoticeNone {
		t.Fatalf("still breaching without renotify: %+v", out)
	}
	out = Decide(r, Sample{Value: 1, HasData: true}, t0.Add(2*time.Minute))
	if out.State != OK || out.Notice != NoticeResolved {
		t.Fatalf("clear: %+v", out)
	}
	out = Decide(r, Sample{HasData: false}, t0.Add(3*time.Minute))
	if out.State != NoData || out.Notice != NoticeResolved {
		t.Fatalf("no data while alerting resolves: %+v", out)
	}
	r.State = NoData
	out = Decide(r, Sample{HasData: false}, t0)
	if out.Changed || out.Notice != NoticeNone {
		t.Fatalf("no data stays quiet: %+v", out)
	}
	out = Decide(r, Sample{Value: 10, HasData: true}, t0)
	if out.State != Alerting || out.Notice != NoticeFiring {
		t.Fatalf("no data to alerting fires: %+v", out)
	}
}

func TestDecideRenotifyCooldownAndMute(t *testing.T) {
	r := rule()
	r.Renotify = 30 * time.Minute
	r.State, r.LastNotifiedAt = Alerting, at(0)
	if out := Decide(r, Sample{Value: 9, HasData: true}, t0.Add(10*time.Minute)); out.Notice != NoticeNone {
		t.Fatalf("before renotify: %+v", out)
	}
	if out := Decide(r, Sample{Value: 9, HasData: true}, t0.Add(30*time.Minute)); out.Notice != NoticeRepeat {
		t.Fatalf("after renotify: %+v", out)
	}
	// Resolved, then fires again quickly: recorded but not sent.
	r.State = OK
	if out := Decide(r, Sample{Value: 9, HasData: true}, t0.Add(5*time.Minute)); out.Notice != NoticeFiring || out.Suppressed != "cooldown" {
		t.Fatalf("flapping: %+v", out)
	}
	if out := Decide(r, Sample{Value: 9, HasData: true}, t0.Add(31*time.Minute)); out.Suppressed != "" {
		t.Fatalf("after cooldown: %+v", out)
	}
	r.MutedUntil = at(time.Hour)
	if out := Decide(r, Sample{Value: 9, HasData: true}, t0.Add(31*time.Minute)); out.State != Alerting || out.Suppressed != "muted" {
		t.Fatalf("muted still transitions: %+v", out)
	}
	if out := Decide(r, Sample{Value: 9, HasData: true}, t0.Add(2*time.Hour)); out.Suppressed != "" {
		t.Fatalf("mute expired: %+v", out)
	}
}

func TestEventRules(t *testing.T) {
	r := rule()
	r.Metric, r.Renotify = NewIssue, 10*time.Minute
	out := DecideEvent(r, t0)
	if out.State != Alerting || out.Notice != NoticeFiring || out.Suppressed != "" {
		t.Fatalf("first event: %+v", out)
	}
	r.State, r.LastNotifiedAt, r.LastTriggeredAt = Alerting, at(0), at(0)
	if out := DecideEvent(r, t0.Add(time.Minute)); out.Suppressed != "cooldown" {
		t.Fatalf("burst: %+v", out)
	}
	if _, ok := Settle(r, t0.Add(4*time.Minute)); ok {
		t.Fatal("settled inside the window")
	}
	out, ok := Settle(r, t0.Add(5*time.Minute))
	if !ok || out.State != OK || out.Notice != NoticeResolved {
		t.Fatalf("settle: %+v %v", out, ok)
	}
	if !Due(r, t0) {
		t.Fatal("alerting event rule should be checked for settling")
	}
	r.State = OK
	if Due(r, t0) {
		t.Fatal("idle event rule is never due")
	}
}

func TestDue(t *testing.T) {
	r := rule()
	if !Due(r, t0) {
		t.Fatal("never evaluated")
	}
	r.LastEvaluatedAt = at(0)
	if Due(r, t0.Add(30*time.Second)) || !Due(r, t0.Add(time.Minute)) {
		t.Fatal("interval")
	}
	r.Enabled = false
	if Due(r, t0.Add(time.Hour)) {
		t.Fatal("disabled")
	}
}

func TestValidate(t *testing.T) {
	ok := Rule{Name: " High error rate ", Metric: ErrorRate, Threshold: 1}
	if err := ok.Validate(); err != nil || ok.Name != "High error rate" || ok.Window != 5*time.Minute || ok.Interval != time.Minute || ok.Comparison != Above {
		t.Fatalf("defaults: %v %+v", err, ok)
	}
	bad := []Rule{
		{Name: "", Metric: ErrorCount},
		{Name: "x", Metric: "cpu"},
		{Name: "x", Metric: EventCount},
		{Name: "x", Metric: ErrorCount, Comparison: "ne"},
		{Name: "x", Metric: ErrorCount, Window: 30 * time.Second},
		{Name: "x", Metric: ErrorCount, Window: 25 * time.Hour},
		{Name: "x", Metric: ErrorCount, Interval: time.Second},
		{Name: "x", Metric: ErrorCount, Threshold: -1},
	}
	for i, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestChannelValidate(t *testing.T) {
	good := []Channel{{Name: "a", Kind: Email, Target: "ops@example.com"}, {Name: "b", Kind: Webhook, Target: "https://example.com/hook"}, {Name: "c", Kind: Slack, Target: "https://hooks.slack.com/services/x"}}
	for _, c := range good {
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", c.Kind, err)
		}
	}
	bad := []Channel{{Name: "a", Kind: Email, Target: "nope"}, {Name: "a", Kind: Email, Target: "a@b.c\r\nBcc: x"}, {Name: "b", Kind: Webhook, Target: "ftp://x"}, {Name: "", Kind: Slack, Target: "https://x"}, {Name: "d", Kind: "sms", Target: "1"}}
	for _, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
}

func TestMetricQuery(t *testing.T) {
	r := rule()
	r.Metric, r.Service, r.Environment, r.Route = ErrorRate, "api", "prod", "/pay"
	q, err := MetricQuery(r, t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FROM errors", "count() / 5", "service_name = ?", "environment = ?", "http_route = ?", "timestamp >= ?"} {
		if !strings.Contains(q.SQL, want) {
			t.Errorf("missing %q in %s", want, q.SQL)
		}
	}
	if q.Args[1] != t0.Add(-5*time.Minute) || q.Args[2] != t0 || q.Empty {
		t.Errorf("args %v empty %v", q.Args, q.Empty)
	}
	r.Metric = LatencyP99
	q, _ = MetricQuery(r, t0)
	if !strings.Contains(q.SQL, "quantile(0.99)(duration_ns) / 1e6") || !strings.Contains(q.SQL, "span_kind = 'server'") || !q.Empty {
		t.Errorf("latency: %s", q.SQL)
	}
	if s := SampleFrom(q, 0, 0, "", 0); s.HasData {
		t.Error("latency without requests should be no data")
	}
	r.Metric, r.EventName = EventCount, "checkout"
	q, _ = SeriesQuery(r, t0.Add(-time.Hour), t0, 5*time.Minute)
	if !strings.Contains(q.SQL, "FROM events") || !strings.Contains(q.SQL, "INTERVAL 300 SECOND") || !strings.Contains(q.SQL, "GROUP BY b") {
		t.Errorf("series: %s", q.SQL)
	}
	if s := SampleFrom(q, 0, 0, "", 0); !s.HasData {
		t.Error("zero events is a value")
	}
	r.Metric = NewIssue
	if _, err := MetricQuery(r, t0); err == nil {
		t.Error("event metrics have no query")
	}
}

func TestSignVerify(t *testing.T) {
	body := []byte(`{"rule_id":1}`)
	h := Sign("whsec_x", body, t0)
	if !strings.HasPrefix(h, "t=1790856000,v1=") {
		t.Fatalf("header %s", h)
	}
	if !Verify("whsec_x", body, h, t0.Add(time.Minute), 5*time.Minute) {
		t.Fatal("valid signature rejected")
	}
	if Verify("other", body, h, t0, time.Minute) || Verify("whsec_x", []byte(`{"rule_id":2}`), h, t0, time.Minute) {
		t.Fatal("wrong secret or body accepted")
	}
	if Verify("whsec_x", body, h, t0.Add(10*time.Minute), 5*time.Minute) {
		t.Fatal("stale signature accepted")
	}
	if Verify("whsec_x", body, "garbage", t0, time.Minute) {
		t.Fatal("garbage accepted")
	}
}

type fakeMail struct{ sent []mail.Message }

func (f *fakeMail) Send(_ context.Context, m mail.Message) error {
	f.sent = append(f.sent, m)
	return nil
}

func TestNotifierChannels(t *testing.T) {
	var got []*http.Request
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, bodies = append(got, r), append(bodies, b)
	}))
	defer srv.Close()
	fm := &fakeMail{}
	loopback := &safehttp.Policy{AllowNets: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	n := &Notifier{Mail: fm, AppURL: "http://app", Now: func() time.Time { return t0 }, Policy: loopback, Backoff: time.Millisecond}
	v := 12.0
	p := Payload{Notice: NoticeFiring, RuleID: 7, RuleName: "Errors", State: Alerting, Previous: OK, Metric: ErrorCount,
		Condition: "error_count > 5 over 5m", Value: &v, IssueID: 3, TraceID: "abc", URL: "http://app/alerts/7", At: t0}

	if err := n.Send(context.Background(), Channel{Kind: Webhook, Target: srv.URL, Secret: "s"}, p); err != nil {
		t.Fatal(err)
	}
	if !Verify("s", bodies[0], got[0].Header.Get(SignatureHeader), t0, time.Minute) {
		t.Fatal("webhook signature does not verify")
	}
	var decoded Payload
	if err := json.Unmarshal(bodies[0], &decoded); err != nil || decoded.RuleID != 7 || decoded.Notice != NoticeFiring || got[0].Header.Get("X-Pulse-Event") != "firing" {
		t.Fatalf("webhook body %s", bodies[0])
	}

	if err := n.Send(context.Background(), Channel{Kind: Slack, Target: srv.URL}, p); err != nil {
		t.Fatal(err)
	}
	var slack map[string]string
	if err := json.Unmarshal(bodies[1], &slack); err != nil || !strings.Contains(slack["text"], "[Alerting] Errors") {
		t.Fatalf("slack body %s", bodies[1])
	}

	if err := n.Send(context.Background(), Channel{Kind: Email, Target: "ops@example.com"}, p); err != nil {
		t.Fatal(err)
	}
	m := fm.sent[0]
	if m.Subject != "[Alerting] Errors" || !strings.Contains(m.Text, "Value: 12") || !strings.Contains(m.Text, "http://app/issues/3") || !strings.Contains(m.Text, "http://app/traces/abc") {
		t.Fatalf("email %+v", m)
	}

	tries := 0
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tries++; w.WriteHeader(500) }))
	defer failing.Close()
	if err := n.Send(context.Background(), Channel{Kind: Webhook, Target: failing.URL}, p); err == nil || tries != 3 {
		t.Fatalf("HTTP 500 should be retried 3 times and fail: %v after %d", err, tries)
	}

	// A 4xx is final; a flaky endpoint succeeds on the retry.
	tries = 0
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tries++; w.WriteHeader(410) }))
	defer gone.Close()
	if err := n.Send(context.Background(), Channel{Kind: Webhook, Target: gone.URL}, p); err == nil || tries != 1 {
		t.Fatalf("HTTP 410: %v after %d", err, tries)
	}
	tries = 0
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tries++
		if tries == 1 {
			w.WriteHeader(503)
		}
	}))
	defer flaky.Close()
	if err := n.Send(context.Background(), Channel{Kind: Webhook, Target: flaky.URL}, p); err != nil || tries != 2 {
		t.Fatalf("flaky: %v after %d", err, tries)
	}
}

func TestNotifierBlocksInternalTargets(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits++ }))
	defer srv.Close()
	n := &Notifier{Backoff: time.Millisecond}
	p := Payload{Notice: NoticeFiring, RuleName: "x", At: t0}
	for _, k := range []ChannelKind{Webhook, Slack} {
		err := n.Send(context.Background(), Channel{Kind: k, Target: srv.URL}, p)
		if !errors.Is(err, safehttp.ErrBlocked) {
			t.Fatalf("%s to loopback: %v", k, err)
		}
		if err := n.CheckTarget(context.Background(), Channel{Kind: k, Target: "http://169.254.169.254/latest"}); err == nil {
			t.Fatal("metadata address accepted")
		}
	}
	if hits != 0 {
		t.Fatalf("blocked target was reached %d times", hits)
	}
}

type fakeStore struct {
	rules    []Rule
	channels []Channel
	records  []Outcome
	entries  []Entry
}

func (f *fakeStore) Rules(context.Context) ([]Rule, error) { return f.rules, nil }
func (f *fakeStore) EventRules(_ context.Context, p int64, m Metric) ([]Rule, error) {
	var out []Rule
	for _, r := range f.rules {
		if r.ProjectID == p && r.Metric == m {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeStore) Channels(context.Context, int64, []int64) ([]Channel, error) {
	return f.channels, nil
}
func (f *fakeStore) Record(_ context.Context, r Rule, out Outcome, value *float64, e Entry, notified, triggered bool, now time.Time) error {
	f.records, f.entries = append(f.records, out), append(f.entries, e)
	for i := range f.rules {
		if f.rules[i].ID == r.ID {
			f.rules[i].State, f.rules[i].LastEvaluatedAt = out.State, &now
			if notified {
				f.rules[i].LastNotifiedAt = &now
			}
			if triggered {
				f.rules[i].LastTriggeredAt = &now
			}
		}
	}
	return nil
}

type fakeMetrics struct{ v float64 }

func (f *fakeMetrics) AlertSample(context.Context, Query) (float64, uint64, string, uint64, error) {
	return f.v, 1, "trace1", 4, nil
}

type fakeSender struct{ sent []Payload }

func (f *fakeSender) Send(_ context.Context, c Channel, p Payload) error {
	f.sent = append(f.sent, p)
	if c.Kind == Slack {
		return errors.New("boom")
	}
	return nil
}

type memLocks struct {
	mu   sync.Mutex
	held map[string]bool
}

func (m *memLocks) Lock(_ context.Context, key string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held[key] {
		return false, nil
	}
	m.held[key] = true
	return true, nil
}

func TestEngineFiresAndResolves(t *testing.T) {
	now := t0
	st := &fakeStore{rules: []Rule{*rule()}, channels: []Channel{{ID: 1, Name: "hook", Kind: Webhook}, {ID: 2, Name: "slack", Kind: Slack}}}
	met := &fakeMetrics{v: 10}
	snd := &fakeSender{}
	e := &Engine{Store: st, Metrics: met, Sender: snd, AppURL: "http://app", Now: func() time.Time { return now }}

	if n, err := e.Tick(context.Background()); err != nil || n != 1 {
		t.Fatalf("tick %d %v", n, err)
	}
	if len(snd.sent) != 2 || snd.sent[0].Notice != NoticeFiring || snd.sent[0].URL != "http://app/alerts/7" || *snd.sent[0].Value != 10 {
		t.Fatalf("sent %+v", snd.sent)
	}
	if st.entries[0].Failures != "slack: boom" || st.entries[0].TraceID != "trace1" || *st.entries[0].IssueID != 4 {
		t.Fatalf("entry %+v", st.entries[0])
	}
	// Not due again until the interval passes.
	now = now.Add(30 * time.Second)
	if n, _ := e.Tick(context.Background()); n != 0 {
		t.Fatal("evaluated before the interval")
	}
	met.v = 0
	now = now.Add(time.Minute)
	if n, _ := e.Tick(context.Background()); n != 1 {
		t.Fatal("not evaluated")
	}
	if len(snd.sent) != 4 || snd.sent[2].Notice != NoticeResolved || st.rules[0].State != OK {
		t.Fatalf("resolve %+v", snd.sent)
	}
}

func TestEngineLeasesAndIssueEvents(t *testing.T) {
	r := rule()
	r.Metric, r.Window = NewIssue, 10*time.Minute
	st := &fakeStore{rules: []Rule{*r}, channels: []Channel{{ID: 1, Name: "hook", Kind: Webhook}}}
	snd := &fakeSender{}
	now := t0
	e := &Engine{Store: st, Sender: snd, Locks: &memLocks{held: map[string]bool{}}, Now: func() time.Time { return now }}
	ev := issues.Event{Type: issues.KindNew, IssueID: 9, ProjectID: 1, Title: "TypeError: x", TraceID: "t9", Timestamp: t0}
	for i := 0; i < 2; i++ { // a redelivered event fires once
		if err := e.HandleIssueEvent(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(snd.sent) != 1 || snd.sent[0].IssueID != 9 || snd.sent[0].Title != "TypeError: x" || st.rules[0].State != Alerting {
		t.Fatalf("sent %+v", snd.sent)
	}
	if err := e.HandleIssueEvent(context.Background(), issues.Event{Type: issues.KindNew, IssueID: 10, ProjectID: 1, Timestamp: t0.Add(-time.Hour)}); err != nil || len(snd.sent) != 1 {
		t.Fatal("stale events should not fire")
	}
	if err := e.HandleIssueEvent(context.Background(), issues.Event{Type: issues.KindRegression, IssueID: 9, ProjectID: 1}); err != nil || len(snd.sent) != 1 {
		t.Fatal("regression should not fire a new-issue rule")
	}
	now = t0.Add(11 * time.Minute)
	if _, err := e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(snd.sent) != 2 || snd.sent[1].Notice != NoticeResolved || st.rules[0].State != OK {
		t.Fatalf("settle %+v", snd.sent)
	}
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

type fakeAlerts struct {
	rules    map[int64]alerts.Rule
	channels map[int64]alerts.Channel
	muted    *time.Time
}

func (f *fakeAlerts) ListRules(_ context.Context, p int64) ([]alerts.Rule, error) {
	var out []alerts.Rule
	for _, r := range f.rules {
		if r.ProjectID == p {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeAlerts) GetRule(_ context.Context, p, id int64) (alerts.Rule, error) {
	r, ok := f.rules[id]
	if !ok || r.ProjectID != p {
		return r, alerts.ErrNotFound
	}
	return r, nil
}
func (f *fakeAlerts) CreateRule(_ context.Context, r alerts.Rule) (alerts.Rule, error) {
	r.ID, r.State = int64(len(f.rules)+1), alerts.OK
	f.rules[r.ID] = r
	return r, nil
}
func (f *fakeAlerts) UpdateRule(ctx context.Context, r alerts.Rule) (alerts.Rule, error) {
	if _, err := f.GetRule(ctx, r.ProjectID, r.ID); err != nil {
		return r, err
	}
	f.rules[r.ID] = r
	return r, nil
}
func (f *fakeAlerts) DeleteRule(ctx context.Context, p, id int64) error {
	if _, err := f.GetRule(ctx, p, id); err != nil {
		return err
	}
	delete(f.rules, id)
	return nil
}
func (f *fakeAlerts) Mute(ctx context.Context, p, id int64, until *time.Time) (alerts.Rule, error) {
	r, err := f.GetRule(ctx, p, id)
	f.muted, r.MutedUntil = until, until
	return r, err
}
func (f *fakeAlerts) History(context.Context, int64, int) ([]alerts.Entry, error) {
	issue := int64(3)
	return []alerts.Entry{{ID: 1, State: alerts.Alerting, Previous: alerts.OK, Notice: alerts.NoticeFiring, IssueID: &issue, TraceID: "t1"}}, nil
}
func (f *fakeAlerts) ListChannels(context.Context, int64) ([]alerts.Channel, error) {
	var out []alerts.Channel
	for _, c := range f.channels {
		out = append(out, c)
	}
	return out, nil
}
func (f *fakeAlerts) GetChannel(_ context.Context, p, id int64) (alerts.Channel, error) {
	c, ok := f.channels[id]
	if !ok || c.ProjectID != p {
		return c, alerts.ErrNotFound
	}
	return c, nil
}
func (f *fakeAlerts) CreateChannel(_ context.Context, c alerts.Channel) (alerts.Channel, error) {
	c.ID, c.Secret = int64(len(f.channels)+1), "whsec_test"
	f.channels[c.ID] = c
	return c, nil
}
func (f *fakeAlerts) DeleteChannel(context.Context, int64, int64) error { return nil }

type fakeAlertMetrics struct{ q alerts.Query }

func (f *fakeAlertMetrics) AlertSample(_ context.Context, q alerts.Query) (float64, uint64, string, uint64, error) {
	return 7, 7, "", 0, nil
}
func (f *fakeAlertMetrics) AlertSeries(_ context.Context, q alerts.Query) ([]ch.AlertPoint, error) {
	f.q = q
	return []ch.AlertPoint{{At: time.Date(2026, 10, 1, 11, 55, 0, 0, time.UTC), Value: 4, Count: 4}}, nil
}

type recordSender struct{ sent []alerts.Payload }

func (r *recordSender) Send(_ context.Context, _ alerts.Channel, p alerts.Payload) error {
	r.sent = append(r.sent, p)
	return nil
}

func TestAlertsAPI(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	fa := &fakeAlerts{rules: map[int64]alerts.Rule{}, channels: map[int64]alerts.Channel{}}
	fm := &fakeAlertMetrics{}
	snd := &recordSender{}
	s := &Server{Alerts: fa, AlertMetrics: fm, AlertSender: snd, Now: func() time.Time { return now }}
	me := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}
	owner := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "owner"}}}

	// Members cannot manage channels; owners can.
	if rec := call(t, s, me, "POST", "/v1/projects/1/alert-channels", `{"name":"Hook","kind":"webhook","target":"http://example.com/h"}`); rec.Code != 403 {
		t.Fatalf("member creates channel: %d", rec.Code)
	}
	rec := call(t, s, owner, "POST", "/v1/projects/1/alert-channels", `{"name":"Hook","kind":"webhook","target":"http://example.com/h"}`)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"secret":"whsec_test"`) {
		t.Fatalf("create channel: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, me, "GET", "/v1/projects/1/alert-channels", ""); strings.Contains(rec.Body.String(), "whsec_test") {
		t.Fatal("list leaks the signing secret")
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/alert-channels", `{"name":"x","kind":"webhook","target":"file:///etc"}`); rec.Code != 400 {
		t.Fatalf("bad target: %d", rec.Code)
	}
	// The outbound check refuses internal targets before they are saved.
	s.OutboundCheck = func(_ context.Context, u string) error {
		if strings.Contains(u, "169.254.169.254") {
			return errors.New("blocked")
		}
		return nil
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/alert-channels", `{"name":"x","kind":"slack","target":"http://169.254.169.254/latest"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "not allowed") {
		t.Fatalf("metadata target: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, me, "POST", "/v1/projects/1/alert-channels/1/test", ""); rec.Code != 403 {
		t.Fatalf("member test send: %d", rec.Code)
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/alert-channels/1/test", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) || !snd.sent[0].Test {
		t.Fatalf("test send: %d %s", rec.Code, rec.Body)
	}

	body := `{"name":"Errors","metric":"error_count","comparison":"gt","threshold":0,"window_seconds":60,"interval_seconds":10,"channel_ids":[1]}`
	rec = call(t, s, me, "POST", "/v1/projects/1/alerts", body)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"condition":"error_count \u003e 0 over 1m"`) || !strings.Contains(rec.Body.String(), `"state":"ok"`) {
		t.Fatalf("create rule: %d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{`{"name":"x","metric":"cpu"}`, `{"name":"x","metric":"error_count","window_seconds":10}`, `{"name":"","metric":"error_count"}`, `{`} {
		if rec := call(t, s, me, "POST", "/v1/projects/1/alerts", bad); rec.Code != 400 {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	if rec := call(t, s, me, "POST", "/v1/projects/2/alerts", body); rec.Code != 404 {
		t.Fatalf("other project: %d", rec.Code)
	}

	rec = call(t, s, me, "GET", "/v1/projects/1/alerts/1", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"issue_id":3`) || !strings.Contains(rec.Body.String(), `"notice":"firing"`) {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, me, "PUT", "/v1/projects/1/alerts/1", strings.Replace(body, `"threshold":0`, `"threshold":5`, 1)); rec.Code != 200 || fa.rules[1].Threshold != 5 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, me, "PUT", "/v1/projects/1/alerts/9", body); rec.Code != 404 {
		t.Fatalf("update missing: %d", rec.Code)
	}

	rec = call(t, s, me, "POST", "/v1/projects/1/alerts/1/mute", `{"minutes":60}`)
	if rec.Code != 200 || fa.muted == nil || !fa.muted.Equal(now.Add(time.Hour)) {
		t.Fatalf("mute: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, me, "POST", "/v1/projects/1/alerts/1/mute", `{"minutes":0}`); rec.Code != 200 || fa.muted != nil {
		t.Fatal("unmute")
	}

	rec = call(t, s, me, "POST", "/v1/projects/1/alerts/preview", `{"name":"p","metric":"error_count","window_seconds":300}`)
	var pv struct {
		Supported bool
		Points    []struct{ Value float64 }
		Current   *float64
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil || rec.Code != 200 || !pv.Supported || len(pv.Points) != 24 || pv.Current == nil || *pv.Current != 7 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if pv.Points[22].Value != 4 || pv.Points[0].Value != 0 {
		t.Fatalf("preview buckets: %+v", pv.Points)
	}
	if !strings.Contains(fm.q.SQL, "INTERVAL 300 SECOND") {
		t.Fatalf("series query: %s", fm.q.SQL)
	}
	rec = call(t, s, me, "POST", "/v1/projects/1/alerts/preview", `{"name":"p","metric":"new_issue"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"supported":false`) {
		t.Fatalf("event preview: %s", rec.Body)
	}

	if rec := call(t, s, me, "DELETE", "/v1/projects/1/alerts/1", ""); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := call(t, s, me, "GET", "/v1/projects/1/alerts/1", ""); rec.Code != 404 {
		t.Fatalf("deleted: %d", rec.Code)
	}
	if rec := call(t, &Server{}, me, "GET", "/v1/projects/1/alerts", ""); rec.Code != 503 {
		t.Fatalf("unconfigured: %d", rec.Code)
	}
}

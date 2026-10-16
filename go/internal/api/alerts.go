package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// AlertStore keeps alert rules, channels and history.
type AlertStore interface {
	ListRules(ctx context.Context, project int64) ([]alerts.Rule, error)
	GetRule(ctx context.Context, project, id int64) (alerts.Rule, error)
	CreateRule(ctx context.Context, r alerts.Rule) (alerts.Rule, error)
	UpdateRule(ctx context.Context, r alerts.Rule) (alerts.Rule, error)
	DeleteRule(ctx context.Context, project, id int64) error
	Mute(ctx context.Context, project, id int64, until *time.Time) (alerts.Rule, error)
	History(ctx context.Context, rule int64, limit int) ([]alerts.Entry, error)
	ListChannels(ctx context.Context, project int64) ([]alerts.Channel, error)
	GetChannel(ctx context.Context, project, id int64) (alerts.Channel, error)
	CreateChannel(ctx context.Context, c alerts.Channel) (alerts.Channel, error)
	DeleteChannel(ctx context.Context, project, id int64) error
}

// AlertMetrics charts rule metrics for the preview.
type AlertMetrics interface {
	AlertSample(ctx context.Context, q alerts.Query) (float64, uint64, string, uint64, error)
	AlertSeries(ctx context.Context, q alerts.Query) ([]ch.AlertPoint, error)
}

// alertsReady checks access and that alerting is configured.
func (s *Server) alertsReady(w http.ResponseWriter, r *http.Request, project int64) bool {
	if !canRead(r, project) {
		writeErr(w, http.StatusNotFound, "project not found")
		return false
	}
	if s.Alerts == nil {
		writeErr(w, http.StatusServiceUnavailable, "alerting is not configured")
		return false
	}
	return true
}

func alertRule(r alerts.Rule) gen.AlertRule {
	ids := r.ChannelIDs
	if ids == nil {
		ids = []int64{}
	}
	return gen.AlertRule{
		Id: r.ID, ProjectId: r.ProjectID, Name: r.Name, Metric: gen.AlertMetric(r.Metric), Comparison: gen.AlertComparison(r.Comparison),
		Threshold: r.Threshold, WindowSeconds: int(r.Window / time.Second), IntervalSeconds: int(r.Interval / time.Second),
		Service: r.Service, Route: r.Route, Environment: r.Environment, EventName: r.EventName, LogQuery: r.LogQuery,
		RenotifySeconds: int(r.Renotify / time.Second), ChannelIds: ids, Enabled: r.Enabled, MutedUntil: r.MutedUntil,
		State: gen.AlertState(r.State), StateSince: r.StateSince, LastValue: r.LastValue, LastEvaluatedAt: r.LastEvaluatedAt,
		LastNotifiedAt: r.LastNotifiedAt, Condition: r.Condition(), Unit: r.Metric.Unit(), CreatedAt: r.CreatedAt,
	}
}

func ruleFromInput(project int64, in gen.AlertRuleInput) (alerts.Rule, error) {
	r := alerts.Rule{ProjectID: project, Name: in.Name, Metric: alerts.Metric(in.Metric), Enabled: true}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	sec := func(p *int) time.Duration {
		if p == nil {
			return 0
		}
		return time.Duration(*p) * time.Second
	}
	if in.Comparison != nil {
		r.Comparison = alerts.Comparison(*in.Comparison)
	}
	if in.Threshold != nil {
		r.Threshold = *in.Threshold
	}
	r.Window, r.Interval, r.Renotify = sec(in.WindowSeconds), sec(in.IntervalSeconds), sec(in.RenotifySeconds)
	if in.RenotifySeconds != nil && *in.RenotifySeconds < 0 {
		r.Renotify = -1
	}
	r.Service, r.Route, r.Environment, r.EventName = str(in.Service), str(in.Route), str(in.Environment), str(in.EventName)
	r.LogQuery = str(in.LogQuery)
	if in.ChannelIds != nil {
		r.ChannelIDs = *in.ChannelIds
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	return r, r.Validate()
}

func (s *Server) ListAlerts(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !s.alertsReady(w, r, projectID) {
		return
	}
	rules, err := s.Alerts.ListRules(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.AlertRule, 0, len(rules))
	for _, x := range rules {
		out = append(out, alertRule(x))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateAlert(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !s.alertsReady(w, r, projectID) {
		return
	}
	var in gen.AlertRuleInput
	if !decode(w, r, &in) {
		return
	}
	rule, err := ruleFromInput(projectID, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.Alerts.CreateRule(r.Context(), rule)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, alertRule(created))
}

// alertError maps store errors to responses.
func alertError(w http.ResponseWriter, err error) {
	if errors.Is(err, alerts.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	internal(w, err)
}

func (s *Server) GetAlert(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, alertID gen.AlertPath) {
	if !s.alertsReady(w, r, projectID) {
		return
	}
	rule, err := s.Alerts.GetRule(r.Context(), projectID, alertID)
	if err != nil {
		alertError(w, err)
		return
	}
	hist, err := s.Alerts.History(r.Context(), rule.ID, 200)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.AlertDetail{Rule: alertRule(rule), History: make([]gen.AlertHistoryEntry, 0, len(hist))}
	for _, e := range hist {
		out.History = append(out.History, gen.AlertHistoryEntry{
			Id: e.ID, At: e.At, State: gen.AlertState(e.State), Previous: gen.AlertState(e.Previous), Value: e.Value,
			Notice: string(e.Notice), Suppressed: e.Suppressed, Failures: e.Failures, IssueId: e.IssueID, TraceId: e.TraceID, Message: e.Message,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) UpdateAlert(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, alertID gen.AlertPath) {
	if !s.alertsReady(w, r, projectID) {
		return
	}
	var in gen.AlertRuleInput
	if !decode(w, r, &in) {
		return
	}
	rule, err := ruleFromInput(projectID, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID = alertID
	updated, err := s.Alerts.UpdateRule(r.Context(), rule)
	if err != nil {
		alertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, alertRule(updated))
}

func (s *Server) DeleteAlert(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, alertID gen.AlertPath) {
	if !s.alertsReady(w, r, projectID) {
		return
	}
	if err := s.Alerts.DeleteRule(r.Context(), projectID, alertID); err != nil {
		alertError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) MuteAlert(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, alertID gen.AlertPath) {
	if !s.alertsReady(w, r, projectID) {
		return
	}
	var in gen.AlertMute
	if !decode(w, r, &in) {
		return
	}
	if in.Minutes < 0 || in.Minutes > 30*24*60 {
		writeErr(w, http.StatusBadRequest, "minutes must be between 0 and 43200")
		return
	}
	var until *time.Time
	if in.Minutes > 0 {
		t := s.now().Add(time.Duration(in.Minutes) * time.Minute).UTC()
		until = &t
	}
	rule, err := s.Alerts.Mute(r.Context(), projectID, alertID, until)
	if err != nil {
		alertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, alertRule(rule))
}

// previewPoints is how many buckets the preview chart shows.
const previewPoints = 24

func (s *Server) PreviewAlert(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	var in gen.AlertRuleInput
	if !decode(w, r, &in) {
		return
	}
	rule, err := ruleFromInput(projectID, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	bucket := rule.Window
	if max := 7 * 24 * time.Hour / previewPoints; bucket > max {
		bucket = max
	}
	out := gen.AlertPreview{Points: []gen.AlertPoint{}, Threshold: rule.Threshold, Comparison: gen.AlertComparison(rule.Comparison),
		Unit: rule.Metric.Unit(), BucketSeconds: int(bucket / time.Second)}
	if rule.Metric.EventDriven() || s.AlertMetrics == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Supported = true
	now := s.now().UTC()
	// Buckets align to the epoch, as ClickHouse's toStartOfInterval does.
	end := now.Truncate(bucket).Add(bucket)
	start := end.Add(-previewPoints * bucket)
	q, err := alerts.SeriesQuery(&rule, start, now, bucket)
	if err != nil {
		internal(w, err)
		return
	}
	pts, err := s.AlertMetrics.AlertSeries(r.Context(), q)
	if err != nil {
		internal(w, err)
		return
	}
	byTime := map[int64]ch.AlertPoint{}
	for _, p := range pts {
		byTime[p.At.Unix()] = p
	}
	for t := start; t.Before(end); t = t.Add(bucket) {
		p, ok := byTime[t.Unix()]
		if !ok || (q.Empty && p.Count == 0) {
			if q.Empty {
				continue // no requests is a gap, not a zero latency
			}
			p.Value = 0
		}
		out.Points = append(out.Points, gen.AlertPoint{At: t, Value: p.Value})
	}
	mq, err := alerts.MetricQuery(&rule, now)
	if err != nil {
		internal(w, err)
		return
	}
	v, n, trace, issue, err := s.AlertMetrics.AlertSample(r.Context(), mq)
	if err != nil {
		internal(w, err)
		return
	}
	if smp := alerts.SampleFrom(mq, v, n, trace, issue); smp.HasData {
		out.Current = &smp.Value
	}
	writeJSON(w, http.StatusOK, out)
}

func alertChannel(c alerts.Channel, withSecret bool) gen.AlertChannel {
	out := gen.AlertChannel{Id: c.ID, Name: c.Name, Kind: gen.AlertChannelKind(c.Kind), Target: c.Target, CreatedAt: c.CreatedAt}
	if withSecret && c.Secret != "" {
		out.Secret = &c.Secret
	}
	return out
}

func (s *Server) ListAlertChannels(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !s.alertsReady(w, r, projectID) {
		return
	}
	list, err := s.Alerts.ListChannels(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.AlertChannel, 0, len(list))
	for _, c := range list {
		out = append(out, alertChannel(c, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// Channels decide where project data is sent, so only owners manage them;
// members can still list them and edit rules.
func (s *Server) channelOwner(w http.ResponseWriter, r *http.Request, projectID int64) bool {
	return s.alertsReady(w, r, projectID) && owns(w, r, projectID)
}

func (s *Server) CreateAlertChannel(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !s.channelOwner(w, r, projectID) {
		return
	}
	var in gen.AlertChannelInput
	if !decode(w, r, &in) {
		return
	}
	c := alerts.Channel{ProjectID: projectID, Name: in.Name, Kind: alerts.ChannelKind(in.Kind), Target: in.Target}
	if err := c.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if (c.Kind == alerts.Webhook || c.Kind == alerts.Slack) && s.OutboundCheck != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		err := s.OutboundCheck(ctx, c.Target)
		cancel()
		if err != nil {
			writeErr(w, http.StatusBadRequest, "target is not allowed: "+err.Error())
			return
		}
	}
	created, err := s.Alerts.CreateChannel(r.Context(), c)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.AlertChannelCreate, Target: c.Name, Details: map[string]any{"kind": string(c.Kind)}})
	writeJSON(w, http.StatusCreated, alertChannel(created, true))
}

func (s *Server) DeleteAlertChannel(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, channelID gen.ChannelPath) {
	if !s.channelOwner(w, r, projectID) {
		return
	}
	if err := s.Alerts.DeleteChannel(r.Context(), projectID, channelID); err != nil {
		alertError(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.AlertChannelDelete, Target: "channel " + strconv.FormatInt(channelID, 10)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) TestAlertChannel(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, channelID gen.ChannelPath) {
	if !s.channelOwner(w, r, projectID) {
		return
	}
	c, err := s.Alerts.GetChannel(r.Context(), projectID, channelID)
	if err != nil {
		alertError(w, err)
		return
	}
	if s.AlertSender == nil {
		writeErr(w, http.StatusServiceUnavailable, "notifications are not configured")
		return
	}
	p := alerts.Payload{Test: true, Notice: "test", ProjectID: projectID, RuleName: "Test notification",
		State: alerts.OK, Previous: alerts.OK, At: s.now().UTC(), URL: s.AppURL + "/alerts"}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out := gen.AlertChannelTest{Ok: true}
	if err := s.AlertSender.Send(ctx, c, p); err != nil {
		msg := err.Error()
		out = gen.AlertChannelTest{Ok: false, Error: &msg}
	}
	writeJSON(w, http.StatusOK, out)
}

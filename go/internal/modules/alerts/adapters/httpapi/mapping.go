package httpapi

import (
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/api/gen"
)

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

func alertChannel(c alerts.Channel, withSecret bool) gen.AlertChannel {
	out := gen.AlertChannel{Id: c.ID, Name: c.Name, Kind: gen.AlertChannelKind(c.Kind), Target: c.Target, CreatedAt: c.CreatedAt}
	if withSecret && c.Secret != "" {
		out.Secret = &c.Secret
	}
	return out
}

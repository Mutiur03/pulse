// Package service runs the alert API use cases.
package service

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/modules/alerts/domain"
	"github.com/Mutiur03/pulse/internal/modules/alerts/ports"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Service manages alert rules and channels. A nil store disables alerting;
// nil metrics leave previews empty; a nil sender disables channel tests.
type Service struct {
	Store   ports.Store
	Metrics ports.Metrics
	Sender  ports.Sender
	Check   ports.OutboundCheck
	AppURL  string
}

// Enabled reports whether alerting is configured.
func (s *Service) Enabled() bool { return s.Store != nil }

// Point is one preview bucket.
type Point struct {
	At    time.Time
	Value float64
}

// Preview is a rule's recent metric chart.
type Preview struct {
	Bucket    time.Duration
	Supported bool
	Points    []Point
	Current   *float64
}

// Preview charts the rule over the last PreviewPoints buckets.
func (s *Service) Preview(ctx context.Context, rule alerts.Rule, now time.Time) (Preview, error) {
	out := Preview{Bucket: domain.PreviewBucket(rule.Window), Points: []Point{}}
	if rule.Metric.EventDriven() || s.Metrics == nil {
		return out, nil
	}
	out.Supported = true
	bucket := out.Bucket
	// Buckets align to the epoch, as ClickHouse's toStartOfInterval does.
	end := now.Truncate(bucket).Add(bucket)
	start := end.Add(-domain.PreviewPoints * bucket)
	q, err := alerts.SeriesQuery(&rule, start, now, bucket)
	if err != nil {
		return out, err
	}
	pts, err := s.Metrics.AlertSeries(ctx, q)
	if err != nil {
		return out, err
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
		out.Points = append(out.Points, Point{At: t, Value: p.Value})
	}
	mq, err := alerts.MetricQuery(&rule, now)
	if err != nil {
		return out, err
	}
	v, n, trace, issue, err := s.Metrics.AlertSample(ctx, mq)
	if err != nil {
		return out, err
	}
	if smp := alerts.SampleFrom(mq, v, n, trace, issue); smp.HasData {
		out.Current = &smp.Value
	}
	return out, nil
}

// CheckTarget applies the outbound policy to webhook and Slack targets.
func (s *Service) CheckTarget(ctx context.Context, c alerts.Channel) error {
	if (c.Kind != alerts.Webhook && c.Kind != alerts.Slack) || s.Check == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.Check(ctx, c.Target)
}

// SenderEnabled reports whether channel tests can be sent.
func (s *Service) SenderEnabled() bool { return s.Sender != nil }

// TestChannel sends a test notification and returns the delivery error.
func (s *Service) TestChannel(ctx context.Context, c alerts.Channel, now time.Time) error {
	if s.Sender == nil {
		return domain.ErrSenderOff
	}
	p := alerts.Payload{Test: true, Notice: "test", ProjectID: c.ProjectID, RuleName: "Test notification",
		State: alerts.OK, Previous: alerts.OK, At: now.UTC(), URL: s.AppURL + "/alerts"}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.Sender.Send(ctx, c, p)
}

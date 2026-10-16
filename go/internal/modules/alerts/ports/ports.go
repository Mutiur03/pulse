// Package ports declares what the alerts module reads and writes.
package ports

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Store keeps alert rules, channels and history.
type Store interface {
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

// Metrics runs rule metric queries for the preview chart.
type Metrics interface {
	AlertSample(ctx context.Context, q alerts.Query) (float64, uint64, string, uint64, error)
	AlertSeries(ctx context.Context, q alerts.Query) ([]ch.AlertPoint, error)
}

// Sender delivers notifications.
type Sender interface {
	Send(ctx context.Context, c alerts.Channel, p alerts.Payload) error
}

// OutboundCheck rejects webhook targets that are not allowed.
type OutboundCheck func(ctx context.Context, url string) error

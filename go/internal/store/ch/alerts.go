package ch

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
)

// AlertSample runs a metric query that returns one row.
func (c *Client) AlertSample(ctx context.Context, q alerts.Query) (float64, uint64, string, uint64, error) {
	var v float64
	var n, issue uint64
	var trace string
	err := c.conn.QueryRow(ctx, q.SQL, q.Args...).Scan(&v, &n, &trace, &issue)
	return v, n, trace, issue, err
}

// AlertPoint is one bucket of a metric chart.
type AlertPoint struct {
	At    time.Time `ch:"b"`
	Value float64   `ch:"v"`
	Count uint64    `ch:"n"`
	Trace string    `ch:"t"`
	Issue uint64    `ch:"i"`
}

// AlertSeries runs a bucketed metric query.
func (c *Client) AlertSeries(ctx context.Context, q alerts.Query) ([]AlertPoint, error) {
	var out []AlertPoint
	err := c.conn.Select(ctx, &out, q.SQL, q.Args...)
	return out, err
}

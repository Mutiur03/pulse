package ch

import (
	"context"
	"time"
)

// AlertQuery is a ClickHouse query for a threshold metric. It selects the value v,
// the number of rows behind it n, and an example trace t and issue i.
type AlertQuery struct {
	SQL  string
	Args []interface{}
	// Empty is true when zero rows means "no data" rather than a zero value.
	Empty bool
}

// AlertSample runs a metric query that returns one row.
func (c *Client) AlertSample(ctx context.Context, q AlertQuery) (float64, uint64, string, uint64, error) {
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
func (c *Client) AlertSeries(ctx context.Context, q AlertQuery) ([]AlertPoint, error) {
	var out []AlertPoint
	err := c.conn.Select(ctx, &out, q.SQL, q.Args...)
	return out, err
}

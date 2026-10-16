package ch_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// TestAlertQueriesClickHouse runs every metric's evaluation and chart query,
// with all filters set, against a ClickHouse with the Pulse schema when
// PULSE_TEST_CLICKHOUSE is set.
func TestAlertQueriesClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := ch.NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	for _, m := range alerts.Metrics {
		if m.EventDriven() {
			continue
		}
		for _, filtered := range []bool{false, true} {
			r := &alerts.Rule{ProjectID: 987654321, Name: "t", Metric: m, Comparison: alerts.Above, Window: 5 * time.Minute, EventName: "e"}
			if filtered {
				r.Service, r.Route, r.Environment = "svc", "/pay", "prod"
			}
			q, err := alerts.MetricQuery(r, now)
			if err != nil {
				t.Fatal(err)
			}
			v, n, _, _, err := c.AlertSample(ctx, q)
			if err != nil {
				t.Fatalf("%s filtered=%v: %v\n%s", m, filtered, err, q.SQL)
			}
			if s := alerts.SampleFrom(q, v, n, "", 0); n != 0 || (s.HasData && v != 0) {
				t.Errorf("%s: empty project gave v=%v n=%d", m, v, n)
			}
			sq, err := alerts.SeriesQuery(r, now.Add(-time.Hour), now, 5*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.AlertSeries(ctx, sq); err != nil {
				t.Fatalf("%s series: %v\n%s", m, err, sq.SQL)
			}
		}
	}
}

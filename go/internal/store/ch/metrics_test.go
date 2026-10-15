package ch

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestQuantile(t *testing.T) {
	bounds := []float64{10, 50, 100}
	counts := []uint64{0, 10, 10, 0} // 10 in (10,50], 10 in (50,100]
	for q, want := range map[float64]float64{0.5: 50, 0.25: 30, 0.75: 75, 1: 100} {
		if got, ok := Quantile(bounds, counts, q); !ok || got != want {
			t.Errorf("q%.2f = %v, want %v", q, got, want)
		}
	}
	if v, _ := Quantile(bounds, []uint64{5, 0, 0, 0}, 0.9); v != 10 {
		t.Errorf("first bucket reports its bound, got %v", v)
	}
	if v, _ := Quantile(bounds, []uint64{0, 0, 0, 5}, 0.5); v != 100 {
		t.Errorf("overflow bucket reports the last bound, got %v", v)
	}
	if _, ok := Quantile(bounds, []uint64{1, 2}, 0.5); ok {
		t.Error("mismatched buckets must not produce a value")
	}
	if _, ok := Quantile(bounds, []uint64{0, 0, 0, 0}, 0.5); ok {
		t.Error("empty histogram")
	}
}

func TestDefaultAgg(t *testing.T) {
	for _, c := range [][3]string{{"gauge", "", "avg"}, {"sum", "delta", "sum"}, {"sum", "cumulative", "max"}, {"histogram", "delta", "p95"}} {
		if got := defaultAgg(c[0], c[1]); got != c[2] {
			t.Errorf("%v: %s", c, got)
		}
	}
}

// TestMetricsClickHouse inserts points and queries them back when
// PULSE_TEST_CLICKHOUSE is set.
func TestMetricsClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project := uint64(2_000_000 + rand.Intn(1_000_000))
	pid := strconv.FormatUint(project, 10)
	base := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)
	var pts []map[string]interface{}
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		for _, svc := range []string{"api", "worker"} {
			pts = append(pts, map[string]interface{}{
				"project_id": pid, "timestamp": ts, "service_name": svc, "name": "queue.depth", "type": "gauge",
				"value": float64(i) - 1, "attributes": map[string]interface{}{"queue": "q-" + svc},
			}, map[string]interface{}{
				"project_id": pid, "timestamp": ts, "service_name": svc, "name": "http.duration", "type": "histogram", "unit": "ms",
				"temporality": "delta", "value": 40.0, "count": 20.0, "sum": 800.0,
				"bounds": []interface{}{10.0, 50.0, 100.0}, "buckets": []interface{}{0.0, 10.0, 10.0, 0.0},
			})
		}
	}
	if err := c.InsertMetrics(ctx, pts); err != nil {
		t.Fatal(err)
	}
	from, to := base.Add(-time.Minute), base.Add(10*time.Minute)
	names, err := c.MetricNames(ctx, project, from, to)
	if err != nil || len(names) != 2 || names[0].Name != "http.duration" || len(names[0].Services) != 2 {
		t.Fatalf("names %+v %v", names, err)
	}
	g, err := c.QueryMetric(ctx, MetricQuery{Project: project, Name: "queue.depth", From: from, To: to, Step: time.Minute, GroupBy: "service.name"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Agg != "avg" || len(g.Series) != 2 || len(g.Series[0].Points) != 4 || g.Series[0].Points[0].Value != -1 {
		t.Fatalf("gauge %+v", g)
	}
	byAttr, err := c.QueryMetric(ctx, MetricQuery{Project: project, Name: "queue.depth", From: from, To: to, Step: time.Hour, GroupBy: "queue", Agg: "max"})
	if err != nil || len(byAttr.Series) != 2 || byAttr.Series[0].Label == "" {
		t.Fatalf("by attribute %+v %v", byAttr, err)
	}
	h, err := c.QueryMetric(ctx, MetricQuery{Project: project, Name: "http.duration", Service: "api", From: from, To: to, Step: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if h.Agg != "p95" || h.Unit != "ms" || len(h.Series) != 1 || len(h.Series[0].Points) != 4 || h.Series[0].Points[0].Value != 95 {
		t.Fatalf("histogram %+v", h)
	}
	avg, _ := c.QueryMetric(ctx, MetricQuery{Project: project, Name: "http.duration", From: from, To: to, Step: time.Minute, Agg: "avg"})
	if len(avg.Series) != 1 || avg.Series[0].Points[0].Value != 40 {
		t.Fatalf("histogram mean %+v", avg)
	}
}

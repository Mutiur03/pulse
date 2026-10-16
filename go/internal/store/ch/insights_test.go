package ch

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTrendValidate(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ok := TrendQuery{Series: []TrendSeries{{Event: "a"}}, From: from, To: from.Add(48 * time.Hour)}
	if err := ok.Validate(); err != nil || ok.Interval != "day" || ok.Series[0].Math != MathTotal {
		t.Fatalf("defaults: %v %+v", err, ok)
	}
	bad := []TrendQuery{
		{From: from, To: from.Add(time.Hour)},
		{Series: []TrendSeries{{}}, From: from, To: from},
		{Series: []TrendSeries{{}}, Interval: "minute", From: from, To: from.Add(time.Hour)},
		{Series: []TrendSeries{{}}, Interval: "hour", From: from, To: from.Add(2000 * time.Hour)},
		{Series: []TrendSeries{{Math: "sum"}}, From: from, To: from.Add(time.Hour)},
		{Series: []TrendSeries{{Math: "median"}}, From: from, To: from.Add(time.Hour)},
		{Series: []TrendSeries{{Filters: []PropertyFilter{{Property: "x", Op: "like"}}}}, From: from, To: from.Add(time.Hour)},
		{Series: []TrendSeries{{Filters: []PropertyFilter{{Op: "eq"}}}}, From: from, To: from.Add(time.Hour)},
	}
	for i, q := range bad {
		if err := q.Validate(); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func TestTrendSQLArgs(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := TrendQuery{
		Project: 7, Breakdown: "$pathname", Interval: "day", From: from, To: from.Add(72 * time.Hour),
		Filters: []PropertyFilter{{Property: "plan", Op: OpEq, Value: "premium"}},
		Series: []TrendSeries{{Event: "buy", Math: MathSum, Property: "amount", Filters: []PropertyFilter{
			{Property: "amount", Op: OpGt, Value: "5"}, {Property: "coupon", Op: OpIsNotSet}, {Property: "distinct_id", Op: OpContains, Value: "u"},
		}}},
	}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bucketed := range []bool{true, false} {
		sql, args := TrendSQL(q, q.Series[0], bucketed)
		if n := strings.Count(sql, "?"); n != len(args) {
			t.Fatalf("bucketed=%v: %d placeholders, %d args\n%s", bucketed, n, len(args), sql)
		}
		for _, frag := range []string{"JSONHas(properties, ?)", "positionCaseInsensitive(distinct_id, ?)", "LIMIT 10", "'$other'", "sum(toFloat64OrNull"} {
			if !strings.Contains(sql, frag) {
				t.Errorf("missing %q in %s", frag, sql)
			}
		}
		if bucketed != strings.Contains(sql, "toStartOfDay(timestamp) AS bucket") {
			t.Errorf("bucket expression: %s", sql)
		}
		// The user's values only travel as arguments.
		if strings.Contains(sql, "premium") || strings.Contains(sql, "$pathname") {
			t.Errorf("value interpolated: %s", sql)
		}
	}
	sql, args := TraceStatsSQL(TraceStatsQuery{Project: 1, Service: "api", Interval: "hour", From: from, To: from.Add(time.Hour)})
	if strings.Count(sql, "?") != len(args) || !strings.Contains(sql, "parent_span_id = ''") {
		t.Errorf("trace stats: %s %v", sql, args)
	}
}

// TestTrendClickHouse runs trends against a real ClickHouse when
// PULSE_TEST_CLICKHOUSE is set.
func TestTrendClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project := uint64(3_000_000 + rand.Intn(1_000_000))
	pid := strconv.FormatUint(project, 10)
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-48 * time.Hour)
	var evs []map[string]interface{}
	add := func(at time.Time, ev, user, path string, amount float64) {
		evs = append(evs, map[string]interface{}{
			"project_id": pid, "event": ev, "timestamp": at.Format(time.RFC3339Nano), "distinct_id": user, "session_id": "s-" + user,
			"properties": map[string]interface{}{"$pathname": path, "amount": amount},
		})
	}
	// Day 0: u1 views / twice and /cart once, buys 10. Day 1: u2 views / and buys 30.
	add(day.Add(time.Hour), "$pageview", "u1", "/", 0)
	add(day.Add(2*time.Hour), "$pageview", "u1", "/", 0)
	add(day.Add(3*time.Hour), "$pageview", "u1", "/cart", 0)
	add(day.Add(4*time.Hour), "buy", "u1", "/cart", 10)
	add(day.Add(25*time.Hour), "$pageview", "u2", "/", 0)
	add(day.Add(26*time.Hour), "buy", "u2", "/cart", 30)
	if err := c.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	q := TrendQuery{Project: project, From: day, To: day.Add(48 * time.Hour), Series: []TrendSeries{
		{Event: "$pageview"}, {Event: "$pageview", Math: MathUniqueUsers}, {Event: "buy", Math: MathSum, Property: "amount"},
	}}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := c.RunTrend(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(data[0].Buckets) != 2 || data[0].Buckets[0].Value != 3 || data[0].Buckets[1].Value != 1 || data[0].Totals[0].Value != 4 {
		t.Fatalf("total %+v", data[0])
	}
	if data[1].Totals[0].Value != 2 || data[2].Totals[0].Value != 40 {
		t.Fatalf("unique users %+v sum %+v", data[1].Totals, data[2].Totals)
	}
	q.Series = []TrendSeries{{Event: "$pageview", Filters: []PropertyFilter{{Property: "$pathname", Op: OpEq, Value: "/"}}}}
	q.Breakdown = "distinct_id"
	data, err = c.RunTrend(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, r := range data[0].Totals {
		got[r.Term] = r.Value
	}
	if len(got) != 2 || got["u1"] != 2 || got["u2"] != 1 {
		t.Fatalf("breakdown %+v", got)
	}
	q.Breakdown, q.Series = "", []TrendSeries{{Event: "buy", Filters: []PropertyFilter{{Property: "amount", Op: OpGt, Value: "20"}}}}
	if data, err = c.RunTrend(ctx, q); err != nil || data[0].Totals[0].Value != 1 {
		t.Fatalf("numeric filter %+v %v", data, err)
	}
	stats, err := c.TraceStats(ctx, TraceStatsQuery{Project: project, Interval: "hour", From: day, To: day.Add(time.Hour)})
	if err != nil || len(stats) != 0 {
		t.Fatalf("trace stats %+v %v", stats, err)
	}
}

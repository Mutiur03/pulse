package insights

import (
	"fmt"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/golden"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

func TestBuildPathsGolden(t *testing.T) {
	var rows []ch.PathRow
	// Ten distinct second pages so step 1 overflows into Other, paths
	// longer than the step limit, and repeats that merge after truncation.
	for i := 0; i < 10; i++ {
		rows = append(rows, ch.PathRow{Path: []string{"/home", fmt.Sprintf("/p%d", i), "/checkout", "/done"}, Sessions: uint64(10 + i)})
	}
	rows = append(rows,
		ch.PathRow{Path: []string{"/home"}, Sessions: 7},
		ch.PathRow{Path: []string{"/blog", "/home"}, Sessions: 3},
		ch.PathRow{Path: []string{"/home", "/p9", "/cart"}, Sessions: 4},
		ch.PathRow{Path: []string{"/home", "/p9", "/checkout"}, Sessions: 2},
	)
	golden.JSON(t, "paths_3_steps", BuildPaths(rows, 3, 5))
	golden.JSON(t, "paths_1_step", BuildPaths(rows, 1, 50))
	golden.JSON(t, "paths_empty", BuildPaths(nil, 3, 5))
}

func TestBuildGolden(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(4 * 24 * time.Hour)
	day := func(d int) time.Time { return from.Add(time.Duration(d)*24*time.Hour + 3*time.Hour) }
	q := ch.TrendQuery{Interval: "day", From: from, To: to, Series: []ch.TrendSeries{
		{Event: "signup", Math: ch.MathTotal},
		{Event: "", Math: "sum", Property: "amount"},
		{Event: "pay", Math: "p95"},
	}}
	data := []ch.TrendSeriesData{
		{Buckets: []ch.TrendRow{{Bucket: day(0), Value: 2}, {Bucket: day(2), Value: 5}, {Bucket: day(9), Value: 99}},
			Totals: []ch.TrendRow{{Value: 7}}},
		{Buckets: []ch.TrendRow{{Bucket: day(1), Value: 10}}, Totals: []ch.TrendRow{{Value: 10}}},
		{},
	}
	golden.JSON(t, "trend_plain", Build(q, data, nil))

	f, err := ParseFormula("A / B * 100", 3)
	if err != nil {
		t.Fatal(err)
	}
	golden.JSON(t, "trend_formula", Build(q, data, f))

	q.Breakdown = "country"
	bd := []ch.TrendSeriesData{
		{Buckets: []ch.TrendRow{{Bucket: day(0), Term: "BD", Value: 3}, {Bucket: day(1), Term: "US", Value: 4}, {Bucket: day(1), Term: ch.OtherBreakdown, Value: 9}},
			Totals: []ch.TrendRow{{Term: "BD", Value: 3}, {Term: "US", Value: 4}, {Term: ch.OtherBreakdown, Value: 9}, {Term: "DE", Value: 4}}},
		{Buckets: []ch.TrendRow{{Bucket: day(3), Term: "US", Value: 8}}, Totals: []ch.TrendRow{{Term: "US", Value: 8}}},
		{},
	}
	golden.JSON(t, "trend_breakdown", Build(q, bd, nil))
	golden.JSON(t, "trend_breakdown_formula", Build(q, bd, f))
}

package insights

import (
	"math"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

func TestFormula(t *testing.T) {
	v := []float64{10, 4, 0}
	for expr, want := range map[string]float64{
		"A / B * 100": 250, "a+b": 14, "-A + 2*(B - 1)": -4, "A / C": 0, "(A)": 10, "1.5 * B": 6, "A - B - 1": 5, "A / B / 2": 1.25,
	} {
		f, err := ParseFormula(expr, 3)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if got := f.Eval(v); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", expr, got, want)
		}
	}
	for _, bad := range []string{"", "A +", "D", "AB", "A ** B", "(A", "A)", "1..2", "A; drop"} {
		if _, err := ParseFormula(bad, 3); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	deep := ""
	for i := 0; i < 30; i++ {
		deep += "("
	}
	if _, err := ParseFormula(deep+"A", 1); err == nil {
		t.Error("deep nesting should fail")
	}
}

func TestBuckets(t *testing.T) {
	from := time.Date(2026, 3, 4, 15, 30, 0, 0, time.UTC) // a Wednesday
	if b := Buckets(from, from.Add(3*time.Hour), "hour"); len(b) != 4 || b[0].Hour() != 15 || b[0].Minute() != 0 {
		t.Errorf("hours %v", b)
	}
	if b := Buckets(from, from.Add(48*time.Hour), "day"); len(b) != 3 || b[0].Day() != 4 {
		t.Errorf("days %v", b)
	}
	if b := BucketStart(from, "week"); b.Weekday() != time.Monday || b.Day() != 2 {
		t.Errorf("week %v", b)
	}
	if b := BucketStart(from, "month"); b.Day() != 1 || b.Month() != 3 {
		t.Errorf("month %v", b)
	}
}

func TestBuild(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := ch.TrendQuery{From: from, To: from.Add(72 * time.Hour), Interval: "day", Series: []ch.TrendSeries{{Event: "a", Math: ch.MathTotal}, {Event: "b", Math: ch.MathUniqueUsers}}}
	data := []ch.TrendSeriesData{
		{Buckets: []ch.TrendRow{{Bucket: from, Value: 4}, {Bucket: from.Add(48 * time.Hour), Value: 6}}, Totals: []ch.TrendRow{{Value: 10}}},
		{Buckets: []ch.TrendRow{{Bucket: from, Value: 2}}, Totals: []ch.TrendRow{{Value: 2}}},
	}
	r := Build(q, data, nil)
	if len(r.Buckets) != 3 || len(r.Series) != 2 {
		t.Fatalf("%+v", r)
	}
	if s := r.Series[0]; s.Data[0] != 4 || s.Data[1] != 0 || s.Data[2] != 6 || s.Total != 10 || s.Label != "A. a" {
		t.Errorf("series A %+v", s)
	}
	if s := r.Series[1]; s.Label != "B. b (unique_users)" || s.Total != 2 {
		t.Errorf("series B %+v", s)
	}
	f, _ := ParseFormula("A / B", 2)
	r = Build(q, data, f)
	if len(r.Series) != 1 || r.Series[0].Index != -1 || r.Series[0].Data[0] != 2 || r.Series[0].Data[1] != 0 || r.Series[0].Total != 5 {
		t.Errorf("formula %+v", r.Series)
	}

	// Breakdowns: terms by total, $other last, missing terms skipped.
	q.Breakdown = "$pathname"
	data = []ch.TrendSeriesData{
		{Buckets: []ch.TrendRow{{Bucket: from, Term: "/a", Value: 1}, {Bucket: from, Term: ch.OtherBreakdown, Value: 9}, {Bucket: from, Term: "/b", Value: 5}},
			Totals: []ch.TrendRow{{Term: "/a", Value: 1}, {Term: ch.OtherBreakdown, Value: 9}, {Term: "/b", Value: 5}}},
		{Buckets: []ch.TrendRow{{Bucket: from, Term: "/a", Value: 1}}, Totals: []ch.TrendRow{{Term: "/a", Value: 1}}},
	}
	r = Build(q, data, nil)
	if len(r.Series) != 4 || r.Series[0].Breakdown != "/b" || r.Series[1].Breakdown != "/a" || r.Series[2].Breakdown != ch.OtherBreakdown || r.Series[3].Index != 1 {
		t.Errorf("breakdown order %+v", r.Series)
	}
	r = Build(q, data, f)
	if len(r.Series) != 3 || r.Series[1].Breakdown != "/a" || r.Series[1].Total != 1 || r.Series[0].Total != 0 {
		t.Errorf("breakdown formula %+v", r.Series)
	}
}

package insights

import (
	"sort"
	"time"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Series is one chart line: a query series (or the formula) for one
// breakdown term.
type Series struct {
	Label     string
	Index     int // series index, -1 for the formula
	Breakdown string
	Data      []float64
	Total     float64
}

// Result is a computed trend.
type Result struct {
	Buckets []time.Time
	Series  []Series
}

// BucketStart truncates t (UTC) like the ClickHouse interval functions.
func BucketStart(t time.Time, interval string) time.Time {
	t = t.UTC()
	switch interval {
	case "hour":
		return t.Truncate(time.Hour)
	case "week":
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		wd := (int(d.Weekday()) + 6) % 7 // Monday is 0
		return d.AddDate(0, 0, -wd)
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func next(t time.Time, interval string) time.Time {
	switch interval {
	case "hour":
		return t.Add(time.Hour)
	case "week":
		return t.AddDate(0, 0, 7)
	case "month":
		return t.AddDate(0, 1, 0)
	}
	return t.AddDate(0, 0, 1)
}

// Buckets lists every bucket start from from to to.
func Buckets(from, to time.Time, interval string) []time.Time {
	var out []time.Time
	for b := BucketStart(from, interval); b.Before(to); b = next(b, interval) {
		out = append(out, b)
		if len(out) > ch.MaxTrendBuckets+2 {
			break
		}
	}
	return out
}

// SeriesLetter names series i in formulas and legends.
func SeriesLetter(i int) string { return string(rune('A' + i)) }

func label(q ch.TrendQuery, i int) string {
	ev := q.Series[i].Event
	if ev == "" {
		ev = "All events"
	}
	if m := q.Series[i].Math; m != ch.MathTotal {
		ev += " (" + m
		if q.Series[i].Property != "" {
			ev += " of " + q.Series[i].Property
		}
		ev += ")"
	}
	return SeriesLetter(i) + ". " + ev
}

// Build fills empty buckets, orders breakdown terms by total and, with a
// formula, replaces the series by the formula evaluated per term.
func Build(q ch.TrendQuery, data []ch.TrendSeriesData, f *Formula) Result {
	buckets := Buckets(q.From, q.To, q.Interval)
	pos := make(map[int64]int, len(buckets))
	for i, b := range buckets {
		pos[b.Unix()] = i
	}
	type key struct {
		series int
		term   string
	}
	values := map[key][]float64{}
	totals := map[key]float64{}
	termSet := map[string]float64{}
	for i, d := range data {
		for _, r := range d.Buckets {
			k := key{i, r.Term}
			if values[k] == nil {
				values[k] = make([]float64, len(buckets))
			}
			if j, ok := pos[BucketStart(r.Bucket, q.Interval).Unix()]; ok {
				values[k][j] += r.Value
			}
		}
		for _, r := range d.Totals {
			k := key{i, r.Term}
			totals[k] = r.Value
			termSet[r.Term] += r.Value
			if values[k] == nil {
				values[k] = make([]float64, len(buckets))
			}
		}
	}
	terms := make([]string, 0, len(termSet))
	for t := range termSet {
		terms = append(terms, t)
	}
	sort.Slice(terms, func(a, b int) bool {
		if (terms[a] == ch.OtherBreakdown) != (terms[b] == ch.OtherBreakdown) {
			return terms[b] == ch.OtherBreakdown
		}
		if termSet[terms[a]] != termSet[terms[b]] {
			return termSet[terms[a]] > termSet[terms[b]]
		}
		return terms[a] < terms[b]
	})
	if q.Breakdown == "" {
		terms = []string{""}
	}
	out := Result{Buckets: buckets, Series: []Series{}}
	if f != nil {
		for _, t := range terms {
			s := Series{Label: "Formula: " + f.String(), Index: -1, Breakdown: t, Data: make([]float64, len(buckets))}
			v := make([]float64, len(q.Series))
			for j := range buckets {
				for i := range q.Series {
					v[i] = 0
					if d := values[key{i, t}]; d != nil {
						v[i] = d[j]
					}
				}
				s.Data[j] = f.Eval(v)
			}
			for i := range q.Series {
				v[i] = totals[key{i, t}]
			}
			s.Total = f.Eval(v)
			out.Series = append(out.Series, s)
		}
		return out
	}
	for i := range q.Series {
		for _, t := range terms {
			d := values[key{i, t}]
			if d == nil {
				if q.Breakdown != "" {
					continue
				}
				d = make([]float64, len(buckets))
			}
			out.Series = append(out.Series, Series{Label: label(q, i), Index: i, Breakdown: t, Data: d, Total: totals[key{i, t}]})
		}
	}
	return out
}

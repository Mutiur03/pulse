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
	g := collect(q, data, buckets)
	terms := g.sortedTerms()
	if q.Breakdown == "" {
		terms = []string{""}
	}
	out := Result{Buckets: buckets, Series: []Series{}}
	if f != nil {
		for _, t := range terms {
			out.Series = append(out.Series, g.formulaSeries(f, len(q.Series), len(buckets), t))
		}
		return out
	}
	for i := range q.Series {
		for _, t := range terms {
			d := g.values[seriesTerm{i, t}]
			if d == nil {
				if q.Breakdown != "" {
					continue
				}
				d = make([]float64, len(buckets))
			}
			out.Series = append(out.Series, Series{Label: label(q, i), Index: i, Breakdown: t, Data: d, Total: g.totals[seriesTerm{i, t}]})
		}
	}
	return out
}

type seriesTerm struct {
	series int
	term   string
}

// trendGrid is every series' values per bucket and totals, per breakdown
// term.
type trendGrid struct {
	values  map[seriesTerm][]float64
	totals  map[seriesTerm]float64
	termSum map[string]float64
}

// collect places rows into buckets; rows outside the range are dropped.
func collect(q ch.TrendQuery, data []ch.TrendSeriesData, buckets []time.Time) trendGrid {
	pos := make(map[int64]int, len(buckets))
	for i, b := range buckets {
		pos[b.Unix()] = i
	}
	g := trendGrid{values: map[seriesTerm][]float64{}, totals: map[seriesTerm]float64{}, termSum: map[string]float64{}}
	row := func(k seriesTerm) []float64 {
		if g.values[k] == nil {
			g.values[k] = make([]float64, len(buckets))
		}
		return g.values[k]
	}
	for i, d := range data {
		for _, r := range d.Buckets {
			v := row(seriesTerm{i, r.Term})
			if j, ok := pos[BucketStart(r.Bucket, q.Interval).Unix()]; ok {
				v[j] += r.Value
			}
		}
		for _, r := range d.Totals {
			k := seriesTerm{i, r.Term}
			g.totals[k] = r.Value
			g.termSum[r.Term] += r.Value
			row(k)
		}
	}
	return g
}

// sortedTerms orders breakdown terms by total, OtherBreakdown last.
func (g trendGrid) sortedTerms() []string {
	terms := make([]string, 0, len(g.termSum))
	for t := range g.termSum {
		terms = append(terms, t)
	}
	sort.Slice(terms, func(a, b int) bool {
		if (terms[a] == ch.OtherBreakdown) != (terms[b] == ch.OtherBreakdown) {
			return terms[b] == ch.OtherBreakdown
		}
		if g.termSum[terms[a]] != g.termSum[terms[b]] {
			return g.termSum[terms[a]] > g.termSum[terms[b]]
		}
		return terms[a] < terms[b]
	})
	return terms
}

// formulaSeries evaluates the formula per bucket and on the totals.
func (g trendGrid) formulaSeries(f *Formula, nSeries, nBuckets int, term string) Series {
	s := Series{Label: "Formula: " + f.String(), Index: -1, Breakdown: term, Data: make([]float64, nBuckets)}
	v := make([]float64, nSeries)
	for j := 0; j < nBuckets; j++ {
		for i := range v {
			v[i] = 0
			if d := g.values[seriesTerm{i, term}]; d != nil {
				v[i] = d[j]
			}
		}
		s.Data[j] = f.Eval(v)
	}
	for i := range v {
		v[i] = g.totals[seriesTerm{i, term}]
	}
	s.Total = f.Eval(v)
	return s
}

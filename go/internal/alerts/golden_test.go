package alerts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/golden"
)

func TestQueryGolden(t *testing.T) {
	to := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	metrics := []Metric{ErrorCount, ErrorRate, FailureRate, LatencyP95, LatencyP99, Throughput, EventCount, LogCount, NewIssue, "bogus"}
	type out struct {
		Name  string
		SQL   string
		Args  []string
		Empty bool
		Err   string
	}
	var got []out
	for _, m := range metrics {
		for _, filtered := range []bool{false, true} {
			r := &Rule{ProjectID: 3, Metric: m, Window: 5 * time.Minute, EventName: "signup", LogQuery: `level:error "50%"`}
			if filtered {
				r.Service, r.Route, r.Environment = "api", "/pay", "prod"
			}
			for _, bucket := range []time.Duration{0, 30 * time.Second, 10 * time.Minute} {
				var q Query
				var err error
				if bucket == 0 {
					q, err = MetricQuery(r, to)
				} else {
					q, err = SeriesQuery(r, to.Add(-time.Hour), to, bucket)
				}
				o := out{Name: fmt.Sprintf("%s filtered=%v bucket=%s", m, filtered, bucket), SQL: q.SQL, Empty: q.Empty}
				for _, a := range q.Args {
					o.Args = append(o.Args, fmt.Sprint(a))
				}
				if err != nil {
					o.Err = err.Error()
				}
				got = append(got, o)
			}
		}
	}
	r := &Rule{ProjectID: 3, Metric: LogCount, Window: time.Minute, LogQuery: `level:(`}
	_, err := MetricQuery(r, to)
	got = append(got, out{Name: "bad log query", Err: fmt.Sprint(err)})
	golden.JSON(t, "queries", got)
}

func TestValidateGolden(t *testing.T) {
	long := strings.Repeat("x", 201)
	cases := map[string]Rule{
		"defaults":         {Name: "  High errors ", Metric: ErrorRate, LogQuery: "ignored"},
		"empty name":       {Name: " ", Metric: ErrorRate},
		"long name":        {Name: long, Metric: ErrorRate},
		"unknown metric":   {Name: "x", Metric: "nope"},
		"event no name":    {Name: "x", Metric: EventCount, EventName: " "},
		"log query":        {Name: "x", Metric: LogCount, LogQuery: "  level:error   timeout "},
		"bad log query":    {Name: "x", Metric: LogCount, LogQuery: "level:("},
		"bad comparison":   {Name: "x", Metric: ErrorCount, Comparison: "~"},
		"below":            {Name: "x", Metric: Throughput, Comparison: Below, Window: time.Hour, Interval: 30 * time.Second},
		"short window":     {Name: "x", Metric: ErrorCount, Window: time.Second},
		"long window":      {Name: "x", Metric: ErrorCount, Window: 25 * time.Hour},
		"fast interval":    {Name: "x", Metric: ErrorCount, Interval: time.Second},
		"slow interval":    {Name: "x", Metric: ErrorCount, Interval: 2 * time.Hour},
		"negative renot":   {Name: "x", Metric: ErrorCount, Renotify: -time.Second},
		"long renotify":    {Name: "x", Metric: ErrorCount, Renotify: 8 * 24 * time.Hour},
		"negative thresh":  {Name: "x", Metric: ErrorCount, Threshold: -1},
		"long filter":      {Name: "x", Metric: ErrorCount, Route: long},
		"new issue":        {Name: "x", Metric: NewIssue},
		"event with route": {Name: "x", Metric: EventCount, EventName: "signup", Route: "/a", Threshold: 3},
	}
	type out struct {
		Err  string
		Rule Rule
	}
	got := map[string]out{}
	for name, r := range cases {
		r := r
		err := r.Validate()
		o := out{Rule: r}
		if err != nil {
			o.Err = err.Error()
		}
		got[name] = o
	}
	golden.JSON(t, "validate", got)
}

package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/insights"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// BuildFunnel turns windowFunnel levels into per-step counts and attaches
// the health of the users who dropped after each step and of those who
// continued.
func BuildFunnel(q ch.FunnelQuery, d ch.FunnelData) gen.FunnelResult {
	n := len(q.Steps)
	// reached[i] = users whose furthest level is >= i+1
	atLevel := make([]int64, n+1)
	for _, l := range d.Levels {
		if int(l.Level) <= n {
			atLevel[l.Level] += int64(l.Users)
		}
	}
	reached := make([]int64, n)
	var acc int64
	for i := n; i >= 1; i-- {
		acc += atLevel[i]
		reached[i-1] = acc
	}

	cohort := map[[2]interface{}]*gen.CohortHealth{}
	get := func(step uint8, kind string) *gen.CohortHealth {
		k := [2]interface{}{step, kind}
		if c, ok := cohort[k]; ok {
			return c
		}
		c := &gen.CohortHealth{TopErrors: []gen.FunnelError{}}
		cohort[k] = c
		return c
	}
	for _, h := range d.Health {
		c := get(h.Step, h.Kind)
		c.Users, c.Sessions, c.ErrorSessions = int64(h.Users), int64(h.Sessions), int64(h.ErrorSessions)
		c.P95LatencyMs = float32(h.P95Ns / 1e6)
	}
	for _, e := range d.Errors {
		c := get(e.Step, e.Kind)
		c.TopErrors = append(c.TopErrors, gen.FunnelError{
			IssueId: int64(e.IssueID), Title: e.Title, ExceptionType: e.ExceptionType, ServiceName: e.ServiceName,
			Users: int64(e.Users), Count: int64(e.Count),
		})
	}

	out := gen.FunnelResult{Steps: make([]gen.FunnelStep, n), WindowSeconds: q.WindowSeconds, From: q.From, To: q.To}
	for i, ev := range q.Steps {
		st := gen.FunnelStep{Event: ev, Users: reached[i]}
		if reached[0] > 0 {
			st.ConversionOverall = float32(reached[i]) / float32(reached[0])
		}
		if i == 0 {
			if reached[0] > 0 {
				st.ConversionFromPrevious = 1
			}
		} else if reached[i-1] > 0 {
			st.ConversionFromPrevious = float32(reached[i]) / float32(reached[i-1])
		}
		if i < n-1 {
			step := uint8(i + 1)
			st.Dropped = get(step, "dropped")
			st.Continued = get(step, "continued")
			// Users without sessions do not show up in the health rows.
			if st.Dropped.Users == 0 {
				st.Dropped.Users = atLevel[i+1]
			}
			if st.Continued.Users == 0 {
				st.Continued.Users = reached[i+1]
			}
		}
		out.Steps[i] = st
	}
	return out
}

func filters(in *[]gen.PropertyFilter) []ch.PropertyFilter {
	if in == nil {
		return nil
	}
	out := make([]ch.PropertyFilter, 0, len(*in))
	for _, f := range *in {
		pf := ch.PropertyFilter{Property: strings.TrimSpace(f.Property), Op: string(f.Op)}
		if f.Value != nil {
			pf.Value = *f.Value
		}
		out = append(out, pf)
	}
	return out
}

// TrendQuery converts and validates a trend request. It returns the parsed
// formula, if any.
func TrendQuery(now time.Time, project uint64, in gen.TrendQuery) (ch.TrendQuery, *insights.Formula, error) {
	q := ch.TrendQuery{Project: project, Filters: filters(in.Filters)}
	for _, si := range in.Series {
		ts := ch.TrendSeries{Filters: filters(si.Filters)}
		if si.Event != nil {
			ts.Event = strings.TrimSpace(*si.Event)
		}
		if si.Math != nil {
			ts.Math = string(*si.Math)
		}
		if si.Property != nil {
			ts.Property = strings.TrimSpace(*si.Property)
		}
		q.Series = append(q.Series, ts)
	}
	if in.Breakdown != nil {
		q.Breakdown = strings.TrimSpace(*in.Breakdown)
	}
	if in.Interval != nil {
		q.Interval = string(*in.Interval)
	}
	q.To = now.UTC()
	if in.To != nil {
		q.To = in.To.UTC()
	}
	q.From = q.To.Add(-7 * 24 * time.Hour)
	if in.From != nil {
		q.From = in.From.UTC()
	}
	if err := q.Validate(); err != nil {
		return q, nil, err
	}
	var f *insights.Formula
	if in.Formula != nil && strings.TrimSpace(*in.Formula) != "" {
		var err error
		if f, err = insights.ParseFormula(*in.Formula, len(q.Series)); err != nil {
			return q, nil, err
		}
	}
	return q, f, nil
}

// CheckWidget validates a saved dashboard widget query by kind.
func CheckWidget(kind string, raw json.RawMessage) error {
	switch kind {
	case "trend":
		var q gen.TrendQuery
		if err := json.Unmarshal(raw, &q); err != nil {
			return errors.New("invalid trend query")
		}
		_, _, err := TrendQuery(time.Now(), 1, q)
		return err
	case "funnel":
		var q gen.FunnelQuery
		if err := json.Unmarshal(raw, &q); err != nil {
			return errors.New("invalid funnel query")
		}
		fq := ch.FunnelQuery{Steps: q.Steps, WindowSeconds: 3600, From: time.Unix(0, 0), To: time.Unix(1, 0)}
		if q.WindowSeconds != nil {
			fq.WindowSeconds = *q.WindowSeconds
		}
		return fq.Validate()
	case "metric":
		var q struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &q); err != nil || strings.TrimSpace(q.Name) == "" {
			return errors.New("a metric widget needs a metric name")
		}
	case "traces":
		var q struct {
			Service string `json:"service"`
		}
		if err := json.Unmarshal(raw, &q); err != nil {
			return errors.New("invalid traces query")
		}
	}
	return nil
}

func personOut(p ch.PersonRow) gen.Person {
	traits := map[string]interface{}{}
	if p.Traits != "" {
		_ = json.Unmarshal([]byte(p.Traits), &traits)
	}
	// $-prefixed keys are SDK context, not traits the app set.
	for k := range traits {
		if strings.HasPrefix(k, "$") {
			delete(traits, k)
		}
	}
	ids := p.DistinctIDs
	if ids == nil {
		ids = []string{}
	}
	return gen.Person{
		Id: p.Person, DistinctIds: ids, FirstSeen: p.FirstSeen, LastSeen: p.LastSeen, Events: int64(p.Events),
		Sessions: int64(p.Sessions), Traits: traits, Browser: p.Browser, Os: p.OS, Country: p.Country, LastUrl: p.LastURL,
	}
}

package api

import (
	"github.com/Mutiur03/pulse/internal/api/gen"
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

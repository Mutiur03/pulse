package httpapi

import (
	"testing"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

func TestBuildFunnel(t *testing.T) {
	q := ch.FunnelQuery{Steps: []string{"view", "checkout", "paid"}}
	d := ch.FunnelData{
		Levels: []ch.FunnelLevel{{Level: 0, Users: 4}, {Level: 1, Users: 50}, {Level: 2, Users: 30}, {Level: 3, Users: 20}},
		Health: []ch.FunnelHealth{
			{Step: 2, Kind: "dropped", Users: 30, Sessions: 31, ErrorSessions: 18, P95Ns: 2.5e9},
			{Step: 2, Kind: "continued", Users: 20, Sessions: 20, ErrorSessions: 1, P95Ns: 1.2e8},
		},
		Errors: []ch.FunnelError{{Step: 2, Kind: "dropped", IssueID: 7, Title: "card declined", Users: 18, Count: 25}},
	}
	r := BuildFunnel(q, d)
	got := []int64{r.Steps[0].Users, r.Steps[1].Users, r.Steps[2].Users}
	if got[0] != 100 || got[1] != 50 || got[2] != 20 {
		t.Fatalf("reached %v", got)
	}
	if r.Steps[1].ConversionFromPrevious != 0.5 || r.Steps[2].ConversionOverall != 0.2 {
		t.Errorf("conversion %+v", r.Steps)
	}
	dr := r.Steps[1].Dropped
	if dr.Users != 30 || dr.ErrorSessions != 18 || dr.P95LatencyMs != 2500 || len(dr.TopErrors) != 1 || dr.TopErrors[0].IssueId != 7 {
		t.Errorf("dropped %+v", dr)
	}
	if r.Steps[0].Dropped.Users != 50 {
		t.Errorf("step 1 dropped users without sessions should fall back to level counts: %+v", r.Steps[0].Dropped)
	}
	if r.Steps[2].Dropped != nil {
		t.Error("the last step has no drop-off")
	}
}

package insights

import (
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

func TestBuildRetention(t *testing.T) {
	d0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	q := ch.RetentionQuery{Period: "day", Periods: 3, From: d0, To: d0.Add(3 * 24 * time.Hour)}
	data := ch.RetentionData{
		Sizes: []ch.RetentionCell{{Cohort: d0, Users: 10}, {Cohort: d0.AddDate(0, 0, 2), Users: 4}},
		Returns: []ch.RetentionCell{
			{Cohort: d0, Offset: 0, Users: 10}, {Cohort: d0, Offset: 1, Users: 6}, {Cohort: d0, Offset: 2, Users: 3},
			{Cohort: d0.AddDate(0, 0, 2), Offset: 1, Users: 1},
		},
	}
	now := d0.Add(2*24*time.Hour + 12*time.Hour) // March 3rd, midday
	got := BuildRetention(q, data, now)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if c := got[0]; c.Size != 10 || len(c.Values) != 3 || c.Values[0] != 10 || c.Values[1] != 6 || c.Values[2] != 3 {
		t.Errorf("cohort 0 %+v", c)
	}
	if c := got[1]; c.Size != 0 || len(c.Values) != 2 || c.Values[1] != 0 {
		t.Errorf("empty cohort %+v", c)
	}
	// The third cohort started today: only offset 0 is known.
	if c := got[2]; c.Size != 4 || len(c.Values) != 1 {
		t.Errorf("cohort 2 %+v", c)
	}
	wq := ch.RetentionQuery{Period: "week", Periods: 2, From: d0, To: d0.AddDate(0, 0, 14)}
	if w := BuildRetention(wq, ch.RetentionData{}, d0.AddDate(0, 1, 0)); len(w) != 3 || w[0].Start.Weekday() != time.Monday || len(w[0].Values) != 3 {
		t.Errorf("weeks %+v", w)
	}
}

func TestBuildPaths(t *testing.T) {
	rows := []ch.PathRow{
		{Path: []string{"/", "/pricing", "/signup"}, Sessions: 5},
		{Path: []string{"/", "/pricing"}, Sessions: 3},
		{Path: []string{"/", "/docs", "/pricing", "/signup"}, Sessions: 2}, // continues past 3 steps
		{Path: []string{"/blog"}, Sessions: 1},
	}
	p := BuildPaths(rows, 3, 10)
	if p.Total != 11 {
		t.Errorf("total %d", p.Total)
	}
	node := map[string]PathNode{}
	for _, n := range p.Nodes {
		node[string(rune('0'+n.Step))+n.Name] = n
	}
	if n := node["0/"]; n.Sessions != 10 || n.Dropoff != 0 {
		t.Errorf("/ %+v", n)
	}
	if n := node["1/pricing"]; n.Sessions != 8 || n.Dropoff != 3 {
		t.Errorf("/pricing %+v", n)
	}
	// /docs > /pricing went on to /signup beyond the window: not a drop-off.
	if n := node["2/pricing"]; n.Sessions != 2 || n.Dropoff != 0 {
		t.Errorf("step 2 /pricing %+v", n)
	}
	if n := node["0/blog"]; n.Dropoff != 1 {
		t.Errorf("/blog %+v", n)
	}
	if p.Nodes[0].Name != "/" || p.Links[0].Source != "/" || p.Links[0].Target != "/pricing" || p.Links[0].Sessions != 8 {
		t.Errorf("order %+v %+v", p.Nodes[0], p.Links[0])
	}
	if len(p.Top) != 4 || strings.Join(p.Top[0].Path, ">") != "/>/pricing>/signup" || len(p.Top[2].Path) != 3 {
		t.Errorf("top %+v", p.Top)
	}

	// More than MaxNodesPerStep pages at a step fold into Other.
	var many []ch.PathRow
	for i := 0; i < MaxNodesPerStep+3; i++ {
		many = append(many, ch.PathRow{Path: []string{"/", "/p" + string(rune('a'+i))}, Sessions: uint64(20 - i)})
	}
	p = BuildPaths(many, 2, 5)
	var other *PathNode
	steps1 := 0
	for i, n := range p.Nodes {
		if n.Step == 1 {
			steps1++
			if n.Name == OtherNode {
				other = &p.Nodes[i]
			}
		}
	}
	if steps1 != MaxNodesPerStep+1 || other == nil || other.Sessions != 12+11+10 || p.Nodes[len(p.Nodes)-1].Name != OtherNode || len(p.Top) != 5 {
		t.Errorf("other %+v (%d nodes)", other, steps1)
	}
}

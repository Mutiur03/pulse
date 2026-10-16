package insights

import (
	"sort"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Cohort is one row of a retention matrix.
type Cohort struct {
	Start  time.Time
	Size   int64
	Values []int64
}

// BuildRetention lays out cohorts from the range start to its end, one per
// period. Offset 0 is the cohort size; later offsets are left out once
// they lie in the future (after now).
func BuildRetention(q ch.RetentionQuery, d ch.RetentionData, now time.Time) []Cohort {
	sizes := map[int64]uint64{}
	for _, s := range d.Sizes {
		sizes[s.Cohort.Unix()] = s.Users
	}
	ret := map[[2]int64]uint64{}
	for _, r := range d.Returns {
		ret[[2]int64{r.Cohort.Unix(), int64(r.Offset)}] = r.Users
	}
	out := []Cohort{}
	for _, start := range Buckets(q.From, q.To, periodInterval(q.Period)) {
		c := Cohort{Start: start, Size: int64(sizes[start.Unix()]), Values: []int64{}}
		for k := 0; k <= q.Periods; k++ {
			at := advance(start, q.Period, k)
			if at.After(now) {
				break
			}
			if k == 0 {
				c.Values = append(c.Values, c.Size)
				continue
			}
			c.Values = append(c.Values, int64(ret[[2]int64{start.Unix(), int64(k)}]))
		}
		out = append(out, c)
	}
	return out
}

func periodInterval(p string) string {
	if p == "week" {
		return "week"
	}
	return "day"
}

func advance(t time.Time, period string, k int) time.Time {
	if period == "week" {
		return t.AddDate(0, 0, 7*k)
	}
	return t.AddDate(0, 0, k)
}

// PathNode is a page or event at one step.
type PathNode struct {
	Step     int
	Name     string
	Sessions int64
	Dropoff  int64
}

// PathLink is the flow between two nodes of adjacent steps.
type PathLink struct {
	Step           int
	Source, Target string
	Sessions       int64
}

// Paths is a computed path diagram.
type Paths struct {
	Total int64
	Nodes []PathNode
	Links []PathLink
	Top   []ch.PathRow
}

// OtherNode groups the less common nodes of a step.
const OtherNode = "Other"

// MaxNodesPerStep keeps the diagram readable.
const MaxNodesPerStep = 8

// BuildPaths turns distinct paths into nodes per step and links between
// them. Each step keeps its busiest nodes; the rest merge into OtherNode.
func BuildPaths(rows []ch.PathRow, steps, top int) Paths {
	out := Paths{Nodes: []PathNode{}, Links: []PathLink{}, Top: []ch.PathRow{}}
	// Pass 1: counts per (step, node) to choose what to keep.
	count := map[int]map[string]int64{}
	for _, r := range rows {
		out.Total += int64(r.Sessions)
		for i, n := range r.Path {
			if i >= steps {
				break
			}
			if count[i] == nil {
				count[i] = map[string]int64{}
			}
			count[i][n] += int64(r.Sessions)
		}
	}
	keep := map[int]map[string]bool{}
	for step, m := range count {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Slice(names, func(a, b int) bool {
			if m[names[a]] != m[names[b]] {
				return m[names[a]] > m[names[b]]
			}
			return names[a] < names[b]
		})
		keep[step] = map[string]bool{}
		for i, n := range names {
			if i < MaxNodesPerStep {
				keep[step][n] = true
			}
		}
	}
	name := func(step int, n string) string {
		if keep[step][n] {
			return n
		}
		return OtherNode
	}
	type nk struct {
		step int
		name string
	}
	type lk struct {
		step     int
		src, dst string
	}
	nodes := map[nk]*PathNode{}
	links := map[lk]int64{}
	for _, r := range rows {
		path := r.Path
		if len(path) > steps {
			path = path[:steps]
		}
		for i, n := range path {
			k := nk{i, name(i, n)}
			if nodes[k] == nil {
				nodes[k] = &PathNode{Step: i, Name: k.name}
			}
			nodes[k].Sessions += int64(r.Sessions)
			if i == len(path)-1 {
				if len(r.Path) <= steps {
					nodes[k].Dropoff += int64(r.Sessions)
				}
			} else {
				links[lk{i, k.name, name(i+1, path[i+1])}] += int64(r.Sessions)
			}
		}
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, *n)
	}
	sort.Slice(out.Nodes, func(a, b int) bool {
		x, y := out.Nodes[a], out.Nodes[b]
		if x.Step != y.Step {
			return x.Step < y.Step
		}
		if (x.Name == OtherNode) != (y.Name == OtherNode) {
			return y.Name == OtherNode
		}
		if x.Sessions != y.Sessions {
			return x.Sessions > y.Sessions
		}
		return x.Name < y.Name
	})
	for k, v := range links {
		out.Links = append(out.Links, PathLink{Step: k.step, Source: k.src, Target: k.dst, Sessions: v})
	}
	sort.Slice(out.Links, func(a, b int) bool {
		x, y := out.Links[a], out.Links[b]
		if x.Step != y.Step {
			return x.Step < y.Step
		}
		if x.Sessions != y.Sessions {
			return x.Sessions > y.Sessions
		}
		if x.Source != y.Source {
			return x.Source < y.Source
		}
		return x.Target < y.Target
	})
	byPath := map[string]*ch.PathRow{}
	var order []string
	for _, r := range rows {
		path := r.Path
		if len(path) > steps {
			path = path[:steps]
		}
		k := strings.Join(path, "\x00")
		if byPath[k] == nil {
			byPath[k] = &ch.PathRow{Path: path}
			order = append(order, k)
		}
		byPath[k].Sessions += r.Sessions
	}
	for _, k := range order {
		out.Top = append(out.Top, *byPath[k])
	}
	sort.SliceStable(out.Top, func(a, b int) bool { return out.Top[a].Sessions > out.Top[b].Sessions })
	if len(out.Top) > top {
		out.Top = out.Top[:top]
	}
	return out
}

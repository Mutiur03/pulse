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
	count := map[int]map[string]int64{}
	for _, r := range rows {
		out.Total += int64(r.Sessions)
		for i, n := range truncate(r.Path, steps) {
			if count[i] == nil {
				count[i] = map[string]int64{}
			}
			count[i][n] += int64(r.Sessions)
		}
	}
	g := pathGraph{keep: keepBusiest(count), nodes: map[nodeKey]*PathNode{}, links: map[linkKey]int64{}}
	for _, r := range rows {
		g.add(r, steps)
	}
	out.Nodes = g.sortedNodes()
	out.Links = g.sortedLinks()
	out.Top = topPaths(rows, steps, top)
	return out
}

func truncate(path []string, steps int) []string {
	if len(path) > steps {
		return path[:steps]
	}
	return path
}

// keepBusiest picks, per step, the MaxNodesPerStep names with the most
// sessions (ties by name).
func keepBusiest(count map[int]map[string]int64) map[int]map[string]bool {
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
	return keep
}

type nodeKey struct {
	step int
	name string
}

type linkKey struct {
	step     int
	src, dst string
}

// pathGraph accumulates nodes and links, folding uncommon names into
// OtherNode.
type pathGraph struct {
	keep  map[int]map[string]bool
	nodes map[nodeKey]*PathNode
	links map[linkKey]int64
}

func (g *pathGraph) name(step int, n string) string {
	if g.keep[step][n] {
		return n
	}
	return OtherNode
}

// add counts one distinct path. A path that ends within the step limit
// drops off at its last node.
func (g *pathGraph) add(r ch.PathRow, steps int) {
	path := truncate(r.Path, steps)
	for i, n := range path {
		k := nodeKey{i, g.name(i, n)}
		if g.nodes[k] == nil {
			g.nodes[k] = &PathNode{Step: i, Name: k.name}
		}
		g.nodes[k].Sessions += int64(r.Sessions)
		switch {
		case i < len(path)-1:
			g.links[linkKey{i, k.name, g.name(i+1, path[i+1])}] += int64(r.Sessions)
		case len(r.Path) <= steps:
			g.nodes[k].Dropoff += int64(r.Sessions)
		}
	}
}

// sortedNodes orders by step, then busiest first with OtherNode last.
func (g *pathGraph) sortedNodes() []PathNode {
	out := []PathNode{}
	for _, n := range g.nodes {
		out = append(out, *n)
	}
	sort.Slice(out, func(a, b int) bool {
		x, y := out[a], out[b]
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
	return out
}

// sortedLinks orders by step, then busiest first, then names.
func (g *pathGraph) sortedLinks() []PathLink {
	out := []PathLink{}
	for k, v := range g.links {
		out = append(out, PathLink{Step: k.step, Source: k.src, Target: k.dst, Sessions: v})
	}
	sort.Slice(out, func(a, b int) bool {
		x, y := out[a], out[b]
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
	return out
}

// topPaths merges paths that are equal after truncation and returns the
// busiest, keeping first-seen order among ties.
func topPaths(rows []ch.PathRow, steps, top int) []ch.PathRow {
	byPath := map[string]*ch.PathRow{}
	var order []string
	for _, r := range rows {
		path := truncate(r.Path, steps)
		k := strings.Join(path, "\x00")
		if byPath[k] == nil {
			byPath[k] = &ch.PathRow{Path: path}
			order = append(order, k)
		}
		byPath[k].Sessions += r.Sessions
	}
	out := []ch.PathRow{}
	for _, k := range order {
		out = append(out, *byPath[k])
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Sessions > out[b].Sessions })
	if len(out) > top {
		out = out[:top]
	}
	return out
}

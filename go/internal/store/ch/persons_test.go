package ch

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEventColumnsAndAliases(t *testing.T) {
	c := EventColumns(map[string]interface{}{"$current_url": "https://x/a", "$browser": "Chrome", "$os": "macOS", "$country": "BDX", "$release": 5})
	if c.CurrentURL != "https://x/a" || c.Browser != "Chrome" || c.OS != "macOS" || c.Country != "BD" || c.Release != "" {
		t.Errorf("%+v", c)
	}
	if (EventColumns(nil) != Columns{}) {
		t.Error("nil properties")
	}
	got := AliasesFromEvents([]map[string]interface{}{
		{"project_id": "7", "event": "$identify", "distinct_id": "user-1", "timestamp": "2026-01-01T00:00:00Z", "properties": map[string]interface{}{"$anon_distinct_id": "anon-1"}},
		{"project_id": "7", "event": "$create_alias", "distinct_id": "user-1", "properties": map[string]interface{}{"alias": "acct-9"}},
		{"project_id": "7", "event": "$identify", "distinct_id": "user-1", "properties": map[string]interface{}{"$anon_distinct_id": "user-1"}},
		{"project_id": "7", "event": "$identify", "distinct_id": "", "properties": map[string]interface{}{"$anon_distinct_id": "anon-2"}},
		{"project_id": "7", "event": "$pageview", "distinct_id": "user-1", "properties": map[string]interface{}{"alias": "nope"}},
	})
	if len(got) != 2 || got[0].ProjectID != 7 || got[0].DistinctID != "anon-1" || got[0].PersonKey != "user-1" || got[0].At.Year() != 2026 ||
		got[1].DistinctID != "acct-9" || got[1].PersonKey != "user-1" {
		t.Errorf("%+v", got)
	}
}

func TestAnalyticsSQLArgs(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(14 * 24 * time.Hour)
	check := func(name, sql string, args []interface{}) {
		if n := strings.Count(sql, "?"); n != len(args) {
			t.Errorf("%s: %d placeholders, %d args\n%s", name, n, len(args), sql)
		}
	}
	q, a := PersonsSQL(PersonFilter{Project: 1, From: from, To: to, Search: "ann", Limit: 10})
	check("persons", q, a)
	rq := RetentionQuery{Project: 1, StartEvent: "signup", From: from, To: to}
	if err := rq.Validate(); err != nil || rq.Period != "day" || rq.Periods != 7 {
		t.Fatal(err, rq)
	}
	s, sa, r, ra := RetentionSQL(rq)
	check("sizes", s, sa)
	check("returns", r, ra)
	pq := PathsQuery{Project: 1, From: from, To: to}
	if err := pq.Validate(); err != nil || pq.Mode != "pageview" || pq.Steps != 4 {
		t.Fatal(err, pq)
	}
	p, pa := PathsSQL(pq)
	check("paths", p, pa)
	for _, bad := range []RetentionQuery{{Period: "month", From: from, To: to}, {Periods: 31, From: from, To: to}, {From: to, To: from}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v should fail", bad)
		}
	}
	for _, bad := range []PathsQuery{{Mode: "clicks", From: from, To: to}, {Steps: 1, From: from, To: to}, {Steps: 9, From: from, To: to}} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v should fail", bad)
		}
	}
}

// TestAnalyticsClickHouse checks identity stitching, persons, retention and
// paths against a real ClickHouse when PULSE_TEST_CLICKHOUSE is set.
func TestAnalyticsClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project := uint64(4_000_000 + rand.Intn(1_000_000))
	pid := strconv.FormatUint(project, 10)
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-5 * 24 * time.Hour)
	var evs []map[string]interface{}
	add := func(at time.Time, ev, user, session string, props map[string]interface{}) {
		if props == nil {
			props = map[string]interface{}{}
		}
		evs = append(evs, map[string]interface{}{"project_id": pid, "event": ev, "timestamp": at.Format(time.RFC3339Nano),
			"distinct_id": user, "session_id": session, "properties": props})
	}
	pv := func(path string) map[string]interface{} {
		return map[string]interface{}{"$pathname": path, "$browser": "Firefox", "$country": "BD"}
	}
	// Ann browses anonymously, then identifies; later a backend alias.
	add(day.Add(time.Hour), "$pageview", "anon-a", "s1", pv("/"))
	add(day.Add(time.Hour+time.Minute), "$pageview", "anon-a", "s1", pv("/pricing"))
	add(day.Add(time.Hour+2*time.Minute), "$identify", "ann", "s1", map[string]interface{}{"$anon_distinct_id": "anon-a", "email": "ann@example.com"})
	add(day.Add(time.Hour+3*time.Minute), "$pageview", "ann", "s1", pv("/signup"))
	add(day.Add(25*time.Hour), "$pageview", "ann", "s2", pv("/"))
	add(day.Add(25*time.Hour+time.Minute), "$create_alias", "ann", "s2", map[string]interface{}{"alias": "acct-1"})
	add(day.Add(49*time.Hour), "purchase", "acct-1", "", nil)
	// Bob visits once.
	add(day.Add(2*time.Hour), "$pageview", "bob", "s3", pv("/"))
	add(day.Add(2*time.Hour+time.Minute), "$pageview", "bob", "s3", pv("/pricing"))
	add(day.Add(2*time.Hour+2*time.Minute), "$pageview", "bob", "s3", pv("/pricing"))
	if err := c.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	if err := c.InsertAliases(ctx, AliasesFromEvents(evs)); err != nil {
		t.Fatal(err)
	}
	from, to := day, day.Add(5*24*time.Hour)

	persons, err := c.ListPersons(ctx, PersonFilter{Project: project, From: from, To: to, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 2 || persons[0].Person != "ann" || len(persons[0].DistinctIDs) != 3 || persons[0].Sessions != 2 ||
		!strings.Contains(persons[0].Traits, "ann@example.com") || persons[0].Browser != "Firefox" || persons[0].Country != "BD" {
		t.Fatalf("persons %+v", persons)
	}
	found, _ := c.ListPersons(ctx, PersonFilter{Project: project, From: from, To: to, Search: "ACCT", Limit: 10})
	if len(found) != 1 || found[0].Person != "ann" {
		t.Errorf("search %+v", found)
	}
	p, ok, err := c.GetPerson(ctx, project, "ann", from, to)
	if err != nil || !ok || p.Events != 7 {
		t.Fatalf("person %+v %v %v", p, ok, err)
	}
	sessions, err := c.PersonSessions(ctx, project, "ann", from, to, 10)
	if err != nil || len(sessions) != 2 || sessions[0].SessionID != "s2" || sessions[1].Pageviews != 3 || sessions[1].EntryPath != "/" || sessions[1].ExitPath != "/signup" {
		t.Fatalf("sessions %+v %v", sessions, err)
	}
	events, err := c.PersonEvents(ctx, project, "ann", from, to, 3)
	if err != nil || len(events) != 3 || events[0].Event != "purchase" || events[0].DistinctID != "acct-1" {
		t.Fatalf("events %+v %v", events, err)
	}

	// Trends and funnels count persons: Ann's three ids are one user.
	trend, err := c.RunTrend(ctx, TrendQuery{Project: project, Series: []TrendSeries{{Math: MathUniqueUsers}}, Interval: "day", From: from, To: to})
	if err != nil || len(trend) != 1 || len(trend[0].Totals) != 1 || trend[0].Totals[0].Value != 2 {
		t.Fatalf("unique users %+v %v", trend, err)
	}
	funnel, err := c.RunFunnel(ctx, FunnelQuery{Project: project, Steps: []string{"$identify", "purchase"}, WindowSeconds: 7 * 24 * 3600, From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if got := funnelCompleted(funnel); got != 1 {
		t.Errorf("funnel across linked ids: %d completed, %+v", got, funnel.Levels)
	}

	rq := RetentionQuery{Project: project, StartEvent: "$pageview", ReturnEvent: "$pageview", Periods: 3, From: from, To: to}
	_ = rq.Validate()
	d, err := c.RunRetention(ctx, rq)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sizes) != 1 || d.Sizes[0].Users != 2 {
		t.Fatalf("sizes %+v", d.Sizes)
	}
	got := map[uint32]uint64{}
	for _, r := range d.Returns {
		got[r.Offset] = r.Users
	}
	// Day 0: both; day 1: Ann's pageview; day 2: Ann's purchase does not count (return event is $pageview).
	if got[0] != 2 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("returns %+v", d.Returns)
	}
	rq.ReturnEvent = ""
	d, _ = c.RunRetention(ctx, rq)
	for _, r := range d.Returns {
		if r.Offset == 2 && r.Users != 1 {
			t.Errorf("any event on day 2 %+v", d.Returns)
		}
	}

	paths, err := c.RunPaths(ctx, PathsQuery{Project: project, Mode: "pageview", Steps: 3, From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]uint64{}
	for _, r := range paths {
		byPath[strings.Join(r.Path, ">")] = r.Sessions
	}
	if byPath["/>/pricing>/signup"] != 1 || byPath["/>/pricing"] != 1 || byPath["/"] != 1 || len(byPath) != 3 {
		t.Fatalf("paths %v", byPath)
	}
	paths, _ = c.RunPaths(ctx, PathsQuery{Project: project, Mode: "pageview", Start: "/pricing", Steps: 2, From: from, To: to})
	byPath = map[string]uint64{}
	for _, r := range paths {
		byPath[strings.Join(r.Path, ">")] = r.Sessions
	}
	if len(byPath) != 2 || byPath["/pricing>/signup"] != 1 || byPath["/pricing"] != 1 {
		t.Fatalf("paths from /pricing %v", byPath)
	}
}

func funnelCompleted(f FunnelData) uint64 {
	for _, l := range f.Levels {
		if l.Level == 2 {
			return l.Users
		}
	}
	return 0
}

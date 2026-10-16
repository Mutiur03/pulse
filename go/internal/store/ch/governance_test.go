package ch

import (
	"context"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/replay"
)

func TestGovernanceClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project := uint64(6_000_000 + rand.Intn(1_000_000))
	pid := strconv.FormatUint(project, 10)
	now := time.Now().UTC().Truncate(time.Second)
	ev := func(ago time.Duration, name, user, session, trace string, props map[string]interface{}) map[string]interface{} {
		if props == nil {
			props = map[string]interface{}{}
		}
		return map[string]interface{}{"project_id": pid, "event": name, "timestamp": now.Add(-ago).Format(time.RFC3339Nano),
			"distinct_id": user, "session_id": session, "trace_id": trace, "properties": props}
	}
	evs := []map[string]interface{}{
		ev(10*24*time.Hour, "$pageview", "anon-1", "s1", "", nil),
		ev(time.Hour, "$identify", "user-1", "s1", "", map[string]interface{}{"$anon_distinct_id": "anon-1"}),
		ev(time.Hour, "$create_alias", "user-1", "s1", "", map[string]interface{}{"alias": "acct-1"}),
		ev(30*time.Minute, "purchase", "acct-1", "s2", "t1", nil),
		ev(time.Hour, "$pageview", "bob", "s3", "t2", nil),
		ev(10*24*time.Hour, "$pageview", "bob", "s4", "", nil),
	}
	if err := c.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	if err := c.InsertAliases(ctx, AliasesFromEvents(evs)); err != nil {
		t.Fatal(err)
	}
	logs := []map[string]interface{}{
		{"project_id": pid, "timestamp": now.Format(time.RFC3339Nano), "trace_id": "t1", "body": "charged", "attributes": "{}"},
		{"project_id": pid, "timestamp": now.Format(time.RFC3339Nano), "trace_id": "t2", "body": "other", "attributes": "{}"},
	}
	if err := c.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	if err := c.InsertReplayChunk(ctx, replay.Meta{ProjectID: project, SessionID: "s2", DistinctID: "acct-1", Key: "replays/" + pid + "/s2/1-a.json.gz",
		Start: now.Add(-30 * time.Minute), End: now.Add(-29 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	ids, err := c.LinkedIDs(ctx, project, []string{"acct-1"})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	if len(ids) != 3 || ids[0] != "acct-1" || ids[1] != "anon-1" || ids[2] != "user-1" {
		t.Fatalf("linked %v", ids)
	}
	d, err := c.FindPersonData(ctx, project, ids)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(d.Sessions)
	if len(d.Sessions) != 2 || d.Sessions[0] != "s1" || len(d.Traces) != 1 || d.Traces[0] != "t1" || len(d.ChunkKeys) != 1 {
		t.Fatalf("data %+v", d)
	}
	counts, err := c.DeletePersonData(ctx, project, ids, d)
	if err != nil {
		t.Fatal(err)
	}
	if counts["events"] != 4 || counts["logs"] != 1 || counts["replay_chunks"] != 1 || counts["person_aliases"] != 2 {
		t.Fatalf("counts %v", counts)
	}
	left, err := c.ListEvents(ctx, EventFilter{Project: project, From: now.Add(-30 * 24 * time.Hour), To: now.Add(time.Minute), Limit: 50})
	if err != nil || len(left) != 2 {
		t.Fatalf("left %d %v", len(left), err)
	}
	for _, e := range left {
		if e.DistinctID != "bob" {
			t.Fatalf("kept %+v", e)
		}
	}
	if again, _ := c.LinkedIDs(ctx, project, []string{"acct-1"}); len(again) != 1 {
		t.Errorf("aliases left: %v", again)
	}

	// Retention removes only rows older than the cutoff.
	n, err := c.DeleteOlder(ctx, project, "events", now.Add(-7*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("older %d %v", n, err)
	}
	if n, _ := c.DeleteOlder(ctx, project, "events", now.Add(-7*24*time.Hour)); n != 0 {
		t.Errorf("second run deleted %d", n)
	}
	if _, err := c.DeleteOlder(ctx, project, "users", now); err == nil {
		t.Error("unknown kind accepted")
	}
}

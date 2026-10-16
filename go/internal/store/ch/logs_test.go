package ch

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/logquery"
)

func TestLogsClickHouse(t *testing.T) {
	addr := os.Getenv("PULSE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("PULSE_TEST_CLICKHOUSE not set")
	}
	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project := uint64(5_000_000 + rand.Intn(1_000_000))
	pid := strconv.FormatUint(project, 10)
	now := time.Now().UTC().Truncate(time.Second)
	rec := func(ago time.Duration, sev, svc, body, trace, attrs string) map[string]interface{} {
		return map[string]interface{}{"project_id": pid, "timestamp": now.Add(-ago).Format(time.RFC3339Nano), "severity": sev,
			"service_name": svc, "body": body, "trace_id": trace, "span_id": "", "attributes": attrs}
	}
	logs := []map[string]interface{}{
		rec(5*time.Minute, "INFO", "api", "authorizing payment", "t1", `{"order_id":7}`),
		rec(4*time.Minute, "WARN", "api", "payment provider timed out", "t1", `{"order_id":7}`),
		rec(3*time.Minute, "ERROR", "api", "Payment failed", "t1", `{"order_id":7,"error":"declined"}`),
		rec(2*time.Minute, "INFO", "web", "rendered page", "", `{"path":"/"}`),
		rec(time.Minute, "ERROR", "api", "payment failed", "t2", `{"order_id":"8"}`),
	}
	if err := c.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	base := LogFilter{Project: project, From: now.Add(-time.Hour), To: now.Add(time.Minute), Limit: 10}
	search := func(q string, mod func(*LogFilter)) []LogRow {
		t.Helper()
		f := base
		f.Query, err = logquery.Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		if mod != nil {
			mod(&f)
		}
		rows, err := c.SearchLogs(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return rows
	}
	for q, want := range map[string]int{
		"":                            5,
		"payment":                     4,
		`"payment failed"`:            2,
		"level:warn+":                 3,
		"level:error service:api":     2,
		"-service:api":                1,
		"trace:T1":                    3,
		"order_id:7":                  3,
		"order_id:8":                  1,
		"error:declined level:error+": 1,
		"payment -level:info":         3,
	} {
		if got := len(search(q, nil)); got != want {
			t.Errorf("%q: %d logs, want %d", q, got, want)
		}
	}
	rows := search("", func(f *LogFilter) { f.Limit = 2 })
	if len(rows) != 2 || rows[0].Body != "payment failed" || rows[0].Attributes != `{"order_id":"8"}` {
		t.Fatalf("newest first: %+v", rows)
	}
	older := search("", func(f *LogFilter) { f.Before = rows[1].Timestamp })
	if len(older) != 3 {
		t.Errorf("before: %d", len(older))
	}
	newer := search("", func(f *LogFilter) { f.After = now.Add(-150 * time.Second) })
	if len(newer) != 2 {
		t.Errorf("after: %d", len(newer))
	}

	f := base
	f.Query, _ = logquery.Parse("level:warn+")
	hist, err := c.LogHistogram(ctx, f, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var total uint64
	for _, b := range hist {
		total += b.Count
	}
	if total != 3 {
		t.Errorf("histogram total %d: %+v", total, hist)
	}
	facets, err := c.LogFacets(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]uint64{}
	for _, fc := range facets {
		got[fc.Field+"="+fc.Value] = fc.Count
	}
	if got["service=api"] != 4 || got["service=web"] != 1 || got["level=ERROR"] != 2 || got["level=INFO"] != 2 || got["level=WARN"] != 1 {
		t.Errorf("facets %v", got)
	}
}

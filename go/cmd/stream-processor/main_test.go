package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestRunFailsFastWithoutClickHouse(t *testing.T) {
	cfg := config.Load()
	cfg.ClickHouse.Addr = "127.0.0.1:1"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := run(ctx, cfg); err == nil {
		t.Fatal("run succeeded without ClickHouse")
	}
}

func TestSplitRecords(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recs := []*kgo.Record{
		{Topic: "raw.events", Value: []byte(`{"event":"a"}`), Timestamp: t0},
		{Topic: "raw.events", Value: []byte(`{"event":"b"}`), Timestamp: t0.Add(time.Second)},
		{Topic: "raw.spans", Value: []byte(`{}`), Timestamp: t0},
		{Topic: "raw.errors", Value: []byte(`{}`), Timestamp: t0},
		{Topic: "raw.logs", Value: []byte(`{}`), Timestamp: t0},
		{Topic: "raw.metrics", Value: []byte(`{}`), Timestamp: t0},
		{Topic: "raw.logs", Value: []byte(`not json`), Timestamp: t0.Add(time.Hour)},
		{Topic: "other", Value: []byte(`{}`), Timestamp: t0},
	}
	b := splitRecords(recs)
	if len(b.events) != 2 || len(b.spans) != 1 || len(b.errs) != 1 || len(b.logs) != 1 || len(b.metrics) != 1 {
		t.Fatalf("%+v", b)
	}
	if !b.newest["raw.events"].Equal(t0.Add(time.Second)) || !b.newest["raw.logs"].Equal(t0.Add(time.Hour)) {
		t.Fatalf("newest %v", b.newest)
	}
}

type fakeSink struct {
	calls []string
	fail  bool
}

func (f *fakeSink) do(name string) error {
	f.calls = append(f.calls, name)
	if f.fail {
		return errors.New("down")
	}
	return nil
}
func (f *fakeSink) InsertEvents(context.Context, []row) error       { return f.do("events") }
func (f *fakeSink) InsertAliases(context.Context, []ch.Alias) error { return f.do("aliases") }
func (f *fakeSink) InsertSpans(context.Context, []row) error        { return f.do("spans") }
func (f *fakeSink) InsertErrors(context.Context, []ch.ErrorRow) error {
	return f.do("errors")
}
func (f *fakeSink) InsertLogs(context.Context, []row) error    { return f.do("logs") }
func (f *fakeSink) InsertMetrics(context.Context, []row) error { return f.do("metrics") }

type fakeIssues struct{}

func (fakeIssues) Record(_ context.Context, o issues.Occurrence) (issues.Issue, issues.Kind, error) {
	return issues.Issue{ID: 1, Fingerprint: o.Fingerprint}, issues.KindNone, nil
}

type fakePub struct{}

func (fakePub) Produce(context.Context, string, []byte, []byte) error { return nil }

func TestPipelineStore(t *testing.T) {
	b := batch{
		events:  []row{{"event": "$identify", "distinct_id": "u1", "properties": map[string]interface{}{"$anon_distinct_id": "a1"}}},
		spans:   []row{{}},
		errs:    []row{{"exception_type": "TypeError", "message": "boom", "project_id": "1"}},
		logs:    []row{{}},
		metrics: []row{{}},
	}
	s := &fakeSink{}
	p := &pipeline{sink: s, issues: fakeIssues{}, publisher: fakePub{}}
	if !p.store(context.Background(), b) {
		t.Fatal("store failed")
	}
	want := map[string]bool{"events": true, "spans": true, "errors": true, "logs": true, "metrics": true}
	for _, c := range s.calls {
		delete(want, c)
	}
	if len(want) != 0 {
		t.Fatalf("missing writes %v (calls %v)", want, s.calls)
	}

	// On shutdown writes stop retrying and the batch is not committed.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if (&pipeline{sink: &fakeSink{fail: true}, issues: fakeIssues{}, publisher: fakePub{}}).store(ctx, batch{spans: []row{{}}}) {
		t.Fatal("store reported success after failed write")
	}
}

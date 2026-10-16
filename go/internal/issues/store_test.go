package issues

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/pgtest"
)

func TestStore(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := &Store{db: db}
	var project int64
	if err := db.QueryRow(ctx, `INSERT INTO projects (name, project_key) VALUES ('p', 'k') RETURNING id`).Scan(&project); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	occ := func(fp, title string, at time.Time) Occurrence {
		return Occurrence{ProjectID: project, Fingerprint: fp, Title: title, ExceptionType: "TypeError",
			Culprit: "app.js", Platform: "javascript", ServiceName: "web", TraceID: "t-" + fp, SpanID: "s", Timestamp: at}
	}

	a, kind, err := s.Record(ctx, occ("a", "boom", t0))
	if err != nil || kind != KindNew || a.Count != 1 || a.Status != Unresolved {
		t.Fatalf("first: %+v %q %v", a, kind, err)
	}
	a, kind, _ = s.Record(ctx, occ("a", "boom", t0.Add(-time.Hour)))
	if kind != KindNone || a.Count != 2 || !a.FirstSeen.Equal(t0.Add(-time.Hour)) || !a.LastSeen.Equal(t0) {
		t.Fatalf("second: %+v %q", a, kind)
	}
	if _, _, err := s.Record(ctx, occ("b", "other crash", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}

	r, err := s.SetStatus(ctx, a.ID, Resolved)
	if err != nil || r.Status != Resolved || r.ResolvedAt == nil {
		t.Fatalf("resolve: %+v %v", r, err)
	}
	// An event from before the fix does not reopen it.
	if got, kind, _ := s.Record(ctx, occ("a", "boom", t0)); kind != KindNone || got.Status != Resolved {
		t.Fatalf("late event: %+v %q", got, kind)
	}
	got, kind, _ := s.Record(ctx, occ("a", "boom", time.Now().Add(time.Hour)))
	if kind != KindRegression || got.Status != Unresolved || got.TimesRegressed != 1 || got.ResolvedAt != nil {
		t.Fatalf("regression: %+v %q", got, kind)
	}

	if g, err := s.Get(ctx, a.ID); err != nil || g.Fingerprint != "a" {
		t.Fatalf("get: %+v %v", g, err)
	}
	if _, err := s.Get(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if _, err := s.SetStatus(ctx, 999999, Ignored); !errors.Is(err, ErrNotFound) {
		t.Fatalf("status missing: %v", err)
	}

	cases := []struct {
		f     ListFilter
		total int64
		first string
	}{
		{ListFilter{ProjectID: project, Limit: 10}, 2, "a"},
		{ListFilter{ProjectID: project, Sort: "count", Limit: 10}, 2, "a"},
		{ListFilter{ProjectID: project, Sort: "first_seen", Limit: 10}, 2, "b"},
		{ListFilter{ProjectID: project, Query: "OTHER", Limit: 10}, 1, "b"},
		{ListFilter{ProjectID: project, Status: Unresolved, Service: "web", Limit: 1}, 2, "a"},
		{ListFilter{ProjectID: project, Status: Ignored, Limit: 10}, 0, ""},
	}
	for _, c := range cases {
		list, total, err := s.List(ctx, c.f)
		if err != nil || total != c.total {
			t.Fatalf("%+v: total %d %v", c.f, total, err)
		}
		if c.first != "" && (len(list) == 0 || list[0].Fingerprint != c.first) {
			t.Fatalf("%+v: list %+v", c.f, list)
		}
	}
	if s.Pool() != db {
		t.Fatal("pool")
	}
}

func TestConnect(t *testing.T) {
	if _, err := Connect(context.Background(), "postgres://nobody@127.0.0.1:1/x?connect_timeout=1"); err == nil {
		t.Fatal("connect to closed port succeeded")
	}
}

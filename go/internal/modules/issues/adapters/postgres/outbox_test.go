package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues/domain"
	"github.com/Mutiur03/pulse/internal/pgtest"
)

func TestOutbox(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := New(db)
	var project int64
	if err := db.QueryRow(ctx, `INSERT INTO projects (name, project_key) VALUES ('p', 'k') RETURNING id`).Scan(&project); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	occ := domain.Occurrence{ProjectID: project, Fingerprint: "a", Title: "boom", ServiceName: "api", TraceID: "t1", Timestamp: t0}
	iss, _, err := s.Record(ctx, occ)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Record(ctx, occ); err != nil { // repeat: no event
		t.Fatal(err)
	}
	if _, err := s.SetStatus(ctx, iss.ID, domain.Resolved); err != nil {
		t.Fatal(err)
	}
	occ.Timestamp = time.Now().Add(time.Minute)
	if _, kind, err := s.Record(ctx, occ); err != nil || kind != domain.KindRegression {
		t.Fatalf("regression: %q %v", kind, err)
	}

	// Kafka down: nothing is marked sent.
	down := errors.New("kafka down")
	if n, err := s.RelayOutbox(ctx, 10, func(context.Context, int64, []byte) error { return down }); n != 0 || !errors.Is(err, down) {
		t.Fatalf("down: %d %v", n, err)
	}
	var got []domain.Event
	send := func(_ context.Context, id int64, payload []byte) error {
		var ev domain.Event
		if err := json.Unmarshal(payload, &ev); err != nil || id != iss.ID {
			t.Errorf("payload %s id %d: %v", payload, id, err)
		}
		got = append(got, ev)
		return nil
	}
	if n, err := s.RelayOutbox(ctx, 10, send); n != 2 || err != nil {
		t.Fatalf("relay: %d %v", n, err)
	}
	if len(got) != 2 || got[0].Type != domain.KindNew || got[1].Type != domain.KindRegression || got[0].ServiceName != "api" || got[0].TraceID != "t1" || !got[0].Timestamp.Equal(t0) {
		t.Fatalf("events %+v", got)
	}
	if n, _ := s.RelayOutbox(ctx, 10, send); n != 0 {
		t.Fatalf("sent twice: %d", n)
	}
	if n, err := s.PruneOutbox(ctx, time.Now().Add(time.Hour)); n != 2 || err != nil {
		t.Fatalf("prune: %d %v", n, err)
	}
}

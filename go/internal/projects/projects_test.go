package projects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/pgtest"
)

func TestCleanName(t *testing.T) {
	if n, err := CleanName("  Web  "); err != nil || n != "Web" {
		t.Fatalf("%q %v", n, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("é", 81)} {
		if _, err := CleanName(bad); !errors.Is(err, ErrBadName) {
			t.Fatalf("%q accepted", bad)
		}
	}
	if _, err := CleanName(strings.Repeat("é", 80)); err != nil {
		t.Fatal("80 runes rejected")
	}
}

type purger struct{ ids []int64 }

func (p *purger) PurgeProject(_ context.Context, id int64) error {
	p.ids = append(p.ids, id)
	return nil
}

func TestStore(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := &Store{DB: db}
	alice := pgtest.User(t, db, "alice@example.com")
	org := pgtest.Org(t, db, "Acme", alice)

	p, k, err := s.Create(ctx, alice, org, " Web ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Web" || p.OrgID != org || p.Role != "owner" || k.ProjectID != p.ID || k.PublicKey == "" {
		t.Fatalf("project %+v key %+v", p, k)
	}
	if _, _, err := s.Create(ctx, alice, org, ""); !errors.Is(err, ErrBadName) {
		t.Fatalf("empty name: %v", err)
	}

	// A user without an organization gets one.
	bob := pgtest.User(t, db, "bob@example.com")
	bp, _, err := s.Create(ctx, bob, 0, "Mobile")
	if err != nil || bp.OrgID == 0 || bp.OrgID == org {
		t.Fatalf("auto org: %+v %v", bp, err)
	}
	var orgName string
	_ = db.QueryRow(ctx, `SELECT name FROM organizations WHERE id = $1`, bp.OrgID).Scan(&orgName)
	if orgName != "bob@example.com's organization" {
		t.Fatalf("org name %q", orgName)
	}
	// With orgID 0 an existing member uses their first organization.
	ap, _, err := s.Create(ctx, alice, 0, "API")
	if err != nil || ap.OrgID != org {
		t.Fatalf("default org: %+v %v", ap, err)
	}

	list, err := s.List(ctx, alice)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if n, err := s.Rename(ctx, p.ID, " Website "); err != nil || n != "Website" {
		t.Fatalf("rename %q %v", n, err)
	}
	if _, err := s.Rename(ctx, 999999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename missing: %v", err)
	}
	if _, err := s.Rename(ctx, p.ID, ""); !errors.Is(err, ErrBadName) {
		t.Fatalf("rename empty: %v", err)
	}

	keys := &purger{}
	o := &Origins{DB: db, Keys: keys}
	if got, err := o.AllowedOrigins(ctx, p.ID); err != nil || len(got) != 0 || got == nil {
		t.Fatalf("origins = %#v, %v", got, err)
	}
	if got, err := o.SetAllowedOrigins(ctx, p.ID, []string{"https://a.example"}); err != nil || len(got) != 1 {
		t.Fatalf("set = %v, %v", got, err)
	}
	if got, _ := o.AllowedOrigins(ctx, p.ID); len(got) != 1 || got[0] != "https://a.example" {
		t.Fatalf("origins = %v", got)
	}
	if len(keys.ids) != 1 || keys.ids[0] != p.ID {
		t.Fatalf("purged %v", keys.ids)
	}
	if got, err := o.SetAllowedOrigins(ctx, p.ID, nil); err != nil || got == nil {
		t.Fatalf("clear = %#v, %v", got, err)
	}
	if _, err := o.SetAllowedOrigins(ctx, 999999, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("set missing: %v", err)
	}
	if _, err := o.AllowedOrigins(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
}

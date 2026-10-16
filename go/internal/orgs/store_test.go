package orgs

import (
	"context"
	"errors"
	"testing"

	"github.com/Mutiur03/pulse/internal/pgtest"
)

func TestTokenHash(t *testing.T) {
	tok, h, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 || string(Hash(tok)) != string(h) {
		t.Fatalf("token %q hash mismatch", tok)
	}
	if tok2, _, _ := NewToken(); tok2 == tok {
		t.Fatal("tokens repeat")
	}
}

func TestMembersAndRoles(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := &Store{DB: db}
	alice := pgtest.User(t, db, "alice@example.com")
	bob := pgtest.User(t, db, "bob@example.com")
	org := pgtest.Org(t, db, "Acme", alice)

	if err := s.RemoveMember(ctx, org, alice); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("remove last owner: %v", err)
	}
	if err := s.SetRole(ctx, org, alice, "member"); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("demote last owner: %v", err)
	}
	if err := s.SetRole(ctx, org, bob, "owner"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("role for non-member: %v", err)
	}
	if err := s.SetRole(ctx, org, alice, "admin"); err == nil {
		t.Fatal("bad role accepted")
	}
	if _, err := db.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, bob); err != nil {
		t.Fatal(err)
	}
	m, err := s.Members(ctx, org)
	if err != nil || len(m) != 2 || m[0].Email != "alice@example.com" || m[1].Role != "member" {
		t.Fatalf("members = %+v, %v", m, err)
	}
	if err := s.SetRole(ctx, org, bob, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRole(ctx, org, alice, "member"); err != nil {
		t.Fatalf("demote with another owner: %v", err)
	}
	if err := s.RemoveMember(ctx, org, alice); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(ctx, org, alice); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
}

func TestInvites(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := &Store{DB: db}
	alice := pgtest.User(t, db, "alice@example.com")
	carol := pgtest.User(t, db, "carol@example.com")
	org := pgtest.Org(t, db, "Acme", alice)

	if _, _, err := s.CreateInvite(ctx, org, "ALICE@example.com", "member", alice); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("invite member: %v", err)
	}
	first, tok1, err := s.CreateInvite(ctx, org, "carol@example.com", "member", alice)
	if err != nil {
		t.Fatal(err)
	}
	if !first.ExpiresAt.After(first.CreatedAt) {
		t.Fatalf("expiry %v", first)
	}
	_, tok2, err := s.CreateInvite(ctx, org, " carol@example.com ", "owner", alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, tok1); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("replaced invite still works: %v", err)
	}
	list, err := s.ListInvites(ctx, org)
	if err != nil || len(list) != 1 || list[0].Role != "owner" {
		t.Fatalf("invites = %+v, %v", list, err)
	}
	info, err := s.Lookup(ctx, tok2)
	if err != nil || info.OrgName != "Acme" || info.Inviter != "alice@example.com" {
		t.Fatalf("lookup = %+v, %v", info, err)
	}
	if _, err := s.Accept(ctx, tok2, alice, "alice@example.com"); !errors.Is(err, ErrWrongUser) {
		t.Fatalf("accept by other user: %v", err)
	}
	got, err := s.Accept(ctx, tok2, carol, "Carol@Example.com")
	if err != nil || got != org {
		t.Fatalf("accept = %d, %v", got, err)
	}
	if _, err := s.Accept(ctx, tok2, carol, "carol@example.com"); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("accept twice: %v", err)
	}
	var role string
	var verified bool
	_ = db.QueryRow(ctx, `SELECT m.role, u.email_verified_at IS NOT NULL FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.user_id = $2`, org, carol).Scan(&role, &verified)
	if role != "owner" || !verified {
		t.Fatalf("role %q verified %v", role, verified)
	}

	inv, _, err := s.CreateInvite(ctx, org, "dave@example.com", "member", alice)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeInvite(ctx, org, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeInvite(ctx, org, inv.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke twice: %v", err)
	}
}

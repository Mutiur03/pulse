package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/orgs"
	"github.com/Mutiur03/pulse/internal/pgtest"
)

func TestSignupLoginSessions(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := &Store{DB: db}

	tok, u, err := s.Signup(ctx, " Alice@Example.com ", "correct horse battery", "Alice", "ua", "")
	if err != nil || tok == "" || u.ID == 0 {
		t.Fatalf("signup: %v", err)
	}
	if _, _, err := s.Signup(ctx, "alice@example.com", "correct horse battery", "A2", "ua", ""); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate signup: %v", err)
	}
	if _, _, err := s.Signup(ctx, "bad", "x", "", "ua", ""); err == nil {
		t.Fatal("invalid signup accepted")
	}

	p, err := s.Principal(ctx, tok)
	if err != nil || p.User.ID != u.ID || len(p.Orgs) != 1 || p.Orgs[0].Role != "owner" {
		t.Fatalf("principal: %+v %v", p, err)
	}
	if p.OrgRole(p.Orgs[0].ID) != "owner" || p.OrgRole(999) != "" {
		t.Fatal("OrgRole")
	}
	for _, bad := range []string{"", strings.Repeat("x", 101), "unknown"} {
		if _, err := s.Principal(ctx, bad); !errors.Is(err, ErrNoSession) {
			t.Fatalf("principal(%q): %v", bad, err)
		}
	}

	if _, _, err := s.Login(ctx, "alice@example.com", "wrong password!", "ua"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := s.Login(ctx, "nobody@example.com", "whatever", "ua"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("unknown user: %v", err)
	}
	tok2, lu, err := s.Login(ctx, "ALICE@example.com", "correct horse battery", strings.Repeat("u", 300))
	if err != nil || lu.ID != u.ID {
		t.Fatalf("login: %v", err)
	}
	var ua string
	_ = db.QueryRow(ctx, `SELECT user_agent FROM user_sessions WHERE token_hash = $1`, tokenHash(tok2)).Scan(&ua)
	if len(ua) != 256 {
		t.Fatalf("UA string not clipped: %d", len(ua))
	}

	// Sessions idle for longer than Idle stop working; recent ones are touched.
	if _, err := db.Exec(ctx, `UPDATE user_sessions SET last_seen_at = now() - interval '2 hours' WHERE token_hash = $1`, tokenHash(tok2)); err != nil {
		t.Fatal(err)
	}
	short := &Store{DB: db, Idle: time.Hour}
	if _, err := short.Principal(ctx, tok2); !errors.Is(err, ErrNoSession) {
		t.Fatalf("idle session: %v", err)
	}
	if _, err := s.Principal(ctx, tok2); err != nil {
		t.Fatalf("default idle: %v", err)
	}
	var age time.Duration
	_ = db.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM now() - last_seen_at)::bigint * 1000000000 FROM user_sessions WHERE token_hash = $1`,
		tokenHash(tok2)).Scan(&age)
	if age > time.Minute {
		t.Fatalf("session not touched, age %v", age)
	}

	if err := s.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Principal(ctx, tok); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after logout: %v", err)
	}
	if n, err := s.LogoutAll(ctx, u.ID); err != nil || n != 1 {
		t.Fatalf("logout all: %d %v", n, err)
	}
}

func TestSignupWithInvite(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := &Store{DB: db}
	owner := pgtest.User(t, db, "owner@example.com")
	org := pgtest.Org(t, db, "Acme", owner)
	_, invite, err := (&orgs.Store{DB: db}).CreateInvite(ctx, org, "bob@example.com", "member", owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Signup(ctx, "eve@example.com", "correct horse battery", "Eve", "", invite); !errors.Is(err, orgs.ErrWrongUser) {
		t.Fatalf("wrong invitee: %v", err)
	}
	tok, u, err := s.Signup(ctx, "bob@example.com", "correct horse battery", "Bob", "", invite)
	if err != nil || !u.EmailVerified {
		t.Fatalf("invite signup: %+v %v", u, err)
	}
	p, err := s.Principal(ctx, tok)
	if err != nil || len(p.Orgs) != 1 || p.Orgs[0].ID != org || p.Orgs[0].Role != "member" || !p.User.EmailVerified {
		t.Fatalf("principal: %+v %v", p, err)
	}
}

func TestEmailTokens(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	s := &Store{DB: db}
	sess, u, err := s.Signup(ctx, "alice@example.com", "correct horse battery", "Alice", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, ok, err := s.RequestReset(ctx, "nobody@example.com"); ok || err != nil {
		t.Fatalf("reset for unknown: %v %v", ok, err)
	}
	r1, ru, ok, err := s.RequestReset(ctx, " ALICE@example.com")
	if !ok || err != nil || ru.ID != u.ID {
		t.Fatalf("reset: %v %v", ok, err)
	}
	r2, _, _, _ := s.RequestReset(ctx, "alice@example.com")

	if err := s.ResetPassword(ctx, r1, "short"); err == nil {
		t.Fatal("weak password accepted")
	}
	if err := s.ResetPassword(ctx, "", "another good password"); !errors.Is(err, ErrBadToken) {
		t.Fatalf("empty token: %v", err)
	}
	if err := s.ResetPassword(ctx, "nope", "another good password"); !errors.Is(err, ErrBadToken) {
		t.Fatalf("unknown token: %v", err)
	}
	if err := s.ResetPassword(ctx, r1, "another good password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Principal(ctx, sess); !errors.Is(err, ErrNoSession) {
		t.Fatalf("sessions survive reset: %v", err)
	}
	if err := s.ResetPassword(ctx, r2, "third good password"); !errors.Is(err, ErrBadToken) {
		t.Fatalf("other reset link still works: %v", err)
	}
	if _, _, err := s.Login(ctx, "alice@example.com", "another good password", ""); err != nil {
		t.Fatalf("login with new password: %v", err)
	}

	bob := pgtest.User(t, db, "bob@example.com")
	v, err := s.IssueVerify(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyEmail(ctx, ""); !errors.Is(err, ErrBadToken) {
		t.Fatalf("empty: %v", err)
	}
	if err := s.ResetPassword(ctx, v, "another good password"); !errors.Is(err, ErrBadToken) {
		t.Fatalf("verify token used as reset: %v", err)
	}
	if err := s.VerifyEmail(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyEmail(ctx, v); !errors.Is(err, ErrBadToken) {
		t.Fatalf("verify twice: %v", err)
	}
	var verified bool
	_ = db.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, bob).Scan(&verified)
	if !verified {
		t.Fatal("not verified")
	}
}

func TestClearCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearCookie(w, true)
	c := w.Result().Cookies()
	if len(c) != 1 || c[0].MaxAge >= 0 || !c[0].Secure || !c[0].HttpOnly {
		t.Fatalf("cookie %+v", c)
	}
}

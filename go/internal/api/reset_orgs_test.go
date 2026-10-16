package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/orgs"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type chanMail chan mail.Message

func (c chanMail) Send(_ context.Context, m mail.Message) error {
	c <- m
	return nil
}

func expectMail(t *testing.T, c chanMail) mail.Message {
	t.Helper()
	select {
	case m := <-c:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no email sent")
		return mail.Message{}
	}
}

func expectNoMail(t *testing.T, c chanMail) {
	t.Helper()
	select {
	case m := <-c:
		t.Fatalf("unexpected email to %s", m.To)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPasswordResetRequest(t *testing.T) {
	mr := miniredis.RunT(t)
	box := make(chanMail, 4)
	s := &Server{
		Accounts: &fakeAccounts{users: map[string]string{"a@example.com": "pw"}},
		Mail:     box, AppURL: "https://pulse.example.com/",
		Limits: AuthLimits{ResetEmail: &auth.Throttle{Redis: redis.NewClient(&redis.Options{Addr: mr.Addr()}), Max: 3, Window: time.Hour, Prefix: "reset:req"}},
	}
	rec := post(s, "/v1/auth/password-reset/request", `{"email":"a@example.com"}`, "1.1.1.1")
	if rec.Code != 204 {
		t.Fatalf("known email: %d", rec.Code)
	}
	m := expectMail(t, box)
	if m.To != "a@example.com" || !strings.Contains(m.Text, "https://pulse.example.com/reset-password?token=reset-token") {
		t.Errorf("mail: %+v", m)
	}
	// Unknown and malformed emails get the same answer and no email.
	for _, b := range []string{`{"email":"nobody@example.com"}`, `{"email":"junk"}`} {
		if rec := post(s, "/v1/auth/password-reset/request", b, "1.1.1.1"); rec.Code != 204 || rec.Body.Len() != 0 {
			t.Errorf("%s: %d %q", b, rec.Code, rec.Body)
		}
	}
	expectNoMail(t, box)
	post(s, "/v1/auth/password-reset/request", `{"email":"a@example.com"}`, "1.1.1.1")
	post(s, "/v1/auth/password-reset/request", `{"email":"a@example.com"}`, "1.1.1.1")
	rec = post(s, "/v1/auth/password-reset/request", `{"email":"a@example.com"}`, "1.1.1.1")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Errorf("fourth request should be throttled: %d", rec.Code)
	}
}

func TestPasswordResetConfirmAndVerify(t *testing.T) {
	s := &Server{Accounts: &fakeAccounts{users: map[string]string{}}}
	cases := []struct {
		path, body string
		want       int
	}{
		{"/v1/auth/password-reset/confirm", `{"token":"reset-token","password":"a new password"}`, 204},
		{"/v1/auth/password-reset/confirm", `{"token":"other","password":"a new password"}`, 400},
		{"/v1/auth/password-reset/confirm", `{"token":"reset-token","password":"short"}`, 400},
		{"/v1/auth/verify-email", `{"token":"verify-token"}`, 204},
		{"/v1/auth/verify-email", `{"token":"nope"}`, 400},
	}
	for _, c := range cases {
		rec := post(s, c.path, c.body, "1.1.1.1")
		if rec.Code != c.want {
			t.Errorf("%s %s: %d want %d", c.path, c.body, rec.Code, c.want)
		}
		if c.want == 204 && strings.Contains(c.path, "reset") && !strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=0") {
			t.Errorf("reset should clear the session cookie: %q", rec.Header().Get("Set-Cookie"))
		}
	}
}

func TestSignupSendsVerification(t *testing.T) {
	box := make(chanMail, 1)
	s := &Server{Accounts: &fakeAccounts{users: map[string]string{}}, Mail: box}
	if rec := post(s, "/v1/auth/signup", `{"email":"v@example.com","password":"long enough pw","name":"V"}`, "1.1.1.1"); rec.Code != 201 {
		t.Fatal(rec.Code)
	}
	if m := expectMail(t, box); !strings.Contains(m.Text, "http://localhost:3000/verify-email?token=verify-token") {
		t.Errorf("%q", m.Text)
	}
}

type fakeOrgs struct {
	removed []int64
	invited []string
	revoked []int64
}

func (f *fakeOrgs) Members(context.Context, int64) ([]orgs.Member, error) {
	return []orgs.Member{{UserID: 1, Email: "o@example.com", Role: "owner"}}, nil
}

func (f *fakeOrgs) RemoveMember(_ context.Context, _, user int64) error {
	switch user {
	case 1:
		return orgs.ErrLastOwner
	case 9:
		return orgs.ErrNotFound
	}
	f.removed = append(f.removed, user)
	return nil
}

func (f *fakeOrgs) CreateInvite(_ context.Context, org int64, email, role string, _ int64) (orgs.Invite, string, error) {
	if email == "o@example.com" {
		return orgs.Invite{}, "", orgs.ErrAlreadyMember
	}
	f.invited = append(f.invited, email+":"+role)
	return orgs.Invite{ID: 3, OrgID: org, Email: email, Role: role}, "invite-token", nil
}

func (f *fakeOrgs) ListInvites(context.Context, int64) ([]orgs.Invite, error) {
	return []orgs.Invite{{ID: 3, Email: "x@example.com", Role: "member"}}, nil
}

func (f *fakeOrgs) RevokeInvite(_ context.Context, _, id int64) error {
	if id != 3 {
		return orgs.ErrNotFound
	}
	f.revoked = append(f.revoked, id)
	return nil
}

func (f *fakeOrgs) Lookup(_ context.Context, token string) (orgs.InviteInfo, error) {
	if token != "invite-token" {
		return orgs.InviteInfo{}, orgs.ErrBadInvite
	}
	return orgs.InviteInfo{OrgName: "Acme", Email: "x@example.com", Role: "member", Inviter: "O"}, nil
}

func (f *fakeOrgs) Accept(_ context.Context, token string, _ int64, email string) (int64, error) {
	if token != "invite-token" {
		return 0, orgs.ErrBadInvite
	}
	if email != "x@example.com" {
		return 0, orgs.ErrWrongUser
	}
	return 7, nil
}

func TestOrgMembersAndInvites(t *testing.T) {
	fo := &fakeOrgs{}
	box := make(chanMail, 2)
	s := &Server{Orgs: fo, Mail: box}
	owner := &auth.Principal{User: auth.User{ID: 1, Name: "O", Email: "o@example.com"},
		Orgs: []auth.Org{{ID: 7, Name: "Acme", Role: "owner"}, {ID: 8, Name: "Other", Role: "member"}}}
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/v1/orgs/7/members", "", 200},
		{"GET", "/v1/orgs/8/members", "", 200},
		{"GET", "/v1/orgs/9/members", "", 404},
		{"POST", "/v1/orgs/7/members/2/remove", "", 204},
		{"POST", "/v1/orgs/7/members/1/remove", "", 409},
		{"POST", "/v1/orgs/7/members/9/remove", "", 404},
		{"POST", "/v1/orgs/8/members/2/remove", "", 403},
		{"GET", "/v1/orgs/7/invites", "", 200},
		{"GET", "/v1/orgs/8/invites", "", 403},
		{"POST", "/v1/orgs/7/invites", `{"email":"x@example.com","role":"member"}`, 201},
		{"POST", "/v1/orgs/7/invites", `{"email":"o@example.com","role":"member"}`, 409},
		{"POST", "/v1/orgs/7/invites", `{"email":"bad","role":"member"}`, 400},
		{"POST", "/v1/orgs/7/invites", `{"email":"y@example.com","role":"admin"}`, 400},
		{"POST", "/v1/orgs/8/invites", `{"email":"y@example.com","role":"member"}`, 403},
		{"POST", "/v1/orgs/7/invites/3/revoke", "", 204},
		{"POST", "/v1/orgs/7/invites/4/revoke", "", 404},
		{"POST", "/v1/projects", `{"name":"P","org_id":8}`, 403},
		{"POST", "/v1/projects", `{"name":"P","org_id":9}`, 404},
	}
	s.Projects = &fakeProjects{renamed: map[int64]string{}}
	for _, c := range cases {
		if rec := call(t, s, owner, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %s: %d want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body)
		}
	}
	if len(fo.removed) != 1 || len(fo.invited) != 1 || fo.invited[0] != "x@example.com:member" || len(fo.revoked) != 1 {
		t.Errorf("%+v", fo)
	}
	m := expectMail(t, box)
	if m.To != "x@example.com" || !strings.Contains(m.Subject, "Acme") || !strings.Contains(m.Text, "/invite?token=invite-token") {
		t.Errorf("invite mail: %+v", m)
	}
}

func TestInviteLookupAndAccept(t *testing.T) {
	s := &Server{Orgs: &fakeOrgs{}, Accounts: &fakeAccounts{users: map[string]string{}}}
	if rec := post(s, "/v1/invites/lookup", `{"token":"invite-token"}`, "1.1.1.1"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"org_name":"Acme"`) {
		t.Errorf("lookup: %d %s", rec.Code, rec.Body)
	}
	if rec := post(s, "/v1/invites/lookup", `{"token":"bad"}`, "1.1.1.1"); rec.Code != 404 {
		t.Errorf("bad lookup: %d", rec.Code)
	}
	right := &auth.Principal{User: auth.User{ID: 2, Email: "x@example.com"}}
	wrong := &auth.Principal{User: auth.User{ID: 3, Email: "z@example.com"}}
	if rec := call(t, s, wrong, "POST", "/v1/invites/accept", `{"token":"invite-token"}`); rec.Code != 403 {
		t.Errorf("wrong user: %d", rec.Code)
	}
	if rec := call(t, s, right, "POST", "/v1/invites/accept", `{"token":"bad"}`); rec.Code != 404 {
		t.Errorf("bad token: %d", rec.Code)
	}
	if rec := call(t, s, nil, "POST", "/v1/invites/accept", `{"token":"invite-token"}`); rec.Code != 401 {
		t.Errorf("anonymous: %d", rec.Code)
	}
}

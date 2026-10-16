package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/legacy"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type fakeAccounts struct {
	users     map[string]string // email -> password
	logins    int
	loggedOut string
}

func (f *fakeAccounts) Login(_ context.Context, email, pw, _ string) (string, auth.User, error) {
	f.logins++
	if p, ok := f.users[email]; ok && p == pw {
		return "tok-" + email, auth.User{Email: email}, nil
	}
	return "", auth.User{}, auth.ErrBadCredentials
}

func (f *fakeAccounts) Signup(_ context.Context, email, pw, name, _, _ string) (string, auth.User, error) {
	email, _, err := auth.NormalizeSignup(email, pw, name)
	if err != nil {
		return "", auth.User{}, err
	}
	if _, ok := f.users[email]; ok {
		return "", auth.User{}, auth.ErrEmailTaken
	}
	f.users[email] = pw
	return "tok-" + email, auth.User{Email: email}, nil
}

func (f *fakeAccounts) Principal(_ context.Context, tok string) (*auth.Principal, error) {
	return &auth.Principal{User: auth.User{ID: 1, Email: strings.TrimPrefix(tok, "tok-")}}, nil
}

func (f *fakeAccounts) Logout(_ context.Context, t string) error { f.loggedOut = t; return nil }

func (f *fakeAccounts) RequestReset(_ context.Context, email string) (string, auth.User, bool, error) {
	if _, ok := f.users[email]; !ok {
		return "", auth.User{}, false, nil
	}
	return "reset-token", auth.User{ID: 1, Email: email, Name: "A"}, true, nil
}

func (f *fakeAccounts) ResetPassword(_ context.Context, token, pw string) error {
	if err := auth.CheckPassword(pw); err != nil {
		return err
	}
	if token != "reset-token" {
		return auth.ErrBadToken
	}
	return nil
}

func (f *fakeAccounts) IssueVerify(context.Context, int64) (string, error) {
	return "verify-token", nil
}

func (f *fakeAccounts) VerifyEmail(_ context.Context, token string) error {
	if token != "verify-token" {
		return auth.ErrBadToken
	}
	return nil
}

func post(s *Server, path, body, ip string) *httptest.ResponseRecorder {
	h := legacy.HandlerFromMux(s, chi.NewRouter())
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSignup(t *testing.T) {
	s := &Server{Accounts: &fakeAccounts{users: map[string]string{"taken@example.com": "x"}}}
	cases := []struct {
		body string
		want int
	}{
		{`{"email":"new@example.com","password":"long enough pw","name":"Ann"}`, 201},
		{`{"email":"new@example.com","password":"long enough pw","name":"Ann"}`, 409},
		{`{"email":"Taken@example.com ","password":"long enough pw","name":"Ann"}`, 201}, // fake is case-sensitive; real store is not
		{`{"email":"not-an-email","password":"long enough pw","name":"Ann"}`, 400},
		{`{"email":"a@b","password":"long enough pw","name":"Ann"}`, 400},
		{`{"email":"Ann <ann@example.com>","password":"long enough pw","name":"Ann"}`, 400},
		{`{"email":"ann@example.com","password":"short","name":"Ann"}`, 400},
		{`{"email":"ann@example.com","password":"long enough pw","name":"  "}`, 400},
		{`{"email":`, 400},
	}
	for _, c := range cases {
		rec := post(s, "/v1/auth/signup", c.body, "10.0.0.1")
		if rec.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.body, rec.Code, c.want, rec.Body)
		}
		if c.want == 201 && !strings.Contains(rec.Header().Get("Set-Cookie"), auth.CookieName+"=tok-") {
			t.Errorf("signup must start a session: %q", rec.Header().Get("Set-Cookie"))
		}
	}
}

func TestLoginThrottle(t *testing.T) {
	mr := miniredis.RunT(t)
	acc := &fakeAccounts{users: map[string]string{"a@example.com": "right password"}}
	s := &Server{Accounts: acc, Limits: AuthLimits{LoginFail: &auth.Throttle{
		Redis: redis.NewClient(&redis.Options{Addr: mr.Addr()}), Max: 5, Window: 15 * time.Minute}}}
	bad := `{"email":"a@example.com","password":"wrong"}`
	good := `{"email":"a@example.com","password":"right password"}`
	for i := 0; i < 5; i++ {
		if rec := post(s, "/v1/auth/login", bad, "1.2.3.4"); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := post(s, "/v1/auth/login", good, "1.2.3.4")
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "900" {
		t.Fatalf("sixth attempt must be throttled even with the right password: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if acc.logins != 5 {
		t.Errorf("throttled attempts must not check the password: %d", acc.logins)
	}
	// Another address is not affected.
	if rec := post(s, "/v1/auth/login", good, "5.6.7.8"); rec.Code != 200 {
		t.Errorf("other ip: %d", rec.Code)
	}
	mr.FastForward(15*time.Minute + time.Second)
	if rec := post(s, "/v1/auth/login", good, "1.2.3.4"); rec.Code != 200 {
		t.Fatalf("after the window: %d", rec.Code)
	}
	// Success resets the counter.
	for i := 0; i < 4; i++ {
		post(s, "/v1/auth/login", bad, "1.2.3.4")
	}
	post(s, "/v1/auth/login", good, "1.2.3.4")
	for i := 0; i < 4; i++ {
		post(s, "/v1/auth/login", bad, "1.2.3.4")
	}
	if rec := post(s, "/v1/auth/login", good, "1.2.3.4"); rec.Code != 200 {
		t.Errorf("counter should reset on success: %d", rec.Code)
	}
}

func TestLoginThrottleRedisDown(t *testing.T) {
	acc := &fakeAccounts{users: map[string]string{"a@example.com": "right password"}}
	s := &Server{Accounts: acc, Limits: AuthLimits{LoginFail: &auth.Throttle{
		Redis: redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1}), Max: 1, Window: time.Minute}}}
	post(s, "/v1/auth/login", `{"email":"a@example.com","password":"x"}`, "1.1.1.1")
	if rec := post(s, "/v1/auth/login", `{"email":"a@example.com","password":"right password"}`, "1.1.1.1"); rec.Code != 200 {
		t.Errorf("must fail open: %d", rec.Code)
	}
}

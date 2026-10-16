package api

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type memAudit struct{ entries []audit.Entry }

func (m *memAudit) Record(_ context.Context, e audit.Entry) error {
	m.entries = append(m.entries, e)
	return nil
}

func (m *memAudit) List(context.Context, int64, int, int64) ([]audit.Entry, error) {
	return m.entries, nil
}

func (m *memAudit) actions() string {
	var out []string
	for _, e := range m.entries {
		out = append(out, e.Action)
	}
	return strings.Join(out, ",")
}

func loginFrom(s *Server, body, peer, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = peer + ":4000"
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	s.Login(rec, req)
	return rec
}

func throttle(mr *miniredis.Miniredis, prefix string, max int, window time.Duration) *auth.Throttle {
	return &auth.Throttle{Redis: redis.NewClient(&redis.Options{Addr: mr.Addr()}), Max: max, Window: window, Prefix: prefix}
}

func TestLoginLimitsUseForwardedClientIP(t *testing.T) {
	mr := miniredis.RunT(t)
	acc := &fakeAccounts{users: map[string]string{"a@example.com": "right password"}}
	proxies, _ := clientip.Parse("10.0.0.0/8")
	al := &memAudit{}
	s := &Server{Accounts: acc, ClientIP: proxies, Audit: al, Limits: AuthLimits{
		LoginFail:  throttle(mr, "login:fail", 3, 15*time.Minute),
		LoginEmail: throttle(mr, "login:email", 5, 15*time.Minute),
		LoginIP:    throttle(mr, "login:ip", 8, 15*time.Minute),
	}}
	bad := `{"email":"a@example.com","password":"wrong"}`
	good := `{"email":"a@example.com","password":"right password"}`

	// Three failures from one client behind the proxy lock that email+IP.
	for i := 0; i < 3; i++ {
		loginFrom(s, bad, "10.0.0.2", "198.51.100.1")
	}
	if rec := loginFrom(s, good, "10.0.0.2", "198.51.100.1"); rec.Code != 429 {
		t.Fatalf("email+ip lock: %d", rec.Code)
	}
	// Another client of the same proxy is not affected.
	if rec := loginFrom(s, good, "10.0.0.2", "198.51.100.2"); rec.Code != 200 {
		t.Fatalf("other client: %d", rec.Code)
	}
	// A client that is not behind a trusted proxy cannot pick its address.
	if rec := loginFrom(s, bad, "203.0.113.5", "198.51.100.77"); rec.Code != 401 {
		t.Fatalf("direct: %d", rec.Code)
	}
	if mr.Exists("login:fail:a@example.com:198.51.100.77") || !mr.Exists("login:fail:a@example.com:203.0.113.5") {
		t.Fatalf("spoofed header was trusted: %v", mr.Keys())
	}
	// Failures from many addresses lock the email itself (5 failures so far
	// after the success reset: 0 + 1 direct; add 4 from new addresses).
	for i := 0; i < 4; i++ {
		loginFrom(s, bad, "10.0.0.2", "198.51.100."+string(rune('3'+i)))
	}
	if rec := loginFrom(s, good, "10.0.0.2", "198.51.100.200"); rec.Code != 429 {
		t.Fatalf("email lock across addresses: %d", rec.Code)
	}
	if !strings.Contains(al.actions(), "login.failure") || !strings.Contains(al.actions(), "login.success") || !strings.Contains(al.actions(), "login.blocked") {
		t.Fatalf("audit: %s", al.actions())
	}
	if al.entries[0].IP != "198.51.100.1" {
		t.Fatalf("audit ip: %+v", al.entries[0])
	}
}

func TestPerIPAttemptLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	acc := &fakeAccounts{users: map[string]string{"a@example.com": "pw"}}
	s := &Server{Accounts: acc, Limits: AuthLimits{
		LoginIP: throttle(mr, "login:ip", 3, time.Minute), SignupIP: throttle(mr, "signup:ip", 1, time.Hour),
		ResetIP: throttle(mr, "reset:ip", 2, time.Hour),
	}}
	for i := 0; i < 3; i++ {
		if rec := loginFrom(s, `{"email":"x`+string(rune('a'+i))+`@example.com","password":"x"}`, "192.0.2.1", ""); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := loginFrom(s, `{"email":"a@example.com","password":"pw"}`, "192.0.2.1", "")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("per-ip limit: %d", rec.Code)
	}
	if rec := loginFrom(s, `{"email":"a@example.com","password":"pw"}`, "192.0.2.2", ""); rec.Code != 200 {
		t.Fatalf("other ip: %d", rec.Code)
	}
	signup := `{"email":"new@example.com","password":"long enough password","name":"N"}`
	post(s, "/v1/auth/signup", signup, "192.0.2.9")
	if rec := post(s, "/v1/auth/signup", strings.Replace(signup, "new@", "new2@", 1), "192.0.2.9"); rec.Code != 429 {
		t.Fatalf("signup limit: %d", rec.Code)
	}
	for i := 0; i < 2; i++ {
		post(s, "/v1/auth/password-reset/request", `{"email":"r`+string(rune('a'+i))+`@example.com"}`, "192.0.2.9")
	}
	if rec := post(s, "/v1/auth/password-reset/request", `{"email":"rz@example.com"}`, "192.0.2.9"); rec.Code != 429 {
		t.Fatalf("reset limit: %d", rec.Code)
	}
}

type fakeRevoker struct{ user int64 }

func (f *fakeRevoker) LogoutAll(_ context.Context, u int64) (int64, error) { f.user = u; return 3, nil }

func TestLogoutAllAndRotation(t *testing.T) {
	acc := &fakeAccounts{users: map[string]string{"a@example.com": "pw"}}
	rv := &fakeRevoker{}
	al := &memAudit{}
	s := &Server{Accounts: acc, Sessions: rv, Audit: al}
	me := &auth.Principal{User: auth.User{ID: 7, Email: "a@example.com"}}
	rec := call(t, s, me, "POST", "/v1/auth/logout-all", "")
	if rec.Code != 200 || rv.user != 7 || !strings.Contains(rec.Body.String(), `"sessions_ended":3`) ||
		!strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout all: %d %s %v", rec.Code, rec.Body, rec.Header())
	}
	if al.actions() != "logout.all" {
		t.Fatal(al.actions())
	}

	// Logging in with an old session cookie ends that session.
	req := httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"pw"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "old-session"})
	rec = httptest.NewRecorder()
	s.Login(rec, req)
	if rec.Code != 200 || acc.loggedOut != "old-session" {
		t.Fatalf("rotation: %d logged out %q", rec.Code, acc.loggedOut)
	}
	c := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(c, want) {
			t.Fatalf("cookie %q lacks %s", c, want)
		}
	}
}

func TestRedactURLAndHeaders(t *testing.T) {
	u, _ := url.Parse("/v1/x?token=abc&key=pk_1&limit=5&upload_secret=s")
	got := RedactURL(u)
	if strings.Contains(got, "abc") || strings.Contains(got, "pk_1") || strings.Contains(got, "=s&") || !strings.Contains(got, "limit=5") {
		t.Fatal(got)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	h := SecurityHeaders(31536000)(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/invites/lookup?token=secret-token", nil))
	if strings.Contains(buf.String(), "secret-token") || !strings.Contains(buf.String(), "token=REDACTED") {
		t.Fatalf("log: %s", buf.String())
	}
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains"} {
		if rec.Header().Get(k) != v {
			t.Fatalf("%s = %q", k, rec.Header().Get(k))
		}
	}
}

func TestAuditAndOriginsRBAC(t *testing.T) {
	al := &memAudit{entries: []audit.Entry{{ID: 1, Action: "key.create", ActorEmail: "o@example.com"}}}
	s := &Server{AuditLog: al}
	owner := &auth.Principal{Orgs: []auth.Org{{ID: 1, Role: "owner"}}, Projects: []auth.Project{{ID: 1, Role: "owner", OrgID: 1}}}
	member := &auth.Principal{Orgs: []auth.Org{{ID: 1, Role: "member"}}, Projects: []auth.Project{{ID: 1, Role: "member", OrgID: 1}}}
	if rec := call(t, s, member, "GET", "/v1/orgs/1/audit-log", ""); rec.Code != 403 {
		t.Fatalf("member reads audit log: %d", rec.Code)
	}
	if rec := call(t, s, owner, "GET", "/v1/orgs/1/audit-log", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "key.create") {
		t.Fatalf("owner audit log: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, s, member, "POST", "/v1/projects/1/allowed-origins", `{"origins":["https://a.example"]}`); rec.Code != 403 {
		t.Fatalf("member sets origins: %d", rec.Code)
	}
	if rec := call(t, s, owner, "POST", "/v1/projects/1/allowed-origins", `{"origins":["not an origin"]}`); rec.Code != 400 {
		t.Fatalf("bad origin: %d", rec.Code)
	}
	if rec := call(t, s, member, "POST", "/v1/orgs/1/members/2/role", `{"role":"owner"}`); rec.Code != 403 {
		t.Fatalf("member changes role: %d", rec.Code)
	}
	if rec := call(t, s, owner, "POST", "/v1/orgs/1/members/2/role", `{"role":"admin"}`); rec.Code != 400 {
		t.Fatalf("bad role: %d", rec.Code)
	}
}

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type fakeSessions map[string]*Principal

func (f fakeSessions) Principal(_ context.Context, tok string) (*Principal, error) {
	if p, ok := f[tok]; ok {
		return p, nil
	}
	return nil, ErrNoSession
}

func TestMiddleware(t *testing.T) {
	me := &Principal{User: User{ID: 1}, Projects: []Project{{ID: 1}}}
	var seen *Principal
	h := Middleware(fakeSessions{"good": me})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = FromContext(r.Context())
		w.WriteHeader(200)
	}))
	cases := []struct {
		method, path, cookie, ct string
		want                     int
	}{
		{"GET", "/health", "", "", 200},
		{"POST", "/v1/auth/login", "", "application/json", 200},
		{"POST", "/v1/auth/login", "", "application/x-www-form-urlencoded", 415},
		{"GET", "/v1/issues", "", "", 401},
		{"GET", "/v1/issues", "forged", "", 401},
		{"GET", "/v1/issues", "good", "", 200},
		{"PATCH", "/v1/issues/1", "good", "text/plain", 415},
		{"PATCH", "/v1/issues/1", "good", "application/json", 200},
	}
	for _, c := range cases {
		seen = nil
		req := httptest.NewRequest(c.method, c.path, strings.NewReader("{}"))
		if c.cookie != "" {
			req.AddCookie(&http.Cookie{Name: CookieName, Value: c.cookie})
		}
		if c.ct != "" {
			req.Header.Set("Content-Type", c.ct)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s cookie=%q ct=%q: got %d want %d", c.method, c.path, c.cookie, c.ct, rec.Code, c.want)
		}
		if c.cookie == "good" && c.want == 200 && seen != me {
			t.Errorf("%s %s: principal not set", c.method, c.path)
		}
	}
}

func TestPrincipalCan(t *testing.T) {
	p := &Principal{Projects: []Project{{ID: 1}, {ID: 4}}}
	if !p.Can(4) || p.Can(2) {
		t.Error("membership check")
	}
}

func TestCookieFlags(t *testing.T) {
	rec := httptest.NewRecorder()
	SetCookie(rec, "tok", true)
	c := rec.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie flags: %+v", c)
	}
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("pulse-demo")
	if err != nil {
		t.Fatal(err)
	}
	if cost, _ := bcrypt.Cost([]byte(h)); cost != BcryptCost {
		t.Errorf("cost %d", cost)
	}
	if bcrypt.CompareHashAndPassword([]byte(h), []byte("pulse-demo")) != nil {
		t.Error("hash does not verify")
	}
	if len(tokenHash("a")) != 32 {
		t.Error("token hash should be sha256")
	}
}

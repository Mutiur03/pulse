package domain

import (
	"strings"
	"testing"

	"github.com/Mutiur03/pulse/internal/auth"
)

func TestRules(t *testing.T) {
	if !ValidRole("owner") || !ValidRole("member") || ValidRole("admin") {
		t.Error("roles")
	}
	if ValidToken("") || ValidToken(strings.Repeat("x", 101)) || !ValidToken("abc") {
		t.Error("tokens")
	}
	if Truncate("abcdef", 3) != "abc" || Truncate("ab", 3) != "ab" {
		t.Error("truncate")
	}
	m := InviteMail(auth.User{Name: "Ann", Email: "ann@example.com"}, "x@example.com", "Acme", "member", "http://l")
	if m.To != "x@example.com" || !strings.Contains(m.Subject, "Acme") || !strings.Contains(m.Text, "http://l") {
		t.Errorf("%+v", m)
	}
}

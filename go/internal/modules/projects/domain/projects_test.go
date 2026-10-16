package domain

import (
	"testing"

	"github.com/Mutiur03/pulse/internal/auth"
)

func TestDefaultOrg(t *testing.T) {
	for _, c := range []struct {
		orgs []auth.Org
		want int64
		ok   bool
	}{
		{nil, 0, true},
		{[]auth.Org{{ID: 1, Role: "owner"}}, 0, true},
		{[]auth.Org{{ID: 1, Role: "member"}, {ID: 2, Role: "owner"}}, 2, true},
		{[]auth.Org{{ID: 1, Role: "member"}}, 0, false},
	} {
		if got, ok := DefaultOrg(&auth.Principal{Orgs: c.orgs}); got != c.want || ok != c.ok {
			t.Errorf("%+v: %d %v", c.orgs, got, ok)
		}
	}
	if KeyLabel("pk_0123456789abcdef") != "pk_012345678..." || KeyLabel("pk_1") != "pk_1" {
		t.Error("label")
	}
}

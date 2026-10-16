package domain

import (
	"strings"
	"testing"
)

func TestDeletionInput(t *testing.T) {
	long := strings.Repeat("x", 501)
	if id, r, err := DeletionInput(" u1 ", &long); err != ErrReasonLong || id != "" || r != "" {
		t.Error("reason")
	}
	if _, _, err := DeletionInput(" ", nil); err != ErrBadDistinct {
		t.Error("empty id")
	}
	why := " gdpr "
	if id, r, err := DeletionInput(" u1 ", &why); err != nil || id != "u1" || r != "gdpr" {
		t.Errorf("%q %q %v", id, r, err)
	}
}

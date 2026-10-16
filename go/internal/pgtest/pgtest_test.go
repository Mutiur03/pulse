package pgtest

import "testing"

func TestNew(t *testing.T) {
	db := New(t)
	u := User(t, db, "a@example.com")
	Org(t, db, "o", u)
}

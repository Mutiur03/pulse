package ch

import "testing"

func TestFingerprintGroupsByTopFrame(t *testing.T) {
	a := Fingerprint("TypeError", "x is undefined (id 1)", "TypeError: x\n    at pay (app.js:10:5)\n    at main")
	b := Fingerprint("TypeError", "x is undefined (id 2)", "TypeError: x\n    at pay (app.js:10:5)")
	c := Fingerprint("TypeError", "x", "TypeError: x\n    at other (app.js:99:1)")
	if a != b {
		t.Error("same top frame should share a fingerprint")
	}
	if a == c {
		t.Error("different top frames should differ")
	}
	if Fingerprint("E", "m1", "") == Fingerprint("E", "m2", "") {
		t.Error("without a stack, message should decide")
	}
}

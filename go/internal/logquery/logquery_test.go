package logquery

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	q, err := Parse(`timeout level:warn+ service:checkout -"health check" order_id:42 trace:ABC msg:"a b"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Term{
		{Value: "timeout"},
		{Field: "level", Value: "WARN", OrWorse: true},
		{Field: "service", Value: "checkout"},
		{Value: "health check", Not: true},
		{Field: "order_id", Value: "42"},
		{Field: "trace", Value: "ABC"},
		{Field: "msg", Value: "a b"},
	}
	if !reflect.DeepEqual(q.Terms, want) {
		t.Fatalf("terms = %+v", q.Terms)
	}
	if got := q.String(); got != `timeout level:warn+ service:checkout -"health check" order_id:42 trace:ABC msg:"a b"` {
		t.Fatalf("string = %s", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{`level:loud`, `"open`, `service:`} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%q: want error", s)
		}
	}
	q, err := Parse("  ")
	if err != nil || len(q.Terms) != 0 {
		t.Fatalf("empty: %v %v", q, err)
	}
	if w, args := q.Where(); w != "1" || args != nil {
		t.Fatalf("empty where = %s %v", w, args)
	}
}

func TestWhere(t *testing.T) {
	q, _ := Parse(`declined level:error+ -service:web order_id:7 trace:AB`)
	w, args := q.Where()
	wantW := "positionCaseInsensitive(body, ?) > 0 AND severity IN (?, ?) AND NOT (service_name = ?) AND JSONExtractRaw(attributes, ?) IN (?, ?) AND trace_id = ?"
	if w != wantW {
		t.Fatalf("where = %s", w)
	}
	wantA := []interface{}{"declined", "ERROR", "FATAL", "web", "order_id", "7", `"7"`, "ab"}
	if !reflect.DeepEqual(args, wantA) {
		t.Fatalf("args = %v", args)
	}
	q, _ = Parse("level:warning")
	if w, args := q.Where(); w != "severity IN ('WARN', 'WARNING')" || len(args) != 0 {
		t.Fatalf("warn = %s %v", w, args)
	}
}

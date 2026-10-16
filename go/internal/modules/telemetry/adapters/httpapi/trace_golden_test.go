package httpapi

import (
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/golden"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// TestBuildTraceGolden pins the full output for a trace that exercises every
// branch: attributes, span events, HTTP status, children ordering, orphans,
// a parent cycle, duplicates, self-parents, loose errors and events.
func TestBuildTraceGolden(t *testing.T) {
	root := span("a", "", "GET /checkout", "web", "webjs", "", 0, 900)
	root.Attributes = map[string]string{"http.method": "GET"}
	root.SpanEvents = `[{"name":"click","timestamp":"2026-10-15T12:00:00.1Z"}]`
	api := span("b", "a", "POST /orders", "api", "go", "", 50, 600)
	api.HTTPStatus = 500
	api.StatusCode = "error"
	api.StatusMessage = "boom"
	api.HTTPRoute = "/orders"
	db1 := span("c", "b", "INSERT", "api", "go", "postgresql", 100, 20)
	db1.DBStatement = "INSERT INTO orders"
	db2 := span("d", "b", "SELECT", "api", "go", "postgresql", 100, 10)
	emptyEvents := span("e", "b", "cache", "api", "go", "", 300, 5)
	emptyEvents.SpanEvents = "[]"
	orphan := span("f", "missing", "late", "worker", "python", "", -20, 30)
	cyc1 := span("g", "h", "loop1", "", "go", "", 400, 1)
	cyc2 := span("h", "g", "loop2", "", "go", "", 401, 1)
	self := span("i", "i", "self", "api", "go", "", 500, 1)
	dup := span("b", "a", "duplicate", "api", "go", "", 999, 1)
	spans := []ch.SpanRecord{root, api, db1, db2, emptyEvents, orphan, cyc1, cyc2, self, dup}

	errs := []ch.ErrorRecord{
		{Timestamp: t0.Add(60 * time.Millisecond), IssueID: 7, ExceptionType: "Error", Message: "boom", SpanID: "b", TraceID: "t1"},
		{Timestamp: t0.Add(-50 * time.Millisecond), IssueID: 8, ExceptionType: "TypeError", Message: "loose", SpanID: "zz", TraceID: "t1", Stack: "at x"},
	}
	events := []ch.EventRecord{
		{Event: "checkout", Timestamp: t0, SpanID: "a", Properties: `{"cart":3}`},
		{Event: "pageview", Timestamp: t0, SpanID: ""},
		{Event: "stray", Timestamp: t0, SpanID: "nope", Properties: "not json"},
	}
	golden.JSON(t, "build_trace", BuildTrace("t1", spans, errs, events))
	golden.JSON(t, "build_trace_empty", BuildTrace("t2", nil, nil, nil))
	golden.JSON(t, "build_trace_errors_only", BuildTrace("t3", nil, errs, events))
}

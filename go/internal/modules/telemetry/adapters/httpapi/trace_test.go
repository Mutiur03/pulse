package httpapi

import (
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func span(id, parent, name, service, platform, db string, offsetMs, durMs int) ch.SpanRecord {
	return ch.SpanRecord{
		TraceID: "t1", SpanID: id, ParentSpanID: parent, Name: name, ServiceName: service, Platform: platform,
		DBSystem: db, Timestamp: t0.Add(time.Duration(offsetMs) * time.Millisecond),
		DurationNs: uint64(durMs) * uint64(time.Millisecond),
	}
}

func TestBuildTraceBrowserBackendDB(t *testing.T) {
	spans := []ch.SpanRecord{
		// deliberately out of order
		span("db1", "api1", "SELECT orders", "go-demo-api", "go", "postgresql", 20, 5),
		span("api1", "fetch1", "GET /api/orders", "go-demo-api", "go", "", 10, 30),
		span("click1", "", "click", "browser-demo", "webjs", "", 0, 50),
		span("fetch1", "click1", "HTTP GET", "browser-demo", "webjs", "", 5, 40),
		span("db0", "api1", "SELECT 1", "go-demo-api", "go", "postgresql", 12, 1),
	}
	errs := []ch.ErrorRecord{{SpanID: "api1", TraceID: "t1", ExceptionType: "PaymentError", Timestamp: t0.Add(30 * time.Millisecond)}}
	events := []ch.EventRecord{
		{Event: "$autocapture", SpanID: "click1", Properties: `{"$tag":"button"}`},
		{Event: "$pageview", SpanID: "", Properties: `{}`},
	}
	tr := BuildTrace("t1", spans, errs, events)

	if len(tr.Roots) != 1 || tr.SpanCount != 5 || tr.ErrorCount != 1 {
		t.Fatalf("roots %d spans %d errors %d", len(tr.Roots), tr.SpanCount, tr.ErrorCount)
	}
	click := tr.Roots[0]
	if click.SpanId != "click1" || click.Source != gen.Browser || len(click.Events) != 1 {
		t.Fatalf("root %+v", click)
	}
	fetch := click.Children[0]
	api := fetch.Children[0]
	if api.Source != gen.Backend || api.Depth != 2 || len(api.Errors) != 1 {
		t.Fatalf("api %+v", api)
	}
	if len(api.Children) != 2 || api.Children[0].SpanId != "db0" || api.Children[1].Source != gen.Database {
		t.Fatalf("children should be sorted by start time: %+v", api.Children)
	}
	if len(tr.Events) != 1 || tr.Events[0].Event != "$pageview" {
		t.Errorf("trace-level events %+v", tr.Events)
	}
	if tr.DurationNs != int64(50*time.Millisecond) || !tr.StartTime.Equal(t0) {
		t.Errorf("duration %d start %v", tr.DurationNs, tr.StartTime)
	}
	if len(tr.Services) != 2 || tr.Services[0] != "browser-demo" {
		t.Errorf("services %v", tr.Services)
	}
}

func TestBuildTraceOrphansCyclesAndDuplicates(t *testing.T) {
	spans := []ch.SpanRecord{
		span("a", "missing", "late child", "svc", "go", "", 5, 1),
		span("b", "", "root", "svc", "go", "", 0, 10),
		span("b", "", "root (duplicate delivery)", "svc", "go", "", 0, 10),
		span("x", "y", "cycle x", "svc", "go", "", 7, 1),
		span("y", "x", "cycle y", "svc", "go", "", 8, 1),
	}
	errs := []ch.ErrorRecord{{SpanID: "never-received", TraceID: "t1"}}
	tr := BuildTrace("t1", spans, errs, nil)
	if len(tr.Roots) != 3 {
		t.Fatalf("want root, orphan and cycle root, got %d", len(tr.Roots))
	}
	if tr.Roots[0].SpanId != "b" || tr.Roots[0].Orphan || !tr.Roots[1].Orphan || tr.Roots[1].SpanId != "a" {
		t.Fatalf("roots %+v", tr.Roots)
	}
	if tr.Roots[2].SpanId != "x" || len(tr.Roots[2].Children) != 1 || len(tr.Roots[2].Children[0].Children) != 0 {
		t.Fatalf("cycle must terminate: %+v", tr.Roots[2])
	}
	if len(tr.Roots[0].Errors) != 1 {
		t.Error("error on an unknown span should stay visible on the first root")
	}
}

func TestBuildTraceEmpty(t *testing.T) {
	tr := BuildTrace("t1", nil, nil, nil)
	if tr.Roots == nil || tr.Events == nil || tr.Services == nil || tr.SpanCount != 0 {
		t.Fatalf("%+v", tr)
	}
}

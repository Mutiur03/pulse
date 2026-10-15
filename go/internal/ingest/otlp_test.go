package ingest

import (
	"testing"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const sample = `{"resourceSpans":[{"resource":{"attributes":[
 {"key":"service.name","value":{"stringValue":"checkout"}},
 {"key":"telemetry.sdk.language","value":{"stringValue":"go"}},
 {"key":"pulse.project_id","value":{"stringValue":"7"}}]},
 "scopeSpans":[{"spans":[{
  "traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","parentSpanId":"aaaaaaaaaaaaaaaa",
  "name":"GET /api/pay","kind":3,"startTimeUnixNano":"1760500000000000000","endTimeUnixNano":"1760500000002000000",
  "attributes":[{"key":"http.response.status_code","value":{"intValue":"502"}},
   {"key":"session.id","value":{"stringValue":"s1"}},
   {"key":"db.system","value":{"stringValue":"postgresql"}}],
  "events":[{"name":"exception","timeUnixNano":"1760500000001000000","attributes":[
   {"key":"exception.type","value":{"stringValue":"*errors.errorString"}},
   {"key":"exception.message","value":{"stringValue":"card 4242 declined"}},
   {"key":"exception.stacktrace","value":{"stringValue":"main.pay()\n\t/app/main.go:10 +0x1"}}]}],
  "status":{"code":2,"message":"bad gateway"}},
  {"traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"1111111111111111","name":"SELECT","kind":"SPAN_KIND_CLIENT",
   "status":{"code":"STATUS_CODE_ERROR","message":"deadlock detected"}}]}]}]}`

func TestDecodeOTLPJSON(t *testing.T) {
	b, err := DecodeOTLPTraces([]byte(sample), false, "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(b.Spans))
	}
	s := b.Spans[0]
	checks := map[string][2]string{
		"project": {s.ProjectID, "7"}, "service": {s.ServiceName, "checkout"}, "kind": {s.Kind, "client"},
		"status": {s.StatusCode, "error"}, "session": {s.SessionID, "s1"}, "db": {s.DBSystem, "postgresql"},
		"parent": {s.ParentSpanID, "aaaaaaaaaaaaaaaa"}, "kind2": {b.Spans[1].Kind, "client"},
	}
	for k, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s: got %q want %q", k, v[0], v[1])
		}
	}
	if s.DurationNs != 2_000_000 || s.HTTPStatus != 502 || s.Timestamp.UnixNano() != 1760500000000000000 {
		t.Errorf("duration %d status %d ts %v", s.DurationNs, s.HTTPStatus, s.Timestamp)
	}

	// The ERROR span without an exception event is the same failure bubbling
	// up, so only the exception event becomes an error.
	if len(b.Errors) != 1 {
		t.Fatalf("want 1 error, got %+v", b.Errors)
	}
	e := b.Errors[0]
	if e.ExceptionType != "*errors.errorString" || e.Message != "card 4242 declined" || e.Stack == "" ||
		e.TraceID != s.TraceID || e.SpanID != s.SpanID || e.ServiceName != "checkout" || e.Platform != "go" {
		t.Errorf("exception event: %+v", e)
	}
}

func TestStatusOnlyErrorSpan(t *testing.T) {
	body := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"ab","spanId":"cd","name":"SELECT",
	 "attributes":[{"key":"error.type","value":{"stringValue":"40P01"}}],
	 "status":{"code":"STATUS_CODE_ERROR","message":"deadlock detected"}}]}]}]}`
	b, err := DecodeOTLPTraces([]byte(body), false, "1")
	if err != nil || len(b.Errors) != 1 {
		t.Fatalf("%+v %v", b, err)
	}
	if e := b.Errors[0]; e.ExceptionType != "40P01" || e.Message != "deadlock detected" || e.SpanID != "cd" {
		t.Errorf("%+v", e)
	}
}

func TestBrowserSpansDoNotDuplicateErrors(t *testing.T) {
	body := `{"resourceSpans":[{"resource":{"attributes":[{"key":"telemetry.sdk.language","value":{"stringValue":"webjs"}}]},
	 "scopeSpans":[{"spans":[{"traceId":"a1","spanId":"b1","name":"exception","status":{"code":2}}]}]}]}`
	b, err := DecodeOTLPTraces([]byte(body), false, "1")
	if err != nil || len(b.Spans) != 1 || len(b.Errors) != 0 {
		t.Fatalf("%+v %v", b, err)
	}
}

func str(s string) *common.AnyValue {
	return &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: s}}
}

func TestDecodeOTLPProtobuf(t *testing.T) {
	req := &coltrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resource.Resource{Attributes: []*common.KeyValue{{Key: "service.name", Value: str("api")}}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			TraceId:           []byte{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
			SpanId:            []byte{0, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
			Name:              "POST /pay",
			Kind:              tracepb.Span_SPAN_KIND_SERVER,
			StartTimeUnixNano: 1_000,
			EndTimeUnixNano:   5_000,
			Attributes: []*common.KeyValue{
				{Key: "http.route", Value: str("/pay")},
				{Key: "http.response.status_code", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: 500}}},
			},
			Events: []*tracepb.Span_Event{{Name: "exception", Attributes: []*common.KeyValue{
				{Key: "exception.type", Value: str("PaymentError")}, {Key: "exception.message", Value: str("declined")},
			}}},
			Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR},
		}}}},
	}}}
	body, _ := proto.Marshal(req)
	b, err := DecodeOTLPTraces(body, true, "1")
	if err != nil || len(b.Spans) != 1 || len(b.Errors) != 1 {
		t.Fatalf("%+v %v", b, err)
	}
	s := b.Spans[0]
	if s.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || s.SpanID != "00f067aa0ba902b7" || s.Kind != "server" ||
		s.HTTPRoute != "/pay" || s.HTTPStatus != 500 || s.DurationNs != 4000 || s.ServiceName != "api" {
		t.Errorf("%+v", s)
	}
	if b.Errors[0].ExceptionType != "PaymentError" || b.Errors[0].Platform != "backend" {
		t.Errorf("%+v", b.Errors[0])
	}
}

func TestDecodeOTLPLogs(t *testing.T) {
	jsonBody := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}}]},
	 "scopeLogs":[{"logRecords":[{"timeUnixNano":"1000","severityNumber":17,"body":{"stringValue":"payment failed"},
	 "traceId":"ab","spanId":"cd","attributes":[{"key":"order","value":{"intValue":"9"}}]}]}]}]}`
	recs, err := DecodeOTLPLogs([]byte(jsonBody), false, "1")
	if err != nil || len(recs) != 1 {
		t.Fatalf("%+v %v", recs, err)
	}
	if r := recs[0]; r.Severity != "ERROR" || r.Body != "payment failed" || r.TraceID != "ab" || r.ServiceName != "api" || r.Attributes != `{"order":"9"}` {
		t.Errorf("%+v", r)
	}

	pb := &collogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			ObservedTimeUnixNano: 2000, SeverityText: "warn", Body: str("slow query"), TraceId: []byte{0xab},
		}}}},
	}}}
	body, _ := proto.Marshal(pb)
	recs, err = DecodeOTLPLogs(body, true, "1")
	if err != nil || len(recs) != 1 || recs[0].Severity != "WARN" || recs[0].TraceID != "ab" || recs[0].Timestamp.UnixNano() != 2000 {
		t.Fatalf("%+v %v", recs, err)
	}
}

package ingest

import "testing"

const sample = `{"resourceSpans":[{"resource":{"attributes":[
 {"key":"service.name","value":{"stringValue":"checkout"}},
 {"key":"pulse.project_id","value":{"stringValue":"7"}}]},
 "scopeSpans":[{"spans":[{
  "traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","parentSpanId":"aaaaaaaaaaaaaaaa",
  "name":"GET /api/pay","kind":3,"startTimeUnixNano":"1760500000000000000","endTimeUnixNano":"1760500000002000000",
  "attributes":[{"key":"http.response.status_code","value":{"intValue":"502"}},
   {"key":"session.id","value":{"stringValue":"s1"}},
   {"key":"db.system","value":{"stringValue":"postgresql"}}],
  "status":{"code":2,"message":"bad gateway"}}]}]}]}`

func TestDecodeOTLPTraces(t *testing.T) {
	spans, err := DecodeOTLPTraces([]byte(sample), "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	s := spans[0]
	checks := map[string][2]string{
		"project": {s.ProjectID, "7"}, "service": {s.ServiceName, "checkout"}, "kind": {s.Kind, "client"},
		"status": {s.StatusCode, "error"}, "session": {s.SessionID, "s1"}, "db": {s.DBSystem, "postgresql"},
		"parent": {s.ParentSpanID, "aaaaaaaaaaaaaaaa"},
	}
	for k, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s: got %q want %q", k, v[0], v[1])
		}
	}
	if s.DurationNs != 2_000_000 || s.HTTPStatus != 502 {
		t.Errorf("duration %d status %d", s.DurationNs, s.HTTPStatus)
	}
	if s.Timestamp.UnixNano() != 1760500000000000000 {
		t.Errorf("timestamp %v", s.Timestamp)
	}
}

func TestDecodeOTLPDefaultProject(t *testing.T) {
	spans, err := DecodeOTLPTraces([]byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"a","spanId":"b","kind":"SPAN_KIND_SERVER"}]}]}]}`), "3")
	if err != nil || len(spans) != 1 || spans[0].ProjectID != "3" || spans[0].Kind != "server" {
		t.Fatalf("got %+v %v", spans, err)
	}
}

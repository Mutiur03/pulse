package ingest

import (
	"encoding/json"
	"strconv"
	"time"
)

// OTLP/HTTP JSON encoding (opentelemetry-proto, ExportTraceServiceRequest).

type otlpRequest struct {
	ResourceSpans []struct {
		Resource struct {
			Attributes []otlpKeyValue `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Spans []otlpSpan `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	Kind              json.RawMessage `json:"kind"`
	StartTimeUnixNano json.RawMessage `json:"startTimeUnixNano"`
	EndTimeUnixNano   json.RawMessage `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue  `json:"attributes"`
	Status            struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	} `json:"status"`
}

type otlpKeyValue struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue *string         `json:"stringValue"`
	IntValue    json.RawMessage `json:"intValue"`
	DoubleValue *float64        `json:"doubleValue"`
	BoolValue   *bool           `json:"boolValue"`
}

func (v otlpValue) String() string {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case len(v.IntValue) > 0:
		return unquote(v.IntValue)
	case v.DoubleValue != nil:
		return strconv.FormatFloat(*v.DoubleValue, 'f', -1, 64)
	case v.BoolValue != nil:
		return strconv.FormatBool(*v.BoolValue)
	}
	return ""
}

// unquote handles proto3 JSON numbers, which may be encoded as strings.
func unquote(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func rawUint(raw json.RawMessage) uint64 {
	if len(raw) == 0 {
		return 0
	}
	n, _ := strconv.ParseUint(unquote(raw), 10, 64)
	return n
}

var spanKinds = map[string]string{
	"0": "unspecified", "1": "internal", "2": "server", "3": "client", "4": "producer", "5": "consumer",
	"SPAN_KIND_UNSPECIFIED": "unspecified", "SPAN_KIND_INTERNAL": "internal", "SPAN_KIND_SERVER": "server",
	"SPAN_KIND_CLIENT": "client", "SPAN_KIND_PRODUCER": "producer", "SPAN_KIND_CONSUMER": "consumer",
}

var statusCodes = map[string]string{
	"0": "unset", "1": "ok", "2": "error",
	"STATUS_CODE_UNSET": "unset", "STATUS_CODE_OK": "ok", "STATUS_CODE_ERROR": "error",
}

func attrMap(kvs []otlpKeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.String()
	}
	return m
}

func first(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// DecodeOTLPTraces converts an OTLP/HTTP JSON trace export into spans.
// defaultProject is used when the resource carries no pulse.project_id.
func DecodeOTLPTraces(body []byte, defaultProject string) ([]Span, error) {
	var req otlpRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	var out []Span
	for _, rs := range req.ResourceSpans {
		res := attrMap(rs.Resource.Attributes)
		project := first(res, "pulse.project_id")
		if project == "" {
			project = defaultProject
		}
		service := res["service.name"]
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				attrs := attrMap(s.Attributes)
				start := rawUint(s.StartTimeUnixNano)
				end := rawUint(s.EndTimeUnixNano)
				var dur uint64
				if end > start {
					dur = end - start
				}
				ts := time.Now().UTC()
				if start > 0 {
					ts = time.Unix(0, int64(start)).UTC()
				}
				status, _ := strconv.ParseUint(first(attrs, "http.response.status_code", "http.status_code"), 10, 16)
				out = append(out, Span{
					ProjectID:     project,
					TraceID:       s.TraceID,
					SpanID:        s.SpanID,
					ParentSpanID:  s.ParentSpanID,
					Name:          s.Name,
					Kind:          spanKinds[unquote(s.Kind)],
					ServiceName:   service,
					Timestamp:     ts,
					DurationNs:    dur,
					StatusCode:    statusCodes[unquote(s.Status.Code)],
					StatusMessage: s.Status.Message,
					HTTPRoute:     first(attrs, "http.route", "url.path", "http.url", "http.target"),
					HTTPStatus:    uint16(status),
					DBSystem:      attrs["db.system"],
					DBStatement:   first(attrs, "db.query.text", "db.statement"),
					SessionID:     first(attrs, "session.id", "pulse.session_id"),
				})
			}
		}
	}
	return out, nil
}

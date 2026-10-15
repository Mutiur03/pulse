package ingest

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

// OTLP/HTTP decoding for traces and logs, in both encodings the spec allows:
// protobuf (application/x-protobuf) and JSON (application/json, hex ids).
// Both are converted to the same intermediate form and then mapped once.

type kv = map[string]string

type rawEvent struct {
	name  string
	time  uint64
	attrs kv
}

type rawSpan struct {
	traceID, spanID, parentID string
	name                      string
	kind                      int
	start, end                uint64
	attrs                     kv
	statusCode                int
	statusMsg                 string
	events                    []rawEvent
}

type rawLog struct {
	time, observed  uint64
	severityText    string
	severityNumber  int
	body            string
	traceID, spanID string
	attrs           kv
}

type rawResource struct {
	attrs kv
	spans []rawSpan
	logs  []rawLog
}

// ---- JSON ----

type jsonValue struct {
	StringValue *string         `json:"stringValue"`
	IntValue    json.RawMessage `json:"intValue"`
	DoubleValue *float64        `json:"doubleValue"`
	BoolValue   *bool           `json:"boolValue"`
	ArrayValue  *struct {
		Values []jsonValue `json:"values"`
	} `json:"arrayValue"`
	KvlistValue *struct {
		Values []jsonKV `json:"values"`
	} `json:"kvlistValue"`
}

type jsonKV struct {
	Key   string    `json:"key"`
	Value jsonValue `json:"value"`
}

func (v jsonValue) String() string {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case len(v.IntValue) > 0:
		return unquote(v.IntValue)
	case v.DoubleValue != nil:
		return strconv.FormatFloat(*v.DoubleValue, 'f', -1, 64)
	case v.BoolValue != nil:
		return strconv.FormatBool(*v.BoolValue)
	case v.ArrayValue != nil:
		parts := make([]string, len(v.ArrayValue.Values))
		for i, x := range v.ArrayValue.Values {
			parts[i] = x.String()
		}
		b, _ := json.Marshal(parts)
		return string(b)
	case v.KvlistValue != nil:
		b, _ := json.Marshal(jsonAttrs(v.KvlistValue.Values))
		return string(b)
	}
	return ""
}

func jsonAttrs(kvs []jsonKV) kv {
	m := make(kv, len(kvs))
	for _, x := range kvs {
		m[x.Key] = x.Value.String()
	}
	return m
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
	n, _ := strconv.ParseUint(unquote(raw), 10, 64)
	return n
}

var enumNames = map[string]int{
	"SPAN_KIND_UNSPECIFIED": 0, "SPAN_KIND_INTERNAL": 1, "SPAN_KIND_SERVER": 2, "SPAN_KIND_CLIENT": 3,
	"SPAN_KIND_PRODUCER": 4, "SPAN_KIND_CONSUMER": 5,
	"STATUS_CODE_UNSET": 0, "STATUS_CODE_OK": 1, "STATUS_CODE_ERROR": 2,
}

func rawEnum(raw json.RawMessage) int {
	s := unquote(raw)
	if n, ok := enumNames[s]; ok {
		return n
	}
	n, _ := strconv.Atoi(s)
	return n
}

type jsonResource struct {
	Attributes []jsonKV `json:"attributes"`
}

type jsonTraces struct {
	ResourceSpans []struct {
		Resource   jsonResource `json:"resource"`
		ScopeSpans []struct {
			Spans []struct {
				TraceID           string          `json:"traceId"`
				SpanID            string          `json:"spanId"`
				ParentSpanID      string          `json:"parentSpanId"`
				Name              string          `json:"name"`
				Kind              json.RawMessage `json:"kind"`
				StartTimeUnixNano json.RawMessage `json:"startTimeUnixNano"`
				EndTimeUnixNano   json.RawMessage `json:"endTimeUnixNano"`
				Attributes        []jsonKV        `json:"attributes"`
				Events            []struct {
					Name         string          `json:"name"`
					TimeUnixNano json.RawMessage `json:"timeUnixNano"`
					Attributes   []jsonKV        `json:"attributes"`
				} `json:"events"`
				Status struct {
					Code    json.RawMessage `json:"code"`
					Message string          `json:"message"`
				} `json:"status"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type jsonLogs struct {
	ResourceLogs []struct {
		Resource  jsonResource `json:"resource"`
		ScopeLogs []struct {
			LogRecords []struct {
				TimeUnixNano         json.RawMessage `json:"timeUnixNano"`
				ObservedTimeUnixNano json.RawMessage `json:"observedTimeUnixNano"`
				SeverityNumber       json.RawMessage `json:"severityNumber"`
				SeverityText         string          `json:"severityText"`
				Body                 jsonValue       `json:"body"`
				Attributes           []jsonKV        `json:"attributes"`
				TraceID              string          `json:"traceId"`
				SpanID               string          `json:"spanId"`
			} `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
}

func parseJSONTraces(body []byte) ([]rawResource, error) {
	var req jsonTraces
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	var out []rawResource
	for _, rs := range req.ResourceSpans {
		r := rawResource{attrs: jsonAttrs(rs.Resource.Attributes)}
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				sp := rawSpan{
					traceID: strings.ToLower(s.TraceID), spanID: strings.ToLower(s.SpanID), parentID: strings.ToLower(s.ParentSpanID),
					name: s.Name, kind: rawEnum(s.Kind), start: rawUint(s.StartTimeUnixNano), end: rawUint(s.EndTimeUnixNano),
					attrs: jsonAttrs(s.Attributes), statusCode: rawEnum(s.Status.Code), statusMsg: s.Status.Message,
				}
				for _, e := range s.Events {
					sp.events = append(sp.events, rawEvent{name: e.Name, time: rawUint(e.TimeUnixNano), attrs: jsonAttrs(e.Attributes)})
				}
				r.spans = append(r.spans, sp)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func parseJSONLogs(body []byte) ([]rawResource, error) {
	var req jsonLogs
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	var out []rawResource
	for _, rl := range req.ResourceLogs {
		r := rawResource{attrs: jsonAttrs(rl.Resource.Attributes)}
		for _, sl := range rl.ScopeLogs {
			for _, l := range sl.LogRecords {
				r.logs = append(r.logs, rawLog{
					time: rawUint(l.TimeUnixNano), observed: rawUint(l.ObservedTimeUnixNano),
					severityText: l.SeverityText, severityNumber: rawEnum(l.SeverityNumber), body: l.Body.String(),
					traceID: strings.ToLower(l.TraceID), spanID: strings.ToLower(l.SpanID), attrs: jsonAttrs(l.Attributes),
				})
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// ---- protobuf ----

func pbValue(v *common.AnyValue) string {
	switch x := v.GetValue().(type) {
	case *common.AnyValue_StringValue:
		return x.StringValue
	case *common.AnyValue_IntValue:
		return strconv.FormatInt(x.IntValue, 10)
	case *common.AnyValue_DoubleValue:
		return strconv.FormatFloat(x.DoubleValue, 'f', -1, 64)
	case *common.AnyValue_BoolValue:
		return strconv.FormatBool(x.BoolValue)
	case *common.AnyValue_BytesValue:
		return hex.EncodeToString(x.BytesValue)
	case *common.AnyValue_ArrayValue:
		parts := make([]string, len(x.ArrayValue.GetValues()))
		for i, e := range x.ArrayValue.GetValues() {
			parts[i] = pbValue(e)
		}
		b, _ := json.Marshal(parts)
		return string(b)
	case *common.AnyValue_KvlistValue:
		b, _ := json.Marshal(pbAttrs(x.KvlistValue.GetValues()))
		return string(b)
	}
	return ""
}

func pbAttrs(kvs []*common.KeyValue) kv {
	m := make(kv, len(kvs))
	for _, x := range kvs {
		m[x.GetKey()] = pbValue(x.GetValue())
	}
	return m
}

func hexID(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return hex.EncodeToString(b)
}

func parseProtoTraces(body []byte) ([]rawResource, error) {
	var req coltrace.ExportTraceServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return protoTraces(&req), nil
}

func protoTraces(req *coltrace.ExportTraceServiceRequest) []rawResource {
	var out []rawResource
	for _, rs := range req.GetResourceSpans() {
		r := rawResource{attrs: pbAttrs(rs.GetResource().GetAttributes())}
		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				sp := rawSpan{
					traceID: hexID(s.GetTraceId()), spanID: hexID(s.GetSpanId()), parentID: hexID(s.GetParentSpanId()),
					name: s.GetName(), kind: int(s.GetKind()), start: s.GetStartTimeUnixNano(), end: s.GetEndTimeUnixNano(),
					attrs: pbAttrs(s.GetAttributes()), statusCode: int(s.GetStatus().GetCode()), statusMsg: s.GetStatus().GetMessage(),
				}
				for _, e := range s.GetEvents() {
					sp.events = append(sp.events, rawEvent{name: e.GetName(), time: e.GetTimeUnixNano(), attrs: pbAttrs(e.GetAttributes())})
				}
				r.spans = append(r.spans, sp)
			}
		}
		out = append(out, r)
	}
	return out
}

func parseProtoLogs(body []byte) ([]rawResource, error) {
	var req collogs.ExportLogsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return protoLogs(&req), nil
}

func protoLogs(req *collogs.ExportLogsServiceRequest) []rawResource {
	var out []rawResource
	for _, rl := range req.GetResourceLogs() {
		r := rawResource{attrs: pbAttrs(rl.GetResource().GetAttributes())}
		for _, sl := range rl.GetScopeLogs() {
			for _, l := range sl.GetLogRecords() {
				r.logs = append(r.logs, rawLog{
					time: l.GetTimeUnixNano(), observed: l.GetObservedTimeUnixNano(),
					severityText: l.GetSeverityText(), severityNumber: int(l.GetSeverityNumber()), body: pbValue(l.GetBody()),
					traceID: hexID(l.GetTraceId()), spanID: hexID(l.GetSpanId()), attrs: pbAttrs(l.GetAttributes()),
				})
			}
		}
		out = append(out, r)
	}
	return out
}

// ---- mapping ----

var kindNames = []string{"unspecified", "internal", "server", "client", "producer", "consumer"}
var statusNames = []string{"unset", "ok", "error"}

func name(list []string, i int) string {
	if i >= 0 && i < len(list) {
		return list[i]
	}
	return ""
}

func first(m kv, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

func nanos(n uint64) time.Time {
	if n == 0 {
		return time.Now().UTC()
	}
	return time.Unix(0, int64(n)).UTC()
}

// projectFor ignores any project claimed in the payload: the project always
// comes from the authenticated ingest key.
func projectFor(_ kv, def string) string { return def }

// platform from the resource: "go", "python", "nodejs", "webjs", ...
func platform(res kv) string {
	if p := res["telemetry.sdk.language"]; p != "" {
		return p
	}
	return "backend"
}

const (
	maxAttrs     = 128
	maxAttrValue = 4096
	maxEvents    = 64
)

var resourceKeys = []string{"service.name", "service.version", "deployment.environment.name",
	"deployment.environment", "telemetry.sdk.language", "telemetry.sdk.name", "host.name"}

func clip(v string) string {
	if len(v) > maxAttrValue {
		return v[:maxAttrValue]
	}
	return v
}

func spanAttributes(span, res kv) map[string]string {
	out := make(map[string]string, len(span)+len(resourceKeys))
	for k, v := range span {
		if len(out) >= maxAttrs {
			break
		}
		out[k] = clip(v)
	}
	for _, k := range resourceKeys {
		if v := res[k]; v != "" {
			out["resource."+k] = clip(v)
		}
	}
	return out
}

func spanEvents(evs []rawEvent) []SpanEvent {
	var out []SpanEvent
	for _, e := range evs {
		if e.name == "exception" || len(out) >= maxEvents {
			continue
		}
		attrs := make(map[string]string, len(e.attrs))
		for k, v := range e.attrs {
			attrs[k] = clip(v)
		}
		out = append(out, SpanEvent{Name: e.name, Time: nanos(e.time), Attributes: attrs})
	}
	return out
}

// TraceBatch is the result of decoding one OTLP trace export.
type TraceBatch struct {
	Spans  []Span
	Errors []ErrorEvent
}

func mapTraces(resources []rawResource, defaultProject string) TraceBatch {
	var b TraceBatch
	// Errors usually bubble up: the span that threw records an exception and
	// its ancestors only set an ERROR status. Traces that already carry an
	// exception event do not get extra status-only errors.
	hasException := map[string]bool{}
	// Session and user ids usually sit only on the server span (copied from
	// baggage); child spans and the errors they record inherit them from any
	// span of the same trace in this export.
	traceSession, traceUser := map[string]string{}, map[string]string{}
	for _, r := range resources {
		for _, s := range r.spans {
			if v := first(s.attrs, "session.id", "pulse.session_id"); v != "" {
				traceSession[s.traceID] = v
			}
			if v := first(s.attrs, "enduser.id", "pulse.distinct_id"); v != "" {
				traceUser[s.traceID] = v
			}
			for _, e := range s.events {
				if e.name == "exception" {
					hasException[s.traceID] = true
				}
			}
		}
	}
	for _, r := range resources {
		project := projectFor(r.attrs, defaultProject)
		service := r.attrs["service.name"]
		plat := platform(r.attrs)
		release := first(r.attrs, "service.version")
		env := first(r.attrs, "deployment.environment.name", "deployment.environment")
		// Browser errors already arrive through /v1/errors with full context,
		// so only backend spans are turned into error rows.
		deriveErrors := plat != "webjs"

		for _, s := range r.spans {
			var dur uint64
			if s.end > s.start {
				dur = s.end - s.start
			}
			status, _ := strconv.ParseUint(first(s.attrs, "http.response.status_code", "http.status_code"), 10, 16)
			session := first(s.attrs, "session.id", "pulse.session_id")
			if session == "" {
				session = traceSession[s.traceID]
			}
			b.Spans = append(b.Spans, Span{
				ProjectID:     project,
				TraceID:       s.traceID,
				SpanID:        s.spanID,
				ParentSpanID:  s.parentID,
				Name:          s.name,
				Kind:          name(kindNames, s.kind),
				ServiceName:   service,
				Platform:      r.attrs["telemetry.sdk.language"],
				Timestamp:     nanos(s.start),
				DurationNs:    dur,
				StatusCode:    name(statusNames, s.statusCode),
				StatusMessage: s.statusMsg,
				HTTPRoute:     first(s.attrs, "http.route", "url.path", "http.target", "http.url", "url.full"),
				HTTPStatus:    uint16(status),
				DBSystem:      first(s.attrs, "db.system.name", "db.system"),
				DBStatement:   first(s.attrs, "db.query.text", "db.statement"),
				SessionID:     session,
				Attributes:    spanAttributes(s.attrs, r.attrs),
				Events:        spanEvents(s.events),
			})
			if !deriveErrors {
				continue
			}
			base := ErrorEvent{
				ProjectID: project, Platform: plat, Release: release, Environment: env, ServiceName: service,
				TraceID: s.traceID, SpanID: s.spanID, SessionID: session,
				DistinctID: orDefault(first(s.attrs, "enduser.id", "pulse.distinct_id"), traceUser[s.traceID]),
				URL:        first(s.attrs, "url.full", "http.url", "http.route", "url.path"),
			}
			found := false
			for _, e := range s.events {
				if e.name != "exception" {
					continue
				}
				found = true
				ev := base
				ev.Timestamp = nanos(e.time)
				ev.ExceptionType = first(e.attrs, "exception.type")
				ev.Message = first(e.attrs, "exception.message")
				ev.Stack = first(e.attrs, "exception.stacktrace")
				if ev.ExceptionType == "" && ev.Message == "" {
					ev.Message = s.name
				}
				b.Errors = append(b.Errors, ev)
			}
			if !found && s.statusCode == 2 && !hasException[s.traceID] {
				ev := base
				ev.Timestamp = nanos(s.end)
				ev.ExceptionType = first(s.attrs, "error.type", "exception.type")
				if ev.ExceptionType == "" {
					ev.ExceptionType = "SpanError"
				}
				ev.Message = s.statusMsg
				if ev.Message == "" {
					ev.Message = s.name + " failed"
				}
				b.Errors = append(b.Errors, ev)
			}
		}
	}
	return b
}

func mapLogs(resources []rawResource, defaultProject string) []LogRecord {
	var out []LogRecord
	for _, r := range resources {
		project := projectFor(r.attrs, defaultProject)
		for _, l := range r.logs {
			ts := l.time
			if ts == 0 {
				ts = l.observed
			}
			sev := l.severityText
			if sev == "" {
				sev = severityName(l.severityNumber)
			}
			attrs, _ := json.Marshal(l.attrs)
			out = append(out, LogRecord{
				ProjectID: project, Timestamp: nanos(ts), TraceID: l.traceID, SpanID: l.spanID,
				Severity: strings.ToUpper(sev), ServiceName: r.attrs["service.name"], Body: l.body, Attributes: string(attrs),
			})
		}
	}
	return out
}

func severityName(n int) string {
	switch {
	case n <= 0:
		return ""
	case n <= 4:
		return "TRACE"
	case n <= 8:
		return "DEBUG"
	case n <= 12:
		return "INFO"
	case n <= 16:
		return "WARN"
	case n <= 20:
		return "ERROR"
	default:
		return "FATAL"
	}
}

// DecodeOTLPTraces decodes an OTLP/HTTP trace export. isProto selects the
// protobuf encoding; otherwise the body is OTLP JSON.
func DecodeOTLPTraces(body []byte, isProto bool, defaultProject string) (TraceBatch, error) {
	parse := parseJSONTraces
	if isProto {
		parse = parseProtoTraces
	}
	res, err := parse(body)
	if err != nil {
		return TraceBatch{}, err
	}
	return mapTraces(res, defaultProject), nil
}

// DecodeOTLPLogs decodes an OTLP/HTTP logs export.
func DecodeOTLPLogs(body []byte, isProto bool, defaultProject string) ([]LogRecord, error) {
	parse := parseJSONLogs
	if isProto {
		parse = parseProtoLogs
	}
	res, err := parse(body)
	if err != nil {
		return nil, err
	}
	return mapLogs(res, defaultProject), nil
}

// TracesFromProto maps an already decoded export, as received over gRPC.
func TracesFromProto(req *coltrace.ExportTraceServiceRequest, defaultProject string) TraceBatch {
	return mapTraces(protoTraces(req), defaultProject)
}

// LogsFromProto maps an already decoded logs export.
func LogsFromProto(req *collogs.ExportLogsServiceRequest, defaultProject string) []LogRecord {
	return mapLogs(protoLogs(req), defaultProject)
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

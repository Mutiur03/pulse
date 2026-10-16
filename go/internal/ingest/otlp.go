package ingest

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
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
	tc := scanTraces(resources)
	for _, r := range resources {
		meta := newResourceMeta(r.attrs, defaultProject)
		for _, s := range r.spans {
			span := meta.span(s, tc)
			b.Spans = append(b.Spans, span)
			// Browser errors already arrive through /v1/errors with full
			// context, so only backend spans are turned into error rows.
			if meta.platform != "webjs" {
				b.Errors = append(b.Errors, meta.spanErrors(s, span.SessionID, tc)...)
			}
		}
	}
	return b
}

// traceContext is what spans of one trace share within an export.
type traceContext struct {
	// Errors usually bubble up: the span that threw records an exception
	// and its ancestors only set an ERROR status. Traces that already carry
	// an exception event do not get extra status-only errors.
	hasException map[string]bool
	// Session and user ids usually sit only on the server span (copied from
	// baggage); child spans and the errors they record inherit them from any
	// span of the same trace in this export.
	session, user map[string]string
}

func scanTraces(resources []rawResource) traceContext {
	tc := traceContext{hasException: map[string]bool{}, session: map[string]string{}, user: map[string]string{}}
	for _, r := range resources {
		for _, s := range r.spans {
			if v := first(s.attrs, "session.id", "pulse.session_id"); v != "" {
				tc.session[s.traceID] = v
			}
			if v := first(s.attrs, "enduser.id", "pulse.distinct_id"); v != "" {
				tc.user[s.traceID] = v
			}
			for _, e := range s.events {
				if e.name == "exception" {
					tc.hasException[s.traceID] = true
				}
			}
		}
	}
	return tc
}

// resourceMeta is what every span of a resource has in common.
type resourceMeta struct {
	attrs                                                 kv
	project, service, platform, sdkLanguage, release, env string
}

func newResourceMeta(attrs kv, defaultProject string) resourceMeta {
	return resourceMeta{
		attrs: attrs,
		// Any project claimed in the payload is ignored: the project always
		// comes from the authenticated ingest key.
		project:     defaultProject,
		service:     attrs["service.name"],
		platform:    platform(attrs),
		sdkLanguage: attrs["telemetry.sdk.language"],
		release:     first(attrs, "service.version"),
		env:         first(attrs, "deployment.environment.name", "deployment.environment"),
	}
}

func (m resourceMeta) span(s rawSpan, tc traceContext) Span {
	var dur uint64
	if s.end > s.start {
		dur = s.end - s.start
	}
	status, _ := strconv.ParseUint(first(s.attrs, "http.response.status_code", "http.status_code"), 10, 16)
	session := orDefault(first(s.attrs, "session.id", "pulse.session_id"), tc.session[s.traceID])
	return Span{
		ProjectID:     m.project,
		TraceID:       s.traceID,
		SpanID:        s.spanID,
		ParentSpanID:  s.parentID,
		Name:          s.name,
		Kind:          name(kindNames, s.kind),
		ServiceName:   m.service,
		Platform:      m.sdkLanguage,
		Timestamp:     nanos(s.start),
		DurationNs:    dur,
		StatusCode:    name(statusNames, s.statusCode),
		StatusMessage: s.statusMsg,
		HTTPRoute:     first(s.attrs, "http.route", "url.path", "http.target", "http.url", "url.full"),
		HTTPStatus:    uint16(status),
		DBSystem:      first(s.attrs, "db.system.name", "db.system"),
		DBStatement:   first(s.attrs, "db.query.text", "db.statement"),
		SessionID:     session,
		Attributes:    spanAttributes(s.attrs, m.attrs),
		Events:        spanEvents(s.events),
	}
}

// spanErrors turns the span's exception events into errors or, when the
// span failed without one and its trace has none, a status-only error.
func (m resourceMeta) spanErrors(s rawSpan, session string, tc traceContext) []ErrorEvent {
	base := ErrorEvent{
		ProjectID: m.project, Platform: m.platform, Release: m.release, Environment: m.env, ServiceName: m.service,
		TraceID: s.traceID, SpanID: s.spanID, SessionID: session,
		DistinctID: orDefault(first(s.attrs, "enduser.id", "pulse.distinct_id"), tc.user[s.traceID]),
		URL:        first(s.attrs, "url.full", "http.url", "http.route", "url.path"),
	}
	var out []ErrorEvent
	for _, e := range s.events {
		if e.name != "exception" {
			continue
		}
		ev := base
		ev.Timestamp = nanos(e.time)
		ev.ExceptionType = first(e.attrs, "exception.type")
		ev.Message = first(e.attrs, "exception.message")
		ev.Stack = first(e.attrs, "exception.stacktrace")
		if ev.ExceptionType == "" && ev.Message == "" {
			ev.Message = s.name
		}
		out = append(out, ev)
	}
	if len(out) == 0 && s.statusCode == 2 && !tc.hasException[s.traceID] {
		ev := base
		ev.Timestamp = nanos(s.end)
		ev.ExceptionType = orDefault(first(s.attrs, "error.type", "exception.type"), "SpanError")
		ev.Message = orDefault(s.statusMsg, s.name+" failed")
		out = append(out, ev)
	}
	return out
}

func mapLogs(resources []rawResource, defaultProject string) []LogRecord {
	var out []LogRecord
	for _, r := range resources {
		// Any project claimed in the payload is ignored: the project always
		// comes from the authenticated ingest key.
		project := defaultProject
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

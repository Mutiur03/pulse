package ingest

import (
	"encoding/hex"
	"encoding/json"
	"strconv"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

// OTLP protobuf decoding into the shared raw form.

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

package ingest

import (
	"encoding/json"
	"strconv"
	"strings"
)

// OTLP/JSON decoding into the shared raw form (hex ids, string-encoded
// 64-bit integers and enum names are all accepted).

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

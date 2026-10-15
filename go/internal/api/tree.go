package api

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Source classifies a span for display: database calls, browser spans
// (from the web SDK), and everything else as backend.
func Source(s ch.SpanRecord) gen.SpanSource {
	switch {
	case s.DBSystem != "":
		return gen.Database
	case s.Platform == "webjs":
		return gen.Browser
	default:
		return gen.Backend
	}
}

func errorOccurrence(e ch.ErrorRecord) gen.ErrorOccurrence {
	return gen.ErrorOccurrence{
		Timestamp: e.Timestamp, IssueId: int64(e.IssueID), ExceptionType: e.ExceptionType, Message: e.Message,
		Stack: strPtr(e.Stack), Culprit: strPtr(e.Culprit), TraceId: e.TraceID, SpanId: e.SpanID,
		ServiceName: e.ServiceName, Platform: e.Platform, SessionId: strPtr(e.SessionID),
		DistinctId: strPtr(e.DistinctID), Url: strPtr(e.URL),
	}
}

func productEvent(e ch.EventRecord) gen.ProductEvent {
	props := map[string]interface{}{}
	_ = json.Unmarshal([]byte(e.Properties), &props)
	return gen.ProductEvent{
		Event: e.Event, Timestamp: e.Timestamp, DistinctId: e.DistinctID, SessionId: e.SessionID,
		SpanId: e.SpanID, Properties: props,
	}
}

// BuildTrace assembles spans into a tree via parent_span_id and attaches
// errors and product events to the span they reference. Spans whose parent
// is missing become extra roots marked orphan; errors and events that point
// at no received span are kept at the trace level (events) or on the
// nearest root (errors) so nothing is dropped.
func BuildTrace(traceID string, spans []ch.SpanRecord, errs []ch.ErrorRecord, events []ch.EventRecord) gen.Trace {
	t := gen.Trace{
		TraceId: traceID, Roots: []gen.SpanNode{}, Events: []gen.ProductEvent{}, Services: []string{},
		ErrorCount: len(errs),
	}

	byID := make(map[string]ch.SpanRecord, len(spans))
	for _, s := range spans {
		if _, dup := byID[s.SpanID]; !dup {
			byID[s.SpanID] = s
		}
	}
	t.SpanCount = len(byID)
	children := map[string][]string{}
	var roots []string
	services := map[string]bool{}
	var start, end time.Time
	for id, s := range byID {
		if s.ParentSpanID != "" && s.ParentSpanID != s.SpanID {
			if _, ok := byID[s.ParentSpanID]; ok {
				children[s.ParentSpanID] = append(children[s.ParentSpanID], id)
			} else {
				roots = append(roots, id)
			}
		} else {
			roots = append(roots, id)
		}
		if s.ServiceName != "" {
			services[s.ServiceName] = true
		}
		if start.IsZero() || s.Timestamp.Before(start) {
			start = s.Timestamp
		}
		if e := s.Timestamp.Add(time.Duration(s.DurationNs)); e.After(end) {
			end = e
		}
	}

	errsBySpan := map[string][]gen.ErrorOccurrence{}
	var looseErrs []gen.ErrorOccurrence
	for _, e := range errs {
		if _, ok := byID[e.SpanID]; ok {
			errsBySpan[e.SpanID] = append(errsBySpan[e.SpanID], errorOccurrence(e))
		} else {
			looseErrs = append(looseErrs, errorOccurrence(e))
		}
		if start.IsZero() || e.Timestamp.Before(start) {
			start = e.Timestamp
		}
	}
	evBySpan := map[string][]gen.ProductEvent{}
	for _, e := range events {
		if _, ok := byID[e.SpanID]; ok && e.SpanID != "" {
			evBySpan[e.SpanID] = append(evBySpan[e.SpanID], productEvent(e))
		} else {
			t.Events = append(t.Events, productEvent(e))
		}
	}

	byStart := func(ids []string) {
		sort.SliceStable(ids, func(i, j int) bool {
			a, b := byID[ids[i]], byID[ids[j]]
			if !a.Timestamp.Equal(b.Timestamp) {
				return a.Timestamp.Before(b.Timestamp)
			}
			return ids[i] < ids[j]
		})
	}

	visited := map[string]bool{}
	var build func(id string, depth int, orphan bool) gen.SpanNode
	build = func(id string, depth int, orphan bool) gen.SpanNode {
		visited[id] = true
		s := byID[id]
		n := gen.SpanNode{
			SpanId: s.SpanID, ParentSpanId: s.ParentSpanID, Name: s.Name, Kind: s.Kind, ServiceName: s.ServiceName,
			Source: Source(s), StartTime: s.Timestamp, DurationNs: int64(s.DurationNs), StatusCode: s.StatusCode,
			StatusMessage: strPtr(s.StatusMessage), HttpRoute: strPtr(s.HTTPRoute), DbSystem: strPtr(s.DBSystem),
			DbStatement: strPtr(s.DBStatement), SessionId: strPtr(s.SessionID), Depth: depth, Orphan: orphan,
			Children: []gen.SpanNode{}, Errors: errsBySpan[id], Events: evBySpan[id],
		}
		if s.HTTPStatus != 0 {
			st := int(s.HTTPStatus)
			n.HttpStatus = &st
		}
		if n.Errors == nil {
			n.Errors = []gen.ErrorOccurrence{}
		}
		if n.Events == nil {
			n.Events = []gen.ProductEvent{}
		}
		kids := children[id]
		byStart(kids)
		for _, k := range kids {
			if !visited[k] {
				n.Children = append(n.Children, build(k, depth+1, false))
			}
		}
		return n
	}

	byStart(roots)
	for _, r := range roots {
		s := byID[r]
		t.Roots = append(t.Roots, build(r, 0, s.ParentSpanID != "" && s.ParentSpanID != s.SpanID))
	}
	// Parent cycles leave spans unreachable from any root; surface them too.
	var rest []string
	for id := range byID {
		if !visited[id] {
			rest = append(rest, id)
		}
	}
	byStart(rest)
	for _, id := range rest {
		if !visited[id] {
			t.Roots = append(t.Roots, build(id, 0, true))
		}
	}
	if len(looseErrs) > 0 {
		if len(t.Roots) > 0 {
			t.Roots[0].Errors = append(t.Roots[0].Errors, looseErrs...)
		}
	}

	for s := range services {
		t.Services = append(t.Services, s)
	}
	sort.Strings(t.Services)
	t.StartTime = start
	if end.After(start) {
		t.DurationNs = int64(end.Sub(start))
	}
	return t
}

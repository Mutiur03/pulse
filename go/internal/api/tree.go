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
		DistinctId: strPtr(e.DistinctID), Url: strPtr(e.URL), Release: strPtr(e.Release),
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
	b := newTraceBuilder(spans)
	t.SpanCount = len(b.byID)
	looseErrs := b.attachErrors(errs)
	t.Events = append(t.Events, b.attachEvents(events)...)
	t.Roots = append(t.Roots, b.roots()...)
	if len(looseErrs) > 0 && len(t.Roots) > 0 {
		t.Roots[0].Errors = append(t.Roots[0].Errors, looseErrs...)
	}
	t.Services = b.serviceList()
	t.StartTime = b.start
	if b.end.After(b.start) {
		t.DurationNs = int64(b.end.Sub(b.start))
	}
	return t
}

// traceBuilder holds the indexes BuildTrace needs while it walks the tree.
type traceBuilder struct {
	byID       map[string]ch.SpanRecord
	children   map[string][]string
	rootIDs    []string
	services   map[string]bool
	start, end time.Time
	errsBySpan map[string][]gen.ErrorOccurrence
	evBySpan   map[string][]gen.ProductEvent
	visited    map[string]bool
}

// hasParent reports whether the span names a parent other than itself.
func hasParent(s ch.SpanRecord) bool {
	return s.ParentSpanID != "" && s.ParentSpanID != s.SpanID
}

// newTraceBuilder indexes spans by id (first occurrence wins), links
// children to parents and records the services and time bounds.
func newTraceBuilder(spans []ch.SpanRecord) *traceBuilder {
	b := &traceBuilder{
		byID: make(map[string]ch.SpanRecord, len(spans)), children: map[string][]string{},
		services: map[string]bool{}, errsBySpan: map[string][]gen.ErrorOccurrence{},
		evBySpan: map[string][]gen.ProductEvent{}, visited: map[string]bool{},
	}
	for _, s := range spans {
		if _, dup := b.byID[s.SpanID]; !dup {
			b.byID[s.SpanID] = s
		}
	}
	for id, s := range b.byID {
		if _, ok := b.byID[s.ParentSpanID]; ok && hasParent(s) {
			b.children[s.ParentSpanID] = append(b.children[s.ParentSpanID], id)
		} else {
			b.rootIDs = append(b.rootIDs, id)
		}
		if s.ServiceName != "" {
			b.services[s.ServiceName] = true
		}
		b.extendStart(s.Timestamp)
		if e := s.Timestamp.Add(time.Duration(s.DurationNs)); e.After(b.end) {
			b.end = e
		}
	}
	return b
}

func (b *traceBuilder) extendStart(ts time.Time) {
	if b.start.IsZero() || ts.Before(b.start) {
		b.start = ts
	}
}

// attachErrors files errors under their span and returns those whose span
// was not received.
func (b *traceBuilder) attachErrors(errs []ch.ErrorRecord) []gen.ErrorOccurrence {
	var loose []gen.ErrorOccurrence
	for _, e := range errs {
		if _, ok := b.byID[e.SpanID]; ok {
			b.errsBySpan[e.SpanID] = append(b.errsBySpan[e.SpanID], errorOccurrence(e))
		} else {
			loose = append(loose, errorOccurrence(e))
		}
		b.extendStart(e.Timestamp)
	}
	return loose
}

// attachEvents files events under their span and returns the trace-level
// ones (no span, or a span that was not received).
func (b *traceBuilder) attachEvents(events []ch.EventRecord) []gen.ProductEvent {
	var traceLevel []gen.ProductEvent
	for _, e := range events {
		if _, ok := b.byID[e.SpanID]; ok && e.SpanID != "" {
			b.evBySpan[e.SpanID] = append(b.evBySpan[e.SpanID], productEvent(e))
		} else {
			traceLevel = append(traceLevel, productEvent(e))
		}
	}
	return traceLevel
}

// sortByStart orders span ids by start time, then id.
func (b *traceBuilder) sortByStart(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		x, y := b.byID[ids[i]], b.byID[ids[j]]
		if !x.Timestamp.Equal(y.Timestamp) {
			return x.Timestamp.Before(y.Timestamp)
		}
		return ids[i] < ids[j]
	})
}

// roots builds every root subtree, then surfaces spans that parent cycles
// left unreachable as extra orphan roots.
func (b *traceBuilder) roots() []gen.SpanNode {
	var out []gen.SpanNode
	b.sortByStart(b.rootIDs)
	for _, r := range b.rootIDs {
		out = append(out, b.build(r, 0, hasParent(b.byID[r])))
	}
	var rest []string
	for id := range b.byID {
		if !b.visited[id] {
			rest = append(rest, id)
		}
	}
	b.sortByStart(rest)
	for _, id := range rest {
		if !b.visited[id] {
			out = append(out, b.build(id, 0, true))
		}
	}
	return out
}

// build turns a span and its unvisited descendants into a node.
func (b *traceBuilder) build(id string, depth int, orphan bool) gen.SpanNode {
	b.visited[id] = true
	n := spanNode(b.byID[id], depth, orphan)
	if errs := b.errsBySpan[id]; errs != nil {
		n.Errors = errs
	}
	if evs := b.evBySpan[id]; evs != nil {
		n.Events = evs
	}
	kids := b.children[id]
	b.sortByStart(kids)
	for _, k := range kids {
		if !b.visited[k] {
			n.Children = append(n.Children, b.build(k, depth+1, false))
		}
	}
	return n
}

func (b *traceBuilder) serviceList() []string {
	out := []string{}
	for s := range b.services {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// spanNode maps one span to its API node, without children, errors or
// events.
func spanNode(s ch.SpanRecord, depth int, orphan bool) gen.SpanNode {
	n := gen.SpanNode{
		SpanId: s.SpanID, ParentSpanId: s.ParentSpanID, Name: s.Name, Kind: s.Kind, ServiceName: s.ServiceName,
		Source: Source(s), StartTime: s.Timestamp, DurationNs: int64(s.DurationNs), StatusCode: s.StatusCode,
		StatusMessage: strPtr(s.StatusMessage), HttpRoute: strPtr(s.HTTPRoute), DbSystem: strPtr(s.DBSystem),
		DbStatement: strPtr(s.DBStatement), SessionId: strPtr(s.SessionID), Depth: depth, Orphan: orphan,
		Children: []gen.SpanNode{}, Errors: []gen.ErrorOccurrence{}, Events: []gen.ProductEvent{},
	}
	if s.HTTPStatus != 0 {
		st := int(s.HTTPStatus)
		n.HttpStatus = &st
	}
	if len(s.Attributes) > 0 {
		attrs := s.Attributes
		n.Attributes = &attrs
	}
	if s.SpanEvents != "" && s.SpanEvents != "[]" {
		var evs []gen.SpanEvent
		if json.Unmarshal([]byte(s.SpanEvents), &evs) == nil && len(evs) > 0 {
			n.SpanEvents = &evs
		}
	}
	return n
}

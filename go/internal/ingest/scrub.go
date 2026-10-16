package ingest

import (
	"context"
	"encoding/json"

	"github.com/Mutiur03/pulse/internal/governance"
)

// ScrubSource returns a project's compiled scrubbing rules; nil scrubs
// nothing.
type ScrubSource interface {
	Scrubber(ctx context.Context, project string) *governance.Scrubber
}

// WithScrubbing removes PII from every record before it is queued, using
// each project's rules.
func WithScrubbing(s ScrubSource) Option { return func(h *handler) { h.scrub = s } }

func (h *handler) scrubber(ctx context.Context, project string) *governance.Scrubber {
	if h.scrub == nil {
		return nil
	}
	return h.scrub.Scrubber(ctx, project)
}

// scrubRecord scrubs the free-text fields of a record in place. Ids, names
// of spans and events, and numbers are kept.
func (h *handler) scrubRecord(ctx context.Context, v interface{}) {
	switch r := v.(type) {
	case *Event:
		if s := h.scrubber(ctx, r.ProjectID); s != nil {
			s.Map(r.Properties)
		}
	case *ErrorEvent:
		if s := h.scrubber(ctx, r.ProjectID); s != nil {
			r.Message, r.Stack, r.URL = s.String(r.Message), s.String(r.Stack), s.String(r.URL)
		}
	case *LogRecord:
		if s := h.scrubber(ctx, r.ProjectID); s != nil {
			r.Body = s.String(r.Body)
			if r.Attributes != "" {
				r.Attributes = string(s.JSON([]byte(r.Attributes)))
			}
		}
	case *Span:
		if s := h.scrubber(ctx, r.ProjectID); s != nil {
			r.StatusMessage, r.DBStatement = s.String(r.StatusMessage), s.String(r.DBStatement)
			s.StringMap(r.Attributes)
			for i := range r.Events {
				s.StringMap(r.Events[i].Attributes)
			}
		}
	}
}

// scrubEvents scrubs the rrweb events of a replay chunk.
func scrubEvents(s *governance.Scrubber, events []json.RawMessage) {
	if s == nil {
		return
	}
	for i, e := range events {
		events[i] = s.JSON(e)
	}
}

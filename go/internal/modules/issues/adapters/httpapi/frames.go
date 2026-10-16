package httpapi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
)

// stackFrames returns an event's frames, resolving them again when asked or
// when nothing mapped at ingest (maps are often uploaded after the deploy).
// Grouping is not changed after the fact.
func (h *Handlers) stackFrames(ctx context.Context, project int64, e ch.ErrorRecord, again bool) ([]symbolicate.Frame, int) {
	var frames []symbolicate.Frame
	_ = json.Unmarshal([]byte(e.Frames), &frames)
	mapped := int(e.MappedFrames)
	if h.sym != nil && (again || mapped == 0) && e.Stack != "" {
		ids := map[string]string{}
		_ = json.Unmarshal([]byte(e.DebugIDs), &ids)
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if res := h.sym.Resolve(ctx, project, e.Release, ids, e.Stack); res.Mapped >= mapped && len(res.Frames) > 0 {
			frames, mapped = res.Frames, res.Mapped
		}
	}
	return frames, mapped
}

func toFrames(in []symbolicate.Frame) *[]gen.StackFrame {
	if len(in) == 0 {
		return nil
	}
	out := make([]gen.StackFrame, len(in))
	for i, f := range in {
		out[i] = gen.StackFrame{
			Function: f.Function, File: f.File, Line: f.Line, Column: f.Column, InApp: f.InApp, Mapped: f.Mapped,
			ContextLine: apikit.StrPtr(f.ContextLine), RawFunction: apikit.StrPtr(f.RawFunction),
			RawUrl: f.RawURL, RawLine: f.RawLine, RawColumn: f.RawColumn,
		}
		if len(f.PreContext) > 0 {
			pre := f.PreContext
			out[i].PreContext = &pre
		}
		if len(f.PostContext) > 0 {
			post := f.PostContext
			out[i].PostContext = &post
		}
	}
	return &out
}

// Package processor holds stream-processing steps that sit between Kafka and storage.
package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Mutiur03/pulse/internal/grouping"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/sourcemap"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
)

// TopicIssueEvents receives new_issue and regression notifications.
const TopicIssueEvents = "issue.events"

type IssueRecorder interface {
	Record(ctx context.Context, o issues.Occurrence) (issues.Issue, issues.Kind, error)
}

type Publisher interface {
	Produce(ctx context.Context, topic string, key, value []byte) error
}

// GroupErrors fingerprints each raw error, records it against its issue in
// Postgres, publishes new-issue and regression events, and returns the rows
// to write to ClickHouse.
func GroupErrors(ctx context.Context, raw []map[string]interface{}, rec IssueRecorder, pub Publisher) ([]ch.ErrorRow, error) {
	return GroupErrorsWith(ctx, raw, rec, pub, nil)
}

// MaxDebugIDs caps how many file to debug id pairs an error may carry.
const MaxDebugIDs = 100

// debugIDs reads the SDK's {generated file URL: debug id} map.
func debugIDs(e map[string]interface{}) map[string]string {
	m, _ := e["debug_ids"].(map[string]interface{})
	out := map[string]string{}
	for k, v := range m {
		if id := sourcemap.NormalizeDebugID(fmt.Sprint(v)); id != "" && len(out) < MaxDebugIDs {
			out[k] = id
		}
	}
	return out
}

// GroupErrorsWith also symbolicates JavaScript stacks with uploaded source
// maps first, so issues group by original code. sym may be nil.
func GroupErrorsWith(ctx context.Context, raw []map[string]interface{}, rec IssueRecorder, pub Publisher, sym *symbolicate.Symbolicator) ([]ch.ErrorRow, error) {
	rows := make([]ch.ErrorRow, 0, len(raw))
	for _, e := range raw {
		r := ch.ErrorRow{
			ProjectID:     ch.ProjectOf(e),
			Timestamp:     ch.TimeOf(e),
			Platform:      ch.Str(e, "platform"),
			ServiceName:   ch.Str(e, "service_name"),
			ExceptionType: ch.Str(e, "exception_type"),
			Message:       ch.Str(e, "message"),
			Stack:         ch.Str(e, "stack"),
			Release:       ch.Str(e, "release"),
			Environment:   ch.Str(e, "environment"),
			TraceID:       ch.Str(e, "trace_id"),
			SpanID:        ch.Str(e, "span_id"),
			SessionID:     ch.Str(e, "session_id"),
			DistinctID:    ch.Str(e, "distinct_id"),
			URL:           ch.Str(e, "url"),
		}
		g := grouping.Group(r.ExceptionType, r.Message, r.Stack)
		ids := debugIDs(e)
		if len(ids) > 0 {
			b, _ := json.Marshal(ids)
			r.DebugIDs = string(b)
		}
		if res := sym.Resolve(ctx, int64(r.ProjectID), r.Release, ids, r.Stack); len(res.Frames) > 0 {
			b, _ := json.Marshal(res.Frames)
			r.Frames, r.MappedFrames = string(b), uint16(res.Mapped)
			if res.Mapped > 0 {
				g = grouping.GroupFrames(r.ExceptionType, r.Message, symbolicate.GroupingFrames(res.Frames))
			}
		}
		r.Fingerprint, r.Title, r.Culprit = g.Fingerprint, g.Title, g.Culprit
		if fp := ch.Str(e, "fingerprint"); fp != "" {
			r.Fingerprint = fp // client-supplied override
		}

		iss, kind, err := rec.Record(ctx, issues.Occurrence{
			ProjectID: int64(r.ProjectID), Fingerprint: r.Fingerprint, Title: r.Title,
			ExceptionType: r.ExceptionType, Culprit: r.Culprit, Platform: r.Platform, ServiceName: r.ServiceName,
			TraceID: r.TraceID, SpanID: r.SpanID, Timestamp: r.Timestamp,
		})
		if err != nil {
			return nil, fmt.Errorf("record issue: %w", err)
		}
		r.IssueID = uint64(iss.ID)
		rows = append(rows, r)

		if kind != issues.KindNone && pub != nil {
			ev, _ := json.Marshal(issues.Event{
				Type: kind, IssueID: iss.ID, ProjectID: iss.ProjectID, Fingerprint: iss.Fingerprint,
				Title: iss.Title, ServiceName: r.ServiceName, TraceID: r.TraceID, Timestamp: r.Timestamp,
			})
			if err := pub.Produce(ctx, TopicIssueEvents, []byte(fmt.Sprint(iss.ID)), ev); err != nil {
				// The issue itself is stored; a lost notification should not block ingestion.
				slog.Error("publishing issue event failed", "kind", kind, "issue_id", iss.ID, "err", err)
			}
		}
	}
	return rows, nil
}

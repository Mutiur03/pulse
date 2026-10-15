// Package processor holds stream-processing steps that sit between Kafka and storage.
package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Mutiur03/pulse/internal/grouping"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/store/ch"
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
				log.Printf("publish %s for issue %d failed: %v", kind, iss.ID, err)
			}
		}
	}
	return rows, nil
}

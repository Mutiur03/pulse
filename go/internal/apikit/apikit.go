// Package apikit holds the HTTP helpers shared by the query-api module
// adapters: error responses, project access, time ranges and the options
// every generated strict handler uses.
package apikit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/httpx"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Error is an API error with its HTTP status. Handlers return it and the
// strict handler options write it as {"error": Message}.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Err returns an *Error.
func Err(status int, msg string) error { return &Error{Status: status, Message: msg} }

// BadRequest wraps err as a 400 with its message.
func BadRequest(err error) error { return Err(http.StatusBadRequest, err.Error()) }

// LimitBody returns middleware that caps request bodies at n bytes.
func LimitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// Readable returns a 404 unless the caller may read project.
func Readable(ctx context.Context, project int64) error {
	if !CanRead(ctx, project) {
		return Err(http.StatusNotFound, "project not found")
	}
	return nil
}

// Messages for queries stopped by a cost limit; the web app shows them.
const (
	MsgQueryTimeout  = "This query took too long and was stopped. Try a shorter time range or more filters."
	MsgQueryTooLarge = "This query would read too much data. Try a shorter time range or more filters."
)

// WriteError writes err: an *Error as is, a ClickHouse cost limit as 504 or
// 422, anything else as a logged 500.
func WriteError(w http.ResponseWriter, err error) {
	var e *Error
	if errors.As(err, &e) {
		httpx.WriteError(w, e.Status, e.Message)
		return
	}
	switch ch.LimitError(err) {
	case ch.ErrQueryTimeout:
		slog.Error("query api: query stopped by time limit", "err", err)
		httpx.WriteError(w, http.StatusGatewayTimeout, MsgQueryTimeout)
		return
	case ch.ErrQueryTooLarge:
		slog.Error("query api: query stopped by size limit", "err", err)
		httpx.WriteError(w, http.StatusUnprocessableEntity, MsgQueryTooLarge)
		return
	}
	slog.Error("query api error", "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal error")
}

// StrictOptions are the error handlers for generated strict servers.
// badBody is the message for a request body that does not decode.
type StrictOptions struct {
	RequestErrorHandlerFunc  func(w http.ResponseWriter, r *http.Request, err error)
	ResponseErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

// Strict returns the options shared by every module's strict handler.
func Strict(badBody string) StrictOptions {
	return StrictOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			httpx.WriteError(w, http.StatusBadRequest, badBody)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteError(w, err)
		},
	}
}

// Project resolves the requested project for the logged-in user: the given
// id if they are a member, else their first project when none was given.
// It returns a 404 when the user cannot read it, so other projects'
// existence is not revealed.
func Project(ctx context.Context, p *gen.ProjectID) (uint64, error) {
	pr := auth.FromContext(ctx)
	if pr == nil {
		return 0, Err(http.StatusUnauthorized, "not logged in")
	}
	if p == nil {
		if len(pr.Projects) == 0 {
			return 0, Err(http.StatusNotFound, "no projects")
		}
		return uint64(pr.Projects[0].ID), nil
	}
	if !pr.Can(*p) {
		return 0, Err(http.StatusNotFound, "project not found")
	}
	return uint64(*p), nil
}

// Clock returns the current time; nil means time.Now.
type Clock func() time.Time

// Now returns the clock's time.
func (c Clock) Now() time.Time {
	if c != nil {
		return c()
	}
	return time.Now()
}

// TimeRange applies the default of the last 24 hours.
func (c Clock) TimeRange(from, to *time.Time) (time.Time, time.Time) {
	end := c.Now().UTC()
	if to != nil {
		end = *to
	}
	start := end.Add(-24 * time.Hour)
	if from != nil {
		start = *from
	}
	return start, end
}

// Limit clamps an optional page size to [1, max], defaulting to def.
func Limit(l *int, def, max int) int {
	if l == nil || *l < 1 {
		return def
	}
	if *l > max {
		return max
	}
	return *l
}

// StrPtr returns nil for an empty string.
func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ErrorOccurrence maps a stored error to its API shape.
func ErrorOccurrence(e ch.ErrorRecord) gen.ErrorOccurrence {
	return gen.ErrorOccurrence{
		Timestamp: e.Timestamp, IssueId: int64(e.IssueID), ExceptionType: e.ExceptionType, Message: e.Message,
		Stack: StrPtr(e.Stack), Culprit: StrPtr(e.Culprit), TraceId: e.TraceID, SpanId: e.SpanID,
		ServiceName: e.ServiceName, Platform: e.Platform, SessionId: StrPtr(e.SessionID),
		DistinctId: StrPtr(e.DistinctID), Url: StrPtr(e.URL), Release: StrPtr(e.Release),
	}
}

// ProductEvent maps a stored product event to its API shape.
func ProductEvent(e ch.EventRecord) gen.ProductEvent {
	props := map[string]interface{}{}
	_ = json.Unmarshal([]byte(e.Properties), &props)
	return gen.ProductEvent{
		Event: e.Event, Timestamp: e.Timestamp, DistinctId: e.DistinctID, SessionId: e.SessionID,
		SpanId: e.SpanID, Properties: props,
	}
}

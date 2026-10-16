// Package ports declares what the analytics module reads.
package ports

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Events lists raw product events and runs funnels.
type Events interface {
	ListEvents(ctx context.Context, f ch.EventFilter) ([]ch.ExploredEvent, error)
	EventNames(ctx context.Context, project uint64, from, to time.Time) ([]ch.EventNameCount, error)
	RunFunnel(ctx context.Context, q ch.FunnelQuery) (ch.FunnelData, error)
}

// Trends runs trend queries.
type Trends interface {
	RunTrend(ctx context.Context, q ch.TrendQuery) ([]ch.TrendSeriesData, error)
}

// People serves persons, retention and paths.
type People interface {
	ListPersons(ctx context.Context, f ch.PersonFilter) ([]ch.PersonRow, error)
	ResolvePerson(ctx context.Context, project uint64, distinctID string) (string, error)
	GetPerson(ctx context.Context, project uint64, person string, from, to time.Time) (ch.PersonRow, bool, error)
	PersonSessions(ctx context.Context, project uint64, person string, from, to time.Time, limit int) ([]ch.PersonSession, error)
	PersonEvents(ctx context.Context, project uint64, person string, from, to time.Time, limit int) ([]ch.EventRecord, error)
	RunRetention(ctx context.Context, q ch.RetentionQuery) (ch.RetentionData, error)
	RunPaths(ctx context.Context, q ch.PathsQuery) ([]ch.PathRow, error)
}

// Package ports declares what the issues module reads and writes.
package ports

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues/domain"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Repository stores issues.
type Repository interface {
	List(ctx context.Context, f domain.ListFilter) ([]domain.Issue, int64, error)
	Get(ctx context.Context, id int64) (domain.Issue, error)
	SetStatus(ctx context.Context, id int64, st domain.Status) (domain.Issue, error)
}

// Occurrences reads error occurrences and per-issue trends.
type Occurrences interface {
	ErrorsByIssue(ctx context.Context, project, issueID uint64, limit int) ([]ch.ErrorRecord, error)
	IssueTrends(ctx context.Context, project uint64, ids []uint64, now time.Time) (map[uint64][]int64, error)
}

// Outbox holds issue events until they are published.
type Outbox interface {
	RelayOutbox(ctx context.Context, limit int, send func(ctx context.Context, issueID int64, payload []byte) error) (int, error)
	PruneOutbox(ctx context.Context, cutoff time.Time) (int64, error)
}

// Producer publishes messages to a topic.
type Producer interface {
	Produce(ctx context.Context, topic string, key, value []byte) error
}

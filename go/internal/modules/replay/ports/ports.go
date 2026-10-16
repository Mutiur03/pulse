// Package ports declares what the replay module needs from storage.
package ports

import (
	"context"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Reader reads replay metadata from ClickHouse.
type Reader interface {
	ListReplays(ctx context.Context, f ch.ReplayFilter) ([]ch.ReplaySummary, error)
	ReplayChunks(ctx context.Context, project uint64, session string) ([]ch.ReplayChunk, error)
	ErrorsBySession(ctx context.Context, project uint64, session string) ([]ch.ErrorRecord, error)
	TracesBySession(ctx context.Context, project uint64, session string) ([]ch.SessionTrace, error)
}

// Objects reads stored chunks.
type Objects interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

package service

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues/domain"
	"github.com/Mutiur03/pulse/internal/modules/issues/ports"
)

// Relay publishes outbox events to Kafka.
type Relay struct {
	Outbox   ports.Outbox
	Producer ports.Producer
	// Batch is the most rows sent per transaction; Interval is the pause
	// when the outbox is empty or Kafka fails; Keep is how long sent rows
	// are kept.
	Batch    int
	Interval time.Duration
	Keep     time.Duration
}

// Run relays until ctx ends.
func (r *Relay) Run(ctx context.Context) {
	if r.Batch <= 0 {
		r.Batch = 100
	}
	if r.Interval <= 0 {
		r.Interval = time.Second
	}
	if r.Keep <= 0 {
		r.Keep = 7 * 24 * time.Hour
	}
	nextPrune := time.Time{}
	for ctx.Err() == nil {
		n, err := r.Once(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("issue outbox relay failed", "err", err)
		}
		if now := time.Now(); now.After(nextPrune) {
			if _, err := r.Outbox.PruneOutbox(ctx, now.Add(-r.Keep)); err != nil && ctx.Err() == nil {
				slog.Error("issue outbox prune failed", "err", err)
			}
			nextPrune = now.Add(time.Hour)
		}
		if err == nil && n == r.Batch {
			continue // more may be waiting
		}
		select {
		case <-ctx.Done():
		case <-time.After(r.Interval):
		}
	}
}

// Once sends one batch and returns how many events went out.
func (r *Relay) Once(ctx context.Context) (int, error) {
	return r.Outbox.RelayOutbox(ctx, r.Batch, func(ctx context.Context, issueID int64, payload []byte) error {
		return r.Producer.Produce(ctx, domain.Topic, []byte(strconv.FormatInt(issueID, 10)), payload)
	})
}

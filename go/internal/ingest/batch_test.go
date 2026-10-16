package ingest

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type batchProducer struct {
	fakeProducer
	batches []int
	fail    bool
}

func (b *batchProducer) ProduceBatch(_ context.Context, topic string, _, values [][]byte) error {
	if b.fail {
		return errors.New("broker down")
	}
	b.batches = append(b.batches, len(values))
	for _, v := range values {
		b.topics = append(b.topics, topic)
		b.vals = append(b.vals, string(v))
	}
	return nil
}

func TestProduceBatchSendsARequestAtOnce(t *testing.T) {
	p := &batchProducer{}
	h := NewHandler(p, &fakeAuth{})
	rec := do(t, h, http.MethodPost, "/v1/events", "application/json", `[{"event":"a"},{"event":"b"},{"event":"c"}]`)
	if rec.Code != http.StatusAccepted || len(p.batches) != 1 || p.batches[0] != 3 {
		t.Fatalf("%d batches %v", rec.Code, p.batches)
	}
	p.fail = true
	if rec = do(t, h, http.MethodPost, "/v1/events", "application/json", `[{"event":"a"}]`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed batch: %d", rec.Code)
	}
}

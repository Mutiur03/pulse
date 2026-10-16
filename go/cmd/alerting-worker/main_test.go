package main

import (
	"context"
	"errors"
	"testing"

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestRunRejectsBadAllowlist(t *testing.T) {
	cfg := config.Load()
	cfg.OutboundAllowlist = "not a cidr/99"
	if err := run(context.Background(), cfg); err == nil {
		t.Fatal("run accepted an invalid allowlist")
	}
}

type recorder struct{ got []issues.Event }

func (r *recorder) HandleIssueEvent(_ context.Context, ev issues.Event) error {
	r.got = append(r.got, ev)
	return errors.New("ignored")
}

type oneBatch struct {
	recs    []*kgo.Record
	cancel  context.CancelFunc
	commits int
}

func (o *oneBatch) Poll(context.Context) ([]*kgo.Record, error) {
	r := o.recs
	o.recs = nil
	return r, nil
}
func (o *oneBatch) Commit(context.Context) error { o.commits++; o.cancel(); return nil }

func TestConsumeIssueEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &oneBatch{cancel: cancel, recs: []*kgo.Record{
		{Value: []byte(`{"issue_id":7}`)},
		{Value: []byte(`garbage`)},
		{Value: []byte(`{"issue_id":8}`)},
	}}
	h := &recorder{}
	consumeIssueEvents(ctx, src, h)
	if len(h.got) != 2 || h.got[0].IssueID != 7 || h.got[1].IssueID != 8 || src.commits != 1 {
		t.Fatalf("events %+v commits %d", h.got, src.commits)
	}
}

package ch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func TestReadLimitsSettings(t *testing.T) {
	if s := (ReadLimits{}).Settings(); len(s) != 0 {
		t.Errorf("no limits should set nothing: %v", s)
	}
	s := ReadLimits{MaxExecutionTime: 20 * time.Second, MaxRowsToRead: 1000}.Settings()
	if s["max_execution_time"] != 20 || s["max_rows_to_read"] != uint64(1000) || s["timeout_overflow_mode"] != "throw" {
		t.Errorf("unexpected settings: %v", s)
	}
}

func TestLimitError(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{nil, nil},
		{errors.New("boom"), nil},
		{fmt.Errorf("funnel: %w", &clickhouse.Exception{Code: 159}), ErrQueryTimeout},
		{&clickhouse.Exception{Code: 158}, ErrQueryTooLarge},
		{&clickhouse.Exception{Code: 241}, ErrQueryTooLarge},
		{&clickhouse.Exception{Code: 60}, nil},
		{fmt.Errorf("read: %w", context.DeadlineExceeded), ErrQueryTimeout},
	}
	for _, c := range cases {
		if got := LimitError(c.err); got != c.want {
			t.Errorf("LimitError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

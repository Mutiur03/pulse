package main

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRepeatRunsImmediatelyAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	done := make(chan struct{})
	go func() {
		repeat(ctx, time.Millisecond, func() {
			n++
			if n == 3 {
				cancel()
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("repeat did not stop")
	}
	if n < 3 {
		t.Fatalf("ran %d times", n)
	}
}

func TestServeHealthReportsListenErrors(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := serveHealth(context.Background(), l.Addr().String()); err == nil {
		t.Fatal("expected an error for a busy port")
	}
}

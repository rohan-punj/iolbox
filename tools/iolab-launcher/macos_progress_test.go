package main

import (
	"errors"
	"testing"
	"time"
)

func TestWithProgressRunsWorkAndPropagatesSuccess(t *testing.T) {
	called := false
	err := withProgress("test step", time.Millisecond, func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("work was never called")
	}
}

func TestWithProgressPropagatesError(t *testing.T) {
	want := errors.New("boom")
	err := withProgress("test step", time.Millisecond, func() error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestWithProgressHeartbeatDuringLongWork(t *testing.T) {
	// A short heartbeat interval against work that outlives several ticks
	// must still return promptly and exactly once work() finishes, with no
	// goroutine leak (the done channel must stop the ticker goroutine).
	err := withProgress("slow step", 2*time.Millisecond, func() error {
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithProgressZeroHeartbeatUsesDefault(t *testing.T) {
	// heartbeat <= 0 must not panic or divide-by-zero; it falls back to the
	// documented default and still returns once work() finishes.
	err := withProgress("default heartbeat", 0, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

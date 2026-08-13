package server

import (
	"errors"
	"testing"
	"time"
)

func TestIsTransientFabricError(t *testing.T) {
	transient := []string{
		"fabric: ensure tap iol3_0: RTNETLINK answers: Device or resource busy",
		"ip: Resource temporarily unavailable",
		"ioctl: Try again",
	}
	for _, s := range transient {
		if !isTransientFabricError(errors.New(s)) {
			t.Errorf("isTransientFabricError(%q) = false, want true", s)
		}
	}
	permanent := []string{
		"fabric: ensure tap iol3_0: operation not permitted",
		"RTNETLINK answers: File exists",
		"sudo: a password is required",
		"exit status 1",
	}
	for _, s := range permanent {
		if isTransientFabricError(errors.New(s)) {
			t.Errorf("isTransientFabricError(%q) = true, want false", s)
		}
	}
	if isTransientFabricError(nil) {
		t.Error("isTransientFabricError(nil) = true")
	}
}

func TestRetryTransientFabricRetriesOnlyTransient(t *testing.T) {
	busy := errors.New("RTNETLINK answers: Device or resource busy")

	calls := 0
	if err := retryTransientFabric(func() error {
		calls++
		if calls < 3 {
			return busy
		}
		return nil
	}); err != nil {
		t.Fatalf("retryTransientFabric: %v", err)
	}
	if calls != 3 {
		t.Fatalf("attempts = %d, want 3", calls)
	}

	// A permanent error must surface immediately — no backoff spent on a
	// failure that will never clear.
	calls = 0
	permanent := errors.New("operation not permitted")
	if err := retryTransientFabric(func() error { calls++; return permanent }); !errors.Is(err, permanent) {
		t.Fatalf("err = %v, want the permanent error", err)
	}
	if calls != 1 {
		t.Fatalf("permanent error attempted %d times, want 1", calls)
	}

	// A transient error that never clears must give up at the budget and
	// return the last error, not loop.
	calls = 0
	start := time.Now()
	if err := retryTransientFabric(func() error { calls++; return busy }); !errors.Is(err, busy) {
		t.Fatalf("err = %v, want the transient error", err)
	}
	if calls != fabricRetryAttempts {
		t.Fatalf("attempts = %d, want %d", calls, fabricRetryAttempts)
	}
	// 25ms + 50ms of backoff, with generous slack for a loaded CI host.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("retry budget took %v; backoff is not bounded", elapsed)
	}
}

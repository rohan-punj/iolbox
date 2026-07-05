package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRunnable is a controllable runnable for lifecycleController tests: it
// blocks until either ctx is cancelled or a test-controlled channel is
// closed, and can be told to return a specific error, so tests never touch a
// real qemu/wsl backend.
type fakeRunnable struct {
	startedCh chan struct{} // closed once run() is entered
	exitCh    chan struct{} // closing this makes run() return immediately (in addition to ctx)
	retErr    error
}

func newFakeRunnable() *fakeRunnable {
	return &fakeRunnable{
		startedCh: make(chan struct{}),
		exitCh:    make(chan struct{}),
	}
}

func (f *fakeRunnable) run(ctx context.Context) error {
	close(f.startedCh)
	select {
	case <-ctx.Done():
		return nil
	case <-f.exitCh:
		return f.retErr
	}
}

func waitForState(t *testing.T, lc *lifecycleController, want backendState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if s, _ := lc.Status(); s == want {
			return
		}
		if time.Now().After(deadline) {
			s, _ := lc.Status()
			t.Fatalf("timed out waiting for state %q, last seen %q", want, s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLifecycleController_StartStop(t *testing.T) {
	fr := newFakeRunnable()
	lc := newLifecycleController(func() (runnable, error) { return fr, nil })

	if s, _ := lc.Status(); s != stateStopped {
		t.Fatalf("initial state = %q, want stopped", s)
	}

	if err := lc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-fr.startedCh
	waitForState(t, lc, stateRunning, time.Second)

	if err := lc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if s, _ := lc.Status(); s != stateStopped {
		t.Fatalf("state after Stop = %q, want stopped", s)
	}
}

func TestLifecycleController_StartWhileRunningErrors(t *testing.T) {
	fr := newFakeRunnable()
	lc := newLifecycleController(func() (runnable, error) { return fr, nil })

	if err := lc.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	<-fr.startedCh
	waitForState(t, lc, stateRunning, time.Second)

	if err := lc.Start(); err == nil {
		t.Fatal("expected second Start to fail while already running")
	}

	_ = lc.Stop()
}

func TestLifecycleController_StopWhenStoppedIsNoop(t *testing.T) {
	lc := newLifecycleController(func() (runnable, error) { return newFakeRunnable(), nil })
	if err := lc.Stop(); err != nil {
		t.Fatalf("Stop on a never-started controller should be a no-op, got: %v", err)
	}
}

func TestLifecycleController_BackendExitErrorSurfacesInStatus(t *testing.T) {
	fr := newFakeRunnable()
	wantErr := errors.New("boom")
	fr.retErr = wantErr
	lc := newLifecycleController(func() (runnable, error) { return fr, nil })

	if err := lc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-fr.startedCh
	close(fr.exitCh) // backend exits on its own with an error

	deadline := time.Now().Add(time.Second)
	for {
		s, err := lc.Status()
		if s == stateStopped && err != nil {
			if err.Error() != wantErr.Error() {
				t.Fatalf("Status error = %v, want %v", err, wantErr)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for stopped+error state, last: %q %v", s, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLifecycleController_RestartAfterStop(t *testing.T) {
	calls := 0
	lc := newLifecycleController(func() (runnable, error) {
		calls++
		return newFakeRunnable(), nil
	})

	if err := lc.Start(); err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	waitForState(t, lc, stateRunning, time.Second)
	if err := lc.Stop(); err != nil {
		t.Fatalf("Stop 1: %v", err)
	}

	if err := lc.Start(); err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	waitForState(t, lc, stateRunning, time.Second)
	if err := lc.Stop(); err != nil {
		t.Fatalf("Stop 2: %v", err)
	}

	if calls != 2 {
		t.Errorf("newBackend called %d times, want 2 (fresh backend built per Start)", calls)
	}
}

func TestLifecycleController_NewBackendError(t *testing.T) {
	wantErr := errors.New("cannot build backend")
	lc := newLifecycleController(func() (runnable, error) { return nil, wantErr })
	if err := lc.Start(); !errors.Is(err, wantErr) {
		t.Fatalf("Start error = %v, want %v", err, wantErr)
	}
	if s, _ := lc.Status(); s != stateStopped {
		t.Fatalf("state after failed Start = %q, want stopped", s)
	}
}

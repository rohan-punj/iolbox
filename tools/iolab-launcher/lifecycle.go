package main

import (
	"context"
	"fmt"
	"sync"
)

// lifecycle.go — the controllable-backend abstraction the control console
// (console.go) drives. Before this package existed, main() called
// backend.run(ctx) directly and blocked until Ctrl-C; the console needs to
// start/stop a backend from HTTP handlers instead, on demand, possibly more
// than once in the same process lifetime (stop, tweak settings, start again).
//
// runnable is the minimal shape both qemuBackend and wslBackend already
// satisfy (run(ctx) error) — no changes needed to either file.
type runnable interface {
	run(ctx context.Context) error
}

// backendState is the lifecycle state machine the console reports via
// /api/status and enforces in start()/stop() (e.g. start() is a no-op while
// already running or starting).
type backendState string

const (
	stateStopped  backendState = "stopped"
	stateStarting backendState = "starting"
	stateRunning  backendState = "running"
	stateStopping backendState = "stopping"
)

// lifecycleController owns at most one running backend at a time, guarded by
// a mutex so concurrent HTTP requests (start/stop/status) never race on the
// same backend instance. It is the seam mocked in console_test.go so handler
// tests never actually launch qemu or wsl.
type lifecycleController struct {
	mu    sync.Mutex
	state backendState
	err   error // last run() error, if the backend exited on its own or failed to start

	cancel context.CancelFunc // cancels the running backend's ctx; nil when stopped
	done   chan struct{}      // closed when the current run() goroutine returns

	// newBackend builds a fresh runnable for the CURRENT config each time
	// start() is called, so a deployment/cpu/ram change made via /api/config
	// takes effect on the next start without restarting the console process.
	newBackend func() (runnable, error)
}

func newLifecycleController(newBackend func() (runnable, error)) *lifecycleController {
	return &lifecycleController{
		state:      stateStopped,
		newBackend: newBackend,
	}
}

// Status reports the current state and last error (if any), for /api/status.
func (lc *lifecycleController) Status() (backendState, error) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.state, lc.err
}

// Start begins a new backend run in the background if one isn't already
// starting/running. Returns immediately — callers poll Status()/HTTP
// /api/status for progress, matching the "start is async" contract in the
// kickoff brief.
func (lc *lifecycleController) Start() error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.state == stateStarting || lc.state == stateRunning {
		return fmt.Errorf("already %s", lc.state)
	}

	b, err := lc.newBackend()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	lc.cancel = cancel
	lc.done = make(chan struct{})
	lc.state = stateStarting
	lc.err = nil

	done := lc.done
	go func() {
		runErr := b.run(ctx)
		lc.mu.Lock()
		lc.state = stateStopped
		lc.err = runErr
		lc.cancel = nil
		lc.mu.Unlock()
		close(done)
	}()

	// Best-effort: flip starting->running shortly after launch so /api/status
	// doesn't wedge on "starting" forever if a backend never signals
	// otherwise. Real "is it actually up" liveness is reported separately by
	// /api/status's gui.reachable probe (see console.go) — this state only
	// tracks "do we have a live backend process/goroutine", not GUI readiness.
	go func() {
		select {
		case <-done:
			// exited before we flipped the state; run() goroutine already set stopped.
		default:
			lc.mu.Lock()
			if lc.state == stateStarting {
				lc.state = stateRunning
			}
			lc.mu.Unlock()
		}
	}()

	return nil
}

// Stop cancels the running backend's context (triggering its own graceful
// shutdown path — QMP system_powerdown for qemu, wsl --terminate for wsl) and
// waits for its run() goroutine to return. A no-op if nothing is running.
func (lc *lifecycleController) Stop() error {
	lc.mu.Lock()
	if lc.state == stateStopped {
		lc.mu.Unlock()
		return nil
	}
	lc.state = stateStopping
	cancel := lc.cancel
	done := lc.done
	lc.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// StopAsync is like Stop but does not block the caller — used by the HTTP
// handler so a slow graceful shutdown (up to --shutdown-grace) doesn't hold
// the request open. Errors, if any, land in Status() once the stop completes.
func (lc *lifecycleController) StopAsync() {
	go lc.Stop()
}

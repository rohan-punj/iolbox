package main

import "time"

// withProgress runs work while emitting periodic heartbeat lines, so a
// human watching the Terminal window during a long-running step (VM boot,
// guest package install) can tell the launcher is still working rather than
// wondering whether it silently hung — the native-arm64 profile's
// UNMEASURED-CANARY-REQUIRED path in particular can run several guest steps
// back to back with no other output in between.
func withProgress(label string, heartbeat time.Duration, work func() error) error {
	start := time.Now()
	logf("==> %s...", label)

	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				logf("    still %s... (%s elapsed)", label, time.Since(start).Round(time.Second))
			}
		}
	}()

	err := work()
	close(done)
	if err != nil {
		return err
	}

	logf("==> %s: done (%s)", label, time.Since(start).Round(time.Second))
	return nil
}

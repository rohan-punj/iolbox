//go:build !linux

package tool

// SetSubreaper has no process-subtree equivalent on non-Linux hosts, so the
// portable supervisor can retain the same startup sequence without failing.
func SetSubreaper() error { return nil }

// StartReaper has no orphan-waiting kernel primitive on non-Linux hosts; the
// returned stop is intentionally idempotent and immediate.
func StartReaper(reg *PIDRegistry) (stop func()) {
	return func() {}
}

// ReapStale is a Linux cgroup/netns recovery operation. Non-Linux startup is
// deliberately a no-op because there are no corresponding objects to sweep.
func ReapStale(cfg ReapConfig) error { return nil }

//go:build !linux

package dirstat

// Open is a no-op on non-Linux: AF_PACKET raw sockets are Linux-only and the
// runtime is always Linux, so the dev-box build returns a nil Classifier (which
// Snapshot/Close both handle nil-safely). The aggregate fps/bps stats path is
// unaffected.
func Open(devs []string) (*Classifier, error) { return nil, nil }

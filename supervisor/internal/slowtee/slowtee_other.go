//go:build !linux

package slowtee

// open is a no-op on non-Linux: AF_PACKET raw sockets are Linux-only and the
// runtime is always Linux, so the dev-box build returns a nil Tee (Close
// handles nil-safely). The kernel bridge (and PAgP/static port-channels) are
// unaffected; only LACP passthrough is unavailable off Linux.
func open(devs []string) (*Tee, error) { return nil, nil }

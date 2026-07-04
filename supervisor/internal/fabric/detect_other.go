//go:build !linux

package fabric

// HasSudo is always false off Linux (no privileged data plane to gate).
func HasSudo() bool { return false }

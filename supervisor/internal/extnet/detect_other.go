//go:build !linux

package extnet

import "errors"

// DefaultRouteIface is unavailable off Linux (no /proc/net/route); the data
// plane only runs inside the Linux runtime.
func DefaultRouteIface() (string, error) {
	return "", errors.New("extnet: default-route detection is only supported on linux")
}

// Detect reports no support off Linux: nat needs a Linux tap data plane, so the
// hello handshake never advertises it on the dev box.
func Detect(sudoOK bool) Capabilities {
	return Capabilities{}
}

// SudoOK is always false off Linux (no privileged data plane to gate).
func SudoOK() bool { return false }

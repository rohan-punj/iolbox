//go:build !linux

package vtap

import "errors"

// ErrUnsupportedPlatform is returned by Start on non-Linux hosts. Tap devices
// (and the udp<->tap shim) only exist inside the Linux runtime; the pure
// pump logic (vtap.go) is platform-independent and is unit-tested on any OS
// via the fakes in shim_test.go.
var ErrUnsupportedPlatform = errors.New("vtap: udp<->tap shim is only supported on linux")

// Shim is the non-Linux stub so the package compiles and the pure-logic tests
// run on Windows/macOS dev boxes. It holds no live socket or tap fd.
type Shim struct{}

// Start always returns ErrUnsupportedPlatform on non-Linux hosts.
func Start(tapName string, bindPort, sendPort int) (*Shim, error) {
	return nil, ErrUnsupportedPlatform
}

// Close is a no-op on non-Linux hosts.
func (s *Shim) Close() error {
	return nil
}

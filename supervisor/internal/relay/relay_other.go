//go:build !linux

package relay

import "errors"

// ErrUnsupportedPlatform is returned when relay socket wiring is requested on a
// non-Linux host. The supervisor's data plane only runs inside the Linux
// runtime; the pure logic (pcapng writer, header stripping) is platform
// independent and lives in build-tag-free files for testing on any OS.
var ErrUnsupportedPlatform = errors.New("relay: UDP data plane is only supported on linux")

// newRelay is the non-Linux stub used so the package compiles and the pure
// logic tests run on Windows/macOS dev boxes.
func newRelay(cfg Config) (Relay, error) {
	return nil, ErrUnsupportedPlatform
}

//go:build !linux

package bcap

import "errors"

// ErrUnsupportedPlatform is returned by Start on non-Linux hosts. tcpdump-
// on-bridge capture only makes sense inside the Linux runtime; the pure
// parser/server logic (bcap.go) is platform-independent and is unit-tested
// on any OS via bcap_test.go.
var ErrUnsupportedPlatform = errors.New("bcap: bridge capture is only supported on linux")

// Capture is the non-Linux stub so the package compiles and the pure-logic
// tests run on Windows/macOS dev boxes. It holds no live process or server.
type Capture struct{}

// Start always returns ErrUnsupportedPlatform on non-Linux hosts.
func Start(bridgeName, bind string, port int) (*Capture, error) {
	return nil, ErrUnsupportedPlatform
}

// Port always returns 0 on non-Linux hosts.
func (c *Capture) Port() int { return 0 }

// Stats always returns zero values on non-Linux hosts.
func (c *Capture) Stats() (frames, bytesN uint64, protos map[string]uint64) {
	return 0, 0, nil
}

// Close is a no-op on non-Linux hosts.
func (c *Capture) Close() error { return nil }

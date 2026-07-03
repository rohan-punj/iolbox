//go:build !linux

package extnet

import "errors"

// ErrUnsupportedPlatform is returned when an external-net endpoint is started
// off Linux. tap/macvtap and the sudo-driven ip/iptables setup only work inside
// the runtime; the pure logic (subnet allocation, DHCP codec, feature gating)
// is tested on any OS.
var ErrUnsupportedPlatform = errors.New("extnet: tap/macvtap endpoints are only supported on linux")

// Endpoint is a placeholder off Linux so signatures resolve.
type Endpoint struct{}

// Start is a stub off Linux.
func Start(cfg Config) (*Endpoint, error) { return nil, ErrUnsupportedPlatform }

// Close is a no-op off Linux.
func (e *Endpoint) Close() error { return nil }

// Rebind is a no-op off Linux (endpoints never start there).
func (e *Endpoint) Rebind(sendPort, listenPort int) error { return nil }

// Ports is a stub off Linux.
func (e *Endpoint) Ports() (sendPort, listenPort int) { return 0, 0 }

//go:build !linux

package iouyap

import (
	"context"
	"errors"
)

// ErrUnsupportedPlatform is returned by New on non-Linux hosts. The netio
// unix-domain socket bridge only runs inside the Linux runtime; the pure
// framing/pump logic (header.go, iouyap.go) is platform-independent and is
// unit-tested on any OS via the fakes in iouyap_test.go.
var ErrUnsupportedPlatform = errors.New("iouyap: netio<->UDP bridge is only supported on linux")

// Bridge is the non-Linux stub so the package compiles and the pure-logic
// tests run on Windows/macOS dev boxes. It holds no live sockets.
type Bridge struct {
	cfg Config
}

// New validates cfg but returns ErrUnsupportedPlatform on non-Linux hosts.
func New(cfg Config) (*Bridge, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return nil, ErrUnsupportedPlatform
}

// Run always returns ErrUnsupportedPlatform on non-Linux hosts.
func (b *Bridge) Run(ctx context.Context) error {
	return ErrUnsupportedPlatform
}

// Close is a no-op on non-Linux hosts.
func (b *Bridge) Close() error {
	return nil
}

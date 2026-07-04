//go:build !linux

package iouyap

import (
	"context"
	"errors"
)

// ErrTapUnsupportedPlatform is returned by NewTap on non-Linux hosts. Tap
// devices (and the netio<->tap bridge) only exist inside the Linux runtime;
// the pure framing/pump logic (header.go, iouyap.go) is platform-independent
// and is unit-tested on any OS via the fakes in iouyap_test.go /
// bridge_tap_test.go.
var ErrTapUnsupportedPlatform = errors.New("iouyap: netio<->tap bridge is only supported on linux")

// TapBridge is the non-Linux stub so the package compiles and the pure-logic
// tests run on Windows/macOS dev boxes. It holds no live socket or tap fd.
type TapBridge struct {
	cfg Config
}

// NewTap validates cfg but returns ErrTapUnsupportedPlatform on non-Linux
// hosts.
func NewTap(cfg Config, tapName string) (*TapBridge, error) {
	if err := cfg.validateTap(); err != nil {
		return nil, err
	}
	return nil, ErrTapUnsupportedPlatform
}

// Run always returns ErrTapUnsupportedPlatform on non-Linux hosts.
func (b *TapBridge) Run(ctx context.Context) error {
	return ErrTapUnsupportedPlatform
}

// Close is a no-op on non-Linux hosts.
func (b *TapBridge) Close() error {
	return nil
}

//go:build !linux

package fabric

import (
	"context"
	"errors"
)

// errUnsupported is returned by every Manager method off Linux, so callers
// (and `go build`) work on windows/darwin even though the fabric only runs
// against a Linux runtime.
var errUnsupported = errors.New("fabric: tap/bridge management is only supported on linux")

// Manager is a no-op stub off Linux.
type Manager struct{}

// NewManager returns a stub Manager.
func NewManager() *Manager { return &Manager{} }

func (m *Manager) EnsureTap(ctx context.Context, name string, uid int) error {
	return errUnsupported
}

func (m *Manager) DeleteTap(ctx context.Context, name string) error {
	return errUnsupported
}

func (m *Manager) EnsureBridge(ctx context.Context, name string) error {
	return errUnsupported
}

func (m *Manager) DeleteBridge(ctx context.Context, name string) error {
	return errUnsupported
}

func (m *Manager) Attach(ctx context.Context, bridge, tap string) error {
	return errUnsupported
}

func (m *Manager) Detach(ctx context.Context, tap string) error {
	return errUnsupported
}

//go:build !linux

package node

import (
	"context"
	"errors"
	"io"
	"net"
)

// ErrUnsupportedPlatform is returned when node spawning is attempted off Linux.
// IOL is a Linux ELF binary; spawning only works inside the runtime. The pure
// logic (state machine, port allocation, argv building) is tested on any OS.
var ErrUnsupportedPlatform = errors.New("node: process spawning is only supported on linux")

// Process is a placeholder on non-Linux platforms so signatures resolve.
type Process struct {
	Spec    Spec
	Machine *Machine
}

// Spawn is a stub on non-Linux platforms.
func Spawn(spec Spec, m *Machine) (*Process, error) {
	return nil, ErrUnsupportedPlatform
}

// PID returns 0 on non-Linux platforms.
func (p *Process) PID() int { return 0 }

// Subscribe always returns nil on non-Linux platforms (no console hub exists
// off Linux — see the Linux Process doc comment).
func (p *Process) Subscribe() *Subscription { return nil }

// RunExec always fails on non-Linux platforms (no console hub exists off
// Linux — see the Linux Process doc comment).
func (p *Process) RunExec(ctx context.Context, holder, cmd string) (string, error) {
	return "", ErrUnsupportedPlatform
}

// Stop is a no-op on non-Linux platforms.
func (p *Process) Stop() error { return ErrUnsupportedPlatform }

// NewConsoleBridge is unavailable off Linux, but exists so server code keeps
// one buildable API across the supported and stub platforms.
func NewConsoleBridge(_ io.ReadWriter, _ string, _ net.Listener) *Process { return &Process{} }

//go:build !linux

package node

import "errors"

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

// Stop is a no-op on non-Linux platforms.
func (p *Process) Stop() error { return ErrUnsupportedPlatform }

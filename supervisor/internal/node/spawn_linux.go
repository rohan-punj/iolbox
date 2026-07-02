//go:build linux

package node

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// Process is a spawned node process with its lifecycle state.
type Process struct {
	Spec    Spec
	Machine *Machine

	mu  sync.Mutex
	cmd *exec.Cmd
}

// Spawn launches the node described by spec. It sets the process working
// directory to spec.WorkDir (so IOL finds NETMAP + iourc there) and the
// environment from spec.Environ. On IOL it uses IOLArgv; on VPCS, VPCSArgv.
//
// The state machine goes stopped->starting immediately; a background waiter
// moves it to crashed on unexpected exit. The caller flips starting->running
// once it confirms the console is reachable.
func Spawn(spec Spec, m *Machine) (*Process, error) {
	var argv []string
	var env []string
	switch spec.Kind {
	case "iol":
		argv = spec.IOLArgv()
		env = append(os.Environ(), spec.Environ()...)
	case "vpcs":
		var err error
		argv, err = spec.VPCSArgv(fmt.Sprintf("pc%d", spec.NodeID))
		if err != nil {
			return nil, err
		}
		env = os.Environ()
	default:
		return nil, fmt.Errorf("node: unknown kind %q", spec.Kind)
	}

	if err := os.MkdirAll(spec.WorkDir, 0o755); err != nil {
		return nil, fmt.Errorf("node %d: workdir: %w", spec.NodeID, err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = spec.WorkDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if !m.To(StateStarting) {
		return nil, fmt.Errorf("node %d: not in a startable state", spec.NodeID)
	}
	if err := cmd.Start(); err != nil {
		m.To(StateCrashed)
		return nil, fmt.Errorf("node %d: spawn: %w", spec.NodeID, err)
	}

	p := &Process{Spec: spec, Machine: m, cmd: cmd}
	go p.wait()
	return p, nil
}

// wait reaps the process and updates state on exit.
func (p *Process) wait() {
	err := p.cmd.Wait()
	// If we deliberately stopped it, state is already/soon Stopped; only mark
	// crashed when still running/starting.
	switch p.Machine.State() {
	case StateStopped:
		// expected shutdown
	default:
		if err != nil {
			p.Machine.To(StateCrashed)
		} else {
			p.Machine.To(StateStopped)
		}
	}
}

// PID returns the OS process id, or 0 if not started.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Stop terminates the process and marks the node stopped.
func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	p.Machine.To(StateStopped)
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	return nil
}

// Package node manages IOL and VPCS processes: argv construction, console port
// allocation, the node state machine, and (on Linux) the actual spawning.
//
// Pure logic — the state machine, port allocation, and argv building — is
// platform independent and tested on any OS. Process spawning is behind
// //go:build linux with a stub elsewhere.
package node

import "sync"

// State is a node lifecycle state (see docs/protocol.md).
type State string

const (
	// StateStopped means the node is not running.
	StateStopped State = "stopped"
	// StateStarting means the node is launching but not yet ready.
	StateStarting State = "starting"
	// StateRunning means the node is up and its console is reachable.
	StateRunning State = "running"
	// StateCrashed means the process exited unexpectedly.
	StateCrashed State = "crashed"
)

// validTransitions encodes the allowed state machine edges:
//
//	stopped  -> starting
//	starting -> running | crashed | stopped
//	running  -> stopped | crashed
//	crashed  -> starting | stopped
var validTransitions = map[State]map[State]bool{
	StateStopped:  {StateStarting: true},
	StateStarting: {StateRunning: true, StateCrashed: true, StateStopped: true},
	StateRunning:  {StateStopped: true, StateCrashed: true},
	StateCrashed:  {StateStarting: true, StateStopped: true},
}

// CanTransition reports whether from->to is a legal state machine edge.
func CanTransition(from, to State) bool {
	return validTransitions[from][to]
}

// Machine is a concurrency-safe holder of a node's current state, invoking an
// optional callback on every accepted transition (used to emit node.state
// events).
type Machine struct {
	mu      sync.Mutex
	state   State
	onEnter func(State)
}

// NewMachine returns a Machine starting in StateStopped.
func NewMachine(onEnter func(State)) *Machine {
	return &Machine{state: StateStopped, onEnter: onEnter}
}

// State returns the current state.
func (m *Machine) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// To attempts a transition. It returns true if the edge was legal and applied.
// The onEnter callback runs (outside the lock) on success.
func (m *Machine) To(to State) bool {
	m.mu.Lock()
	if !CanTransition(m.state, to) {
		m.mu.Unlock()
		return false
	}
	m.state = to
	cb := m.onEnter
	m.mu.Unlock()
	if cb != nil {
		cb(to)
	}
	return true
}

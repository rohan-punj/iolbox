package relay

import (
	"fmt"
	"sync"
)

// UDPEndpoint is one side of an IOL/VPCS UDP tunnel: the local port the relay
// binds to receive from a node, and the remote port the node listens on to
// receive frames the relay sends it. Host is normally 127.0.0.1 because IOL,
// VPCS and the supervisor share the runtime.
type UDPEndpoint struct {
	Host string
	// LocalPort is where the relay receives frames from this node.
	LocalPort int
	// RemotePort is where this node receives frames the relay forwards to it.
	RemotePort int
}

// Kind selects the relay behaviour for a link.
type Kind int

const (
	// KindP2P bidirectionally forwards between exactly two endpoints.
	KindP2P Kind = iota
	// KindHub floods every received frame to all other members.
	KindHub
)

// Config describes one link's relay: its kind, members, and optional capture.
type Config struct {
	LinkID    int
	Kind      Kind
	Endpoints []UDPEndpoint
	// CapturePort, when > 0, requests the tee to listen for a Wireshark client.
	CapturePort int
}

// Relay is the interface a running link relay satisfies. The concrete
// implementation is platform-specific (linux binds real UDP sockets); other
// platforms get a no-op so the package builds and the pure logic tests run.
type Relay interface {
	LinkID() int
	CapturePort() int
	Close() error
}

// Manager tracks active relays by link id. It is safe for concurrent use.
type Manager struct {
	mu     sync.Mutex
	relays map[int]Relay
}

// NewManager returns an empty relay manager.
func NewManager() *Manager {
	return &Manager{relays: make(map[int]Relay)}
}

// Start creates and registers a relay for cfg. It fails if a relay already
// exists for the link id. The actual socket wiring is platform-specific.
func (m *Manager) Start(cfg Config) (Relay, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.relays[cfg.LinkID]; exists {
		return nil, fmt.Errorf("relay: link %d already active", cfg.LinkID)
	}
	r, err := newRelay(cfg)
	if err != nil {
		return nil, err
	}
	m.relays[cfg.LinkID] = r
	return r, nil
}

// Stop closes and unregisters the relay for a link id, if present.
func (m *Manager) Stop(linkID int) error {
	m.mu.Lock()
	r, ok := m.relays[linkID]
	delete(m.relays, linkID)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return r.Close()
}

// StopAll closes every active relay.
func (m *Manager) StopAll() {
	m.mu.Lock()
	relays := m.relays
	m.relays = make(map[int]Relay)
	m.mu.Unlock()
	for _, r := range relays {
		_ = r.Close()
	}
}

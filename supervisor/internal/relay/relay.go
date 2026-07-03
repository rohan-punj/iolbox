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
	// CaptureBind is the host the pcapng tee listener binds. Empty defaults to
	// loopback (the wsbridge always dials via loopback). Set 0.0.0.0 (supervisor
	// -capture-bind) so a native Wireshark on the GUI host can attach directly
	// with `wireshark -k -i TCP@<vm-ip>:<capturePort>` — same trust boundary as
	// -console-bind / -ws-addr.
	CaptureBind string
}

// Relay is the interface a running link relay satisfies. The concrete
// implementation is platform-specific (linux binds real UDP sockets); other
// platforms get a no-op so the package builds and the pure logic tests run.
type Relay interface {
	LinkID() int
	CapturePort() int
	// Stats returns cumulative counters of datagrams FORWARDED by this relay
	// since it started, summed across both directions (a P2P frame forwarded
	// to the peer counts once; a hub frame flooded to N members counts N). The
	// server polls these on a ticker to derive per-link throughput without the
	// relay package importing server. Monotonic; safe for concurrent reads.
	Stats() (frames, bytes uint64)
	// ProtoStats returns cumulative per-protocol forwarded-frame counters keyed
	// by protocol label (see Classify), a fresh copy safe to keep and diff. The
	// server diffs two snapshots per interval to derive per-proto fps for the
	// link.stats breakdown. Counts sum to the frames returned by Stats. May be
	// nil/empty when no traffic has been forwarded yet.
	ProtoStats() map[string]uint64
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

// LinkStats is a snapshot of one relay's cumulative forwarded counters.
type LinkStats struct {
	LinkID int
	Frames uint64
	Bytes  uint64
	// Protos is the cumulative per-protocol forwarded-frame count keyed by
	// protocol label (see Classify). nil/empty when no traffic yet. The server
	// diffs consecutive snapshots to derive per-proto fps.
	Protos map[string]uint64
}

// Stats snapshots the cumulative forwarded counters of every active relay,
// keyed by link id. The server polls this on a ticker to emit per-link
// throughput events; relays with no traffic yet report zero. The returned map
// is a fresh copy safe to iterate without holding the manager lock.
func (m *Manager) Stats() map[int]LinkStats {
	m.mu.Lock()
	relays := make([]Relay, 0, len(m.relays))
	for _, r := range m.relays {
		relays = append(relays, r)
	}
	m.mu.Unlock()
	out := make(map[int]LinkStats, len(relays))
	for _, r := range relays {
		frames, bytes := r.Stats()
		out[r.LinkID()] = LinkStats{LinkID: r.LinkID(), Frames: frames, Bytes: bytes, Protos: r.ProtoStats()}
	}
	return out
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

// Package extnet implements the "outside world" node kind — nat — as a
// supervisor-internal endpoint. A "nat" node owns a tap device with a private
// gateway address (172.31.<n>.1/24), runs a minimal DHCP server on it, and NATs
// the pool out the VM's default route so lab nodes can `ip dhcp` and reach the
// internet. The tap joins the link's Linux bridge (br-<linkid>), so the kernel
// switches lab frames to it directly.
//
// The pure logic here — subnet-index allocation and DHCP packet encode/decode —
// is platform independent and unit-tested on any OS. The privileged data plane
// (tap fds, ip/iptables/sysctl via sudo) lives behind //go:build linux with a
// stub elsewhere, mirroring internal/node.
package extnet

import (
	"fmt"
	"sync"
)

// Kind is the external-net node kind this package realises.
type Kind string

const (
	// KindNAT is a NAT gateway node (tap + DHCP + MASQUERADE).
	KindNAT Kind = "nat"
)

// MaxSubnetIndex is the largest per-nat subnet index. The gateway address is
// 172.31.<n>.1/24, so n must fit the third octet (0..255). Index 0 is reserved
// (172.31.0.0/24) so the first allocated nat node uses 172.31.1.0/24, keeping
// the addresses readable.
const MaxSubnetIndex = 255

// SubnetAllocator hands out distinct per-nat subnet indices (the <n> in
// 172.31.<n>.0/24) so multiple nat nodes in one lab never collide. It is safe
// for concurrent use. Indices start at 1 and are released on teardown so a
// long-lived supervisor does not exhaust the /16.
type SubnetAllocator struct {
	mu    sync.Mutex
	taken map[int]bool
}

// NewSubnetAllocator returns an empty allocator.
func NewSubnetAllocator() *SubnetAllocator {
	return &SubnetAllocator{taken: make(map[int]bool)}
}

// Next returns the lowest free subnet index (>=1) and marks it taken. It errors
// only when every index in [1,MaxSubnetIndex] is in use.
func (a *SubnetAllocator) Next() (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for n := 1; n <= MaxSubnetIndex; n++ {
		if !a.taken[n] {
			a.taken[n] = true
			return n, nil
		}
	}
	return 0, fmt.Errorf("extnet: subnet index pool [1,%d] exhausted", MaxSubnetIndex)
}

// Reserve marks a specific index taken (used when re-adopting an index across a
// plan rebuild so the same nat node keeps its address). It errors if already
// taken.
func (a *SubnetAllocator) Reserve(n int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n < 1 || n > MaxSubnetIndex {
		return fmt.Errorf("extnet: subnet index %d out of range [1,%d]", n, MaxSubnetIndex)
	}
	if a.taken[n] {
		return fmt.Errorf("extnet: subnet index %d already reserved", n)
	}
	a.taken[n] = true
	return nil
}

// Release frees a previously allocated index.
func (a *SubnetAllocator) Release(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.taken, n)
}

// Subnet describes the addressing derived from a nat node's subnet index.
type Subnet struct {
	Index int // the <n> in 172.31.<n>.0/24
}

// GatewayIP returns the tap/gateway address (172.31.<n>.1) as a dotted string.
func (s Subnet) GatewayIP() string { return fmt.Sprintf("172.31.%d.1", s.Index) }

// CIDR returns the gateway address with its /24 prefix (172.31.<n>.1/24), the
// form `ip addr add` wants.
func (s Subnet) CIDR() string { return fmt.Sprintf("172.31.%d.1/24", s.Index) }

// Network returns the network CIDR (172.31.<n>.0/24), the form iptables
// MASQUERADE/FORWARD rules match on.
func (s Subnet) Network() string { return fmt.Sprintf("172.31.%d.0/24", s.Index) }

// PoolStart and PoolEnd bound the DHCP pool (172.31.<n>.100 .. .199), leaving
// .1 for the gateway and .2-.99 / .200-.254 free for static assignments.
func (s Subnet) PoolStart() string { return fmt.Sprintf("172.31.%d.100", s.Index) }
func (s Subnet) PoolEnd() string   { return fmt.Sprintf("172.31.%d.199", s.Index) }

// tapName returns the kernel device name for a nat node's tap. Bounded to the
// 15-char IFNAMSIZ limit: "iolnat" + up to a few digits fits.
func tapName(nodeID int) string { return fmt.Sprintf("iolnat%d", nodeID) }

// Config describes one external-net endpoint to bring up. The server fills it
// from the lab node + the node's relay endpoint (the same UDP port pairing VPCS
// uses: the endpoint SENDS frames to SendPort — the relay's receiving LocalPort
// — and LISTENS on ListenPort — the relay's delivery RemotePort).
type Config struct {
	Kind   Kind
	NodeID int

	// SendPort/ListenPort tie this endpoint to its link's relay, exactly like
	// VPCS: SendPort is the relay's receiving LocalPort (we send tap frames to
	// it); ListenPort is the relay's delivery RemotePort (we bind it to receive
	// frames the relay forwards, then write them into the tap).
	SendPort   int
	ListenPort int
	// Host is the relay host; empty defaults to 127.0.0.1.
	Host string

	// SubnetIndex is the <n> in 172.31.<n>.0/24 (nat only), allocated by the
	// server so multiple nat nodes never collide.
	SubnetIndex int
	// DefaultIface is the VM's default-route interface, out which nat MASQUERADEs
	// (nat only). Resolved by the server via DefaultRouteIface.
	DefaultIface string

	// Bridged selects the P2 static-tap bridge-fabric data plane (nat only): the
	// tap is created unbridged at Start with NO relay/UDP pumps, the DHCP server
	// runs directly on the tap fd, and AttachBridge/DetachBridge wire the gateway
	// + NAT onto the link bridge when the link is drawn/removed. When false the
	// endpoint uses the legacy UDP-relay pumps (mgmt, and nat until migrated).
	Bridged bool
}

// resolvedHost returns Config.Host, defaulting to loopback.
func (c Config) resolvedHost() string {
	if c.Host == "" {
		return "127.0.0.1"
	}
	return c.Host
}

// Capabilities reports which external-net node kinds the runtime supports,
// detected at server startup (Detect on Linux, all-false off Linux). The server
// advertises the corresponding hello features and rejects lab.start of an
// unsupported kind.
type Capabilities struct {
	NAT bool // nat gateway nodes are supported
}

// GateFeatures returns the hello feature strings to advertise for the supported
// kinds ("natgw" for NAT), in a stable order. Pure so feature gating is
// unit-testable with an injected Capabilities value.
func (c Capabilities) GateFeatures() []string {
	var out []string
	if c.NAT {
		out = append(out, "natgw")
	}
	return out
}

// Supports reports whether the runtime can run a node of the given kind.
func (c Capabilities) Supports(kind Kind) bool {
	switch kind {
	case KindNAT:
		return c.NAT
	default:
		return false
	}
}

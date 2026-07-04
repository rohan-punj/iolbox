// Package netmap implements IOL interface addressing and NETMAP file generation.
//
// IOL numbers interfaces as adapter/port pairs written like "e0/0" or "s1/2".
// Internally each interface also maps to a single flat port index (used by the
// UDP/iouyap bridge path):
//
//	port = adapter*16 + portInAdapter
//
// The NETMAP file wires nodes together. Each line describes one connection:
//
//	<a-nodeid>:<a-adapter>/<a-port> <b-nodeid>:<b-adapter>/<b-port>
//
// where the node id is the lab node.id and the interface token is IOL's own
// "adapter/port" form (e0/0 -> 0/0, s1/2 -> 1/2). This is the exact token format
// the P0 manual test proved carries traffic between two real IOL instances
// (NETMAP line "1:0/0 2:0/0" brought both Ethernet0/0 line protocols up). IOL
// reads this file from its current working directory and connects same-directory
// instances directly over unix-socket netio (no UDP relay).
//
// NOTE: this NETMAP form intentionally differs from the flat-index encoding
// documented in an early draft of docs/protocol.md; P0 corrected it to the
// adapter/port token that real IOL 17.18.02 actually accepts.
package netmap

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PortsPerAdapter is the number of ports in one IOL adapter group.
const PortsPerAdapter = 16

// PortsPerGroup is the number of interfaces IOL actually exposes per adapter
// group (confirmed on real IOL 17.18.02: launching with "-e 1" produced
// exactly e0/0..e0/3, and the default serial group count produced s0/0..s0/3
// and s1/0..s1/3 — 4 ports per group). This is distinct from PortsPerAdapter
// (16), which is only the flat-index stride used by Iface.Index() for the
// UDP/iouyap bridge path; IOL itself never populates ports 4..15 of a group.
const PortsPerGroup = 4

// MaxIOLInstance is IOL's highest valid instance id. IOL accepts instance ids
// 1..1024 (it refuses 0), so a lab node.id (schema minimum 0) must be mapped to
// this range before it is used as the IOL instance id.
const MaxIOLInstance = 1024

// InstanceID maps a lab node.id to the IOL instance id used consistently in the
// argv positional, the NETMAP node id, and the nvram_<id> filename. IOL rejects
// instance id 0, so the mapping is nodeID+1 (node 0 -> instance 1). All three
// call sites MUST use this one helper so argv/NETMAP/NVRAM stay in sync.
func InstanceID(nodeID int) int {
	return nodeID + 1
}

// PseudoInstanceBase is the low end of the reserved pseudo-instance pool used to
// wire BRIDGED IOL endpoints (capture / VPCS / cross-host). A bridged IOL
// endpoint's NETMAP entry points at a pseudo-instance that iouyap owns (it binds
// /tmp/netio<uid>/<pseudoInstance>) instead of a peer IOL's real instance. The
// pool starts high (500) so it clears the real-instance range in every practical
// lab: real nodes use InstanceID(nodeID)=nodeID+1, and a lab dense enough to
// reach node id 499 (real instance 500) is far beyond any single-host lab. When
// real instances DO reach into the pool, AllocPseudoInstances skips them, so the
// scheme stays collision-free up to MaxIOLInstance regardless.
const PseudoInstanceBase = 500

// AllocPseudoInstances returns n distinct pseudo-instance ids in
// [PseudoInstanceBase, MaxIOLInstance] that do NOT collide with any id in
// realInstances (the set of real-node IOL instance ids already in use). Ids are
// handed out in ascending order for deterministic NETMAP output. It errors if
// the pool cannot satisfy n ids without colliding.
//
// realInstances holds IOL *instance* ids (InstanceID(nodeID)), not raw node ids.
// Callers build it from every IOL node in the lab so a pseudo-instance can never
// alias a running IOL's netio socket path.
func AllocPseudoInstances(realInstances map[int]bool, n int) ([]int, error) {
	out := make([]int, 0, n)
	for id := PseudoInstanceBase; id <= MaxIOLInstance && len(out) < n; id++ {
		if realInstances[id] {
			continue
		}
		out = append(out, id)
	}
	if len(out) < n {
		return nil, fmt.Errorf("pseudo-instance pool [%d,%d] exhausted: need %d, got %d (too many bridged endpoints)", PseudoInstanceBase, MaxIOLInstance, n, len(out))
	}
	return out, nil
}

// ValidateInstance checks that a lab node.id maps to an IOL instance id within
// IOL's 1..MaxIOLInstance range. It returns a descriptive error otherwise, for
// use at lab.load.
func ValidateInstance(nodeID int) error {
	inst := InstanceID(nodeID)
	if inst < 1 || inst > MaxIOLInstance {
		return fmt.Errorf("node id %d maps to IOL instance id %d, outside the valid 1..%d range", nodeID, inst, MaxIOLInstance)
	}
	return nil
}

// IfaceType classifies an IOL interface as Ethernet or Serial.
type IfaceType int

const (
	// Ethernet is an "e" interface.
	Ethernet IfaceType = iota
	// Serial is an "s" interface.
	Serial
)

// String renders the interface type prefix.
func (t IfaceType) String() string {
	if t == Serial {
		return "s"
	}
	return "e"
}

// Iface is a parsed IOL interface: type + adapter/port coordinates.
type Iface struct {
	Type    IfaceType
	Adapter int
	Port    int
}

// Index returns the flat IOL port index: adapter*16 + port.
func (i Iface) Index() int {
	return i.Adapter*PortsPerAdapter + i.Port
}

// String renders the canonical interface name, e.g. "e0/0" or "s1/2".
func (i Iface) String() string {
	return fmt.Sprintf("%s%d/%d", i.Type, i.Adapter, i.Port)
}

// ParseIface parses an IOL interface string like "e0/0", "Ethernet0/1",
// "s1/2", or "Serial0/3". The port-within-adapter must be 0..15.
func ParseIface(s string) (Iface, error) {
	orig := s
	s = strings.TrimSpace(s)
	if s == "" {
		return Iface{}, fmt.Errorf("empty interface")
	}
	lower := strings.ToLower(s)

	var t IfaceType
	switch {
	case strings.HasPrefix(lower, "ethernet"):
		t, lower = Ethernet, lower[len("ethernet"):]
	case strings.HasPrefix(lower, "serial"):
		t, lower = Serial, lower[len("serial"):]
	case strings.HasPrefix(lower, "e"):
		t, lower = Ethernet, lower[1:]
	case strings.HasPrefix(lower, "s"):
		t, lower = Serial, lower[1:]
	default:
		return Iface{}, fmt.Errorf("interface %q: unknown type prefix", orig)
	}

	slash := strings.IndexByte(lower, '/')
	if slash < 0 {
		return Iface{}, fmt.Errorf("interface %q: expected adapter/port", orig)
	}
	adapter, err := strconv.Atoi(lower[:slash])
	if err != nil {
		return Iface{}, fmt.Errorf("interface %q: bad adapter: %w", orig, err)
	}
	port, err := strconv.Atoi(lower[slash+1:])
	if err != nil {
		return Iface{}, fmt.Errorf("interface %q: bad port: %w", orig, err)
	}
	if adapter < 0 || port < 0 {
		return Iface{}, fmt.Errorf("interface %q: negative coordinate", orig)
	}
	if port >= PortsPerAdapter {
		return Iface{}, fmt.Errorf("interface %q: port %d exceeds %d per adapter", orig, port, PortsPerAdapter-1)
	}
	return Iface{Type: t, Adapter: adapter, Port: port}, nil
}

// IfacesForCounts returns every interface a node with ethGroups Ethernet
// adapter groups and serialGroups Serial adapter groups exposes at boot, in
// deterministic order: all Ethernet interfaces first (adapter-major,
// port-minor), then all Serial interfaces (same order). Each group contributes
// exactly PortsPerGroup (4) interfaces, ports 0..PortsPerGroup-1, matching
// IOL's real boot-time behavior (see PortsPerGroup doc comment). A
// non-positive count for either type yields no interfaces of that type.
//
// Examples: IfacesForCounts(1, 0) -> [e0/0, e0/1, e0/2, e0/3].
// IfacesForCounts(2, 1) -> e0/0..e0/3, e1/0..e1/3, s0/0..s0/3.
func IfacesForCounts(ethGroups, serialGroups int) []Iface {
	var out []Iface
	for a := 0; a < ethGroups; a++ {
		for p := 0; p < PortsPerGroup; p++ {
			out = append(out, Iface{Type: Ethernet, Adapter: a, Port: p})
		}
	}
	for a := 0; a < serialGroups; a++ {
		for p := 0; p < PortsPerGroup; p++ {
			out = append(out, Iface{Type: Serial, Adapter: a, Port: p})
		}
	}
	return out
}

// StaticEntry is one interface's static NETMAP wiring: a node's own interface
// pointed at its own fixed pseudo-instance (its own tap), independent of
// whether any link has been drawn to it. This is the fix for the
// topology-dependent bug in Build: IOL reads NETMAP once at boot, so every
// interface needs a line at boot time, not just the ones with a link already
// drawn. "Drawing a link" later becomes a runtime bridge-attach between two
// pseudo-instances' taps, handled outside this package.
//
// InstanceID is the IOL *instance* id (already InstanceID(nodeID), see the
// InstanceID helper), NOT a raw lab node id — BuildStatic is a pure formatter
// and does not perform that mapping itself, so callers must pass instance ids.
type StaticEntry struct {
	// InstanceID is the IOL instance id that owns Iface (InstanceID(nodeID)).
	InstanceID int
	// Iface is the node's own interface this line wires.
	Iface Iface
	// PseudoInstance is the reserved instance id that owns this interface's
	// own tap (iouyap binds /tmp/netio<uid>/<PseudoInstance>).
	PseudoInstance int
}

// staticLine renders one static NETMAP line:
//
//	<InstanceID>:<adapter>/<port> <PseudoInstance>:0/0
//
// This wires one interface at boot regardless of links (the topology-independent
// NETMAP that makes hot-connect work). e.InstanceID is used as-is (it is already
// an IOL instance id, not a raw node id needing the InstanceID(nodeID) mapping).
func staticLine(e StaticEntry) string {
	return fmt.Sprintf("%d:%d/%d %d:0/0", e.InstanceID, e.Iface.Adapter, e.Iface.Port, e.PseudoInstance)
}

// BuildStatic produces the static NETMAP file content from a flat list of
// StaticEntry: one line per entry, in the
// "<InstanceID>:<adapter>/<port> <PseudoInstance>:0/0" format IOL accepts,
// sorted for deterministic output. Returns "" for no entries.
func BuildStatic(entries []StaticEntry) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, staticLine(e))
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

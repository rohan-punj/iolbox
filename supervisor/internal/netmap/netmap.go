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

// Entry is one endpoint of a NETMAP connection.
type Entry struct {
	NodeID int
	Iface  Iface
}

// String renders "<nodeid>:<adapter>/<port>", the IOL NETMAP interface token
// (e.g. "2:0/0" for node 2 Ethernet0/0). This is the format real IOL accepts;
// see the package doc and docs/p0-spike.md.
func (e Entry) String() string {
	return fmt.Sprintf("%d:%d/%d", e.NodeID, e.Iface.Adapter, e.Iface.Port)
}

// LinkSpec is the minimal link description Build needs: whether it is a direct
// p2p pairing and its endpoints. This keeps netmap free of a dependency on the
// lab package (avoiding an import cycle, since lab.Validate uses ParseIface).
type LinkSpec struct {
	// P2P is true for a direct IOL-to-IOL pairing; segment links are false
	// because the userspace hub handles flooding (no NETMAP line).
	P2P bool
	// Endpoints are (nodeID, interface) pairs.
	Endpoints []EndpointSpec
}

// EndpointSpec is one side of a LinkSpec.
type EndpointSpec struct {
	NodeID int
	// Interface is the raw IOL interface string, e.g. "e0/0".
	Interface string
	// IsIOL marks IOL endpoints; only IOL-to-IOL pairs produce NETMAP lines.
	IsIOL bool
}

// Build produces the NETMAP file content from link specs. Only IOL-to-IOL p2p
// pairings appear; VPCS endpoints and segment links are wired by the relay
// layer instead. Lines are sorted for deterministic output.
func Build(links []LinkSpec) string {
	var lines []string
	for _, link := range links {
		var iol []Entry
		for _, ep := range link.Endpoints {
			if !ep.IsIOL {
				continue
			}
			ifc, err := ParseIface(ep.Interface)
			if err != nil {
				continue // validation catches this earlier
			}
			iol = append(iol, Entry{NodeID: ep.NodeID, Iface: ifc})
		}
		if link.P2P && len(iol) == 2 {
			lines = append(lines, iol[0].String()+" "+iol[1].String())
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

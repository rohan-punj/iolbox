package server

import (
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
)

// linkWiring classifies how a single lab link is realized in the data plane.
//
// P0 proved two distinct paths:
//
//   - Native NETMAP netio: two same-host IOL instances sharing a directory with
//     a NETMAP line connect directly over unix-socket netio — no UDP relay, no
//     supervisor in the data path. This is the fast, default path for plain
//     same-host IOL<->IOL point-to-point links.
//
//   - Bridged (iouyap + UDP relay): a link that needs packet capture, that
//     involves a VPCS (or any non-IOL) endpoint, or that spans hosts CANNOT use
//     native netio. Those links must be routed through the netio<->UDP bridge
//     (internal/iouyap, built in parallel — NOT imported here) fronting the UDP
//     relay/pcapng tee.
//
// This type is the single seam where that decision is made. See wiringFor.
type linkWiring int

const (
	// wiringNative = same-host IOL<->IOL, realized purely via the whole-lab
	// NETMAP file (netmap.Build emits a line for it). No relay/bridge.
	wiringNative linkWiring = iota
	// wiringBridged = capture / VPCS / cross-host: must go through the
	// iouyap netio<->UDP bridge + UDP relay. NOT wired natively.
	wiringBridged
)

func (w linkWiring) String() string {
	if w == wiringBridged {
		return "bridged"
	}
	return "native"
}

// wiringFor decides whether a link is realized natively (whole-lab NETMAP) or
// must be bridged through iouyap+relay.
//
// A link is native iff ALL of:
//   - it is point-to-point (segment links flood via the userspace hub, not netio)
//   - both endpoints are IOL nodes (VPCS speaks UDP, never netio)
//   - capture is not requested on it (a tee needs the UDP relay in the path)
//   - (cross-host is not represented in the single-host lab doc today; when it
//     is, add the host check here — this is the one place to do it.)
//
// isIOL maps node id -> whether that node is an IOL node.
//
// Keep this predicate pure (no imports of iouyap/relay) so it stays
// unit-testable on any OS. A wiringBridged result feeds bridgePlan
// (bridgeplan.go), which allocates the pseudo-instance + iouyap bridge + relay
// for the link. See docs/p0-spike.md "Architecture corrections" #2.
func wiringFor(link *lab.Link, isIOL map[int]bool) linkWiring {
	if link.EffectiveType() != lab.LinkP2P {
		return wiringBridged
	}
	if link.Capture != nil && link.Capture.Enabled {
		return wiringBridged
	}
	if len(link.Endpoints) != 2 {
		return wiringBridged
	}
	for _, ep := range link.Endpoints {
		if !isIOL[ep.Node] {
			return wiringBridged
		}
	}
	return wiringNative
}

// isIOLMap builds the node-id -> is-IOL lookup wiringFor and labToLinkSpecs need.
func isIOLMap(doc *lab.Lab) map[int]bool {
	m := make(map[int]bool, len(doc.Nodes))
	for _, n := range doc.Nodes {
		if n.Kind == lab.KindIOL {
			m[n.ID] = true
		}
	}
	return m
}

// nativeLinkSpecs converts a lab document into the netmap.LinkSpec values for
// ONLY the links that are realized natively (see wiringFor). Bridged links are
// deliberately excluded so no NETMAP line is emitted for them — they are wired
// through iouyap+relay instead, and a NETMAP line would double-wire the port.
func nativeLinkSpecs(doc *lab.Lab) []netmap.LinkSpec {
	isIOL := isIOLMap(doc)
	out := make([]netmap.LinkSpec, 0, len(doc.Links))
	for i := range doc.Links {
		l := &doc.Links[i]
		if wiringFor(l, isIOL) != wiringNative {
			continue
		}
		ls := netmap.LinkSpec{P2P: true}
		for _, ep := range l.Endpoints {
			ls.Endpoints = append(ls.Endpoints, netmap.EndpointSpec{
				NodeID: ep.Node, Interface: ep.Interface, IsIOL: isIOL[ep.Node],
			})
		}
		out = append(out, ls)
	}
	return out
}

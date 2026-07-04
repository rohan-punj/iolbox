package server

import (
	"sort"
	"strconv"

	"github.com/rohanpunj/iolab/supervisor/internal/fabric"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
)

// isFabricLink reports whether a link is realised by the STATIC-TAP LINUX-BRIDGE
// FABRIC (the P1 migration path) rather than the legacy iouyap-UDP + relay path.
//
// A link is realised on the fabric iff it has at least two endpoints and every
// endpoint's node kind can live on the fabric (see fabricNodes). Both p2p links
// (a 2-port bridge) and segment links (an N-port bridge) qualify. captureReady
// and capture.enabled are deliberately NOT consulted: in the fabric model every
// link is capturable via tcpdump on its bridge (bcap), so a fabric-eligible link
// always takes the fabric regardless of any capture flag.
//
// fabricOK maps node id -> whether the node's kind can live on the fabric (see
// fabricNodes). With mgmt retired, every node kind (IOL/NAT/VPCS) is fabric, so
// every well-formed link is a fabric link.
func isFabricLink(l *lab.Link, fabricOK map[int]bool) bool {
	if len(l.Endpoints) < 2 {
		return false
	}
	for _, ep := range l.Endpoints {
		if !fabricOK[ep.Node] {
			return false
		}
	}
	return true
}

// fabricNodes returns the set of node ids whose kind is realised by the static-
// tap fabric: IOL, NAT and VPCS — i.e. every node kind (mgmt is retired).
func fabricNodes(doc *lab.Lab) map[int]bool {
	out := make(map[int]bool, len(doc.Nodes))
	for i := range doc.Nodes {
		switch doc.Nodes[i].Kind {
		case lab.KindIOL, lab.KindNAT, lab.KindVPCS:
			out[doc.Nodes[i].ID] = true
		}
	}
	return out
}

// ifaceTap is one IOL interface's STABLE, topology-independent fabric identity: a
// dedicated tap device plus the pseudo-instance its (static) NETMAP line points
// at, so the interface's netio frames land on a netio<->tap iouyap that owns that
// tap. Allocated from the node set + adapter counts alone (never the link set),
// so it is identical across every plan rebuild for the lab's lifetime — the
// property that makes hot-connect a pure runtime bridge-attach.
type ifaceTap struct {
	nodeID    int
	instance  int          // netmap.InstanceID(nodeID)
	iface     netmap.Iface // the IOL interface (e.g. e0/0)
	flatIndex int          // iface.Index() = adapter*16+port
	pseudo    int          // pseudo-instance id naming its /tmp/netio<uid>/<pseudo> socket
	tapName   string       // fabric.TapName(instance, flatIndex)
	netioPath string       // netioPathFor(uid, pseudo)
}

// ifaceKey identifies one node interface by (node id, canonical interface name).
type ifaceKey struct {
	node  int
	iface string
}

// legacyBridgedIfaces returns the set of IOL interfaces that are endpoints of a
// LEGACY bridged link (VPCS/NAT/segment/capture) and therefore must NOT get a
// static tap — their NETMAP line points at the legacy iouyap-UDP pseudo-instance
// instead, and a static tap would double-wire the port. Interface names are
// normalised to netmap's canonical "e0/0" form so they match computeStaticTaps.
func legacyBridgedIfaces(doc *lab.Lab, isIOL map[int]bool, captureReady bool) map[ifaceKey]bool {
	fabricOK := fabricNodes(doc)
	out := make(map[ifaceKey]bool)
	for i := range doc.Links {
		l := &doc.Links[i]
		if isFabricLink(l, fabricOK) {
			continue
		}
		if wiringFor(l, isIOL, captureReady) != wiringBridged {
			continue // native (legacy) links carry no bridged pseudo either
		}
		for _, ep := range l.Endpoints {
			if !isIOL[ep.Node] {
				continue
			}
			ifc, err := netmap.ParseIface(ep.Interface)
			if err != nil {
				continue
			}
			out[ifaceKey{ep.Node, ifc.String()}] = true
		}
	}
	return out
}

// computeStaticTaps builds the whole-lab static-tap fabric identities for every
// fabric-eligible IOL interface. It is DETERMINISTIC (nodes in id order,
// interfaces in enumeration order) and depends only on the node set + adapter
// counts + which interfaces are on legacy links — never on the fabric link set —
// so the identity of a given interface is stable across rebuilds.
//
// P1 enumerates ETHERNET interfaces only (the IOL<->IOL L2 path the spike
// proved); serial-interface taps are a later refinement. Pseudo-instances are
// drawn from netmap's reserved pool, skipping real instance ids; if the pool is
// exhausted the remaining interfaces are left unwired (bounded only in
// pathologically large labs).
func computeStaticTaps(doc *lab.Lab, uid int) map[int]map[string]ifaceTap {
	isIOL := isIOLMap(doc)
	captureReady := doc.CaptureReadyEnabled()
	legacy := legacyBridgedIfaces(doc, isIOL, captureReady)

	nodes := make([]lab.Node, len(doc.Nodes))
	copy(nodes, doc.Nodes)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	used := realInstances(doc) // avoid pseudo == a real instance id
	nextPseudo := netmap.PseudoInstanceBase
	alloc := func() (int, bool) {
		for nextPseudo <= netmap.MaxIOLInstance {
			p := nextPseudo
			nextPseudo++
			if !used[p] {
				used[p] = true
				return p, true
			}
		}
		return 0, false
	}

	out := make(map[int]map[string]ifaceTap)
	for i := range nodes {
		n := &nodes[i]
		if n.Kind != lab.KindIOL {
			continue
		}
		inst := netmap.InstanceID(n.ID)
		eth := intOr(n.Ethernet, 1)
		for _, ifc := range netmap.IfacesForCounts(eth, 0) {
			key := ifc.String()
			if legacy[ifaceKey{n.ID, key}] {
				continue // on a legacy link -> old iouyap-UDP path, no static tap
			}
			name, err := fabric.TapName(inst, ifc.Index())
			if err != nil {
				continue // name too long (pathological instance/port); skip
			}
			p, ok := alloc()
			if !ok {
				break // pseudo pool exhausted
			}
			if out[n.ID] == nil {
				out[n.ID] = make(map[string]ifaceTap)
			}
			out[n.ID][key] = ifaceTap{
				nodeID:    n.ID,
				instance:  inst,
				iface:     ifc,
				flatIndex: ifc.Index(),
				pseudo:    p,
				tapName:   name,
				netioPath: netioPathFor(uid, p),
			}
		}
	}
	return out
}

// staticPseudoSet returns the set of pseudo-instance ids the static fabric owns,
// so the legacy bridge plan can avoid colliding with them (both draw from the
// same reserved [500,1024] pool and both name /tmp/netio<uid>/<pseudo> sockets).
func staticPseudoSet(taps map[int]map[string]ifaceTap) map[int]bool {
	out := make(map[int]bool)
	for _, m := range taps {
		for _, t := range m {
			out[t.pseudo] = true
		}
	}
	return out
}

// staticNetmapEntries flattens the fabric identities into the netmap.StaticEntry
// values BuildStatic renders — one static NETMAP line per fabric-eligible IOL
// interface, pointing it at its own pseudo-instance (and thus its own tap).
func staticNetmapEntries(taps map[int]map[string]ifaceTap) []netmap.StaticEntry {
	var out []netmap.StaticEntry
	for _, m := range taps {
		for _, t := range m {
			out = append(out, netmap.StaticEntry{
				InstanceID:     t.instance,
				Iface:          t.iface,
				PseudoInstance: t.pseudo,
			})
		}
	}
	return out
}

// vtapDevName returns the tap device name for a fabric VPCS node's udp<->tap
// shim. Bounded to the 15-char IFNAMSIZ limit: "iolvpc" + a few digits fits.
func vtapDevName(nodeID int) string {
	return "iolvpc" + strconv.Itoa(nodeID)
}

// tapForEndpoint looks up the static tap identity for a link endpoint, or false
// if that interface has no static tap (e.g. it is on a legacy link).
func tapForEndpoint(taps map[int]map[string]ifaceTap, ep lab.Endpoint) (ifaceTap, bool) {
	ifc, err := netmap.ParseIface(ep.Interface)
	if err != nil {
		return ifaceTap{}, false
	}
	m, ok := taps[ep.Node]
	if !ok {
		return ifaceTap{}, false
	}
	t, ok := m[ifc.String()]
	return t, ok
}

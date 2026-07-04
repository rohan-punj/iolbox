package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
	"github.com/rohanpunj/iolab/supervisor/internal/relay"
)

// labBridge is one running iouyap netio<->UDP bridge tracked in a lab's
// lifecycle. The closer field abstracts internal/iouyap.Bridge so this type (and
// loadedLab) compile on any OS; only bridgeplan_linux.go populates it with a
// real bridge. cancel stops the bridge's Run goroutine.
type labBridge struct {
	netioPath string
	cancel    context.CancelFunc
	closer    interface{ Close() error }
}

// close cancels the bridge's pump loop and closes its sockets/socket file. Safe
// to call once per bridge on stop.
func (b *labBridge) close() error {
	if b == nil {
		return nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	if b.closer != nil {
		return b.closer.Close()
	}
	return nil
}

// bridgePlan is the deterministic, whole-lab wiring for every BRIDGED link
// (capture / VPCS / segment / cross-host). It is computed once per (re)start
// from the lab doc so the two consumers that must agree — the whole-lab NETMAP
// (written before spawn) and the iouyap bridge instances (started before spawn)
// — derive the SAME pseudo-instance ids and UDP ports for each bridged IOL
// endpoint. Keeping it a pure struct (no sockets) makes the allocation and
// pairing unit-testable on any OS; the Linux-only iouyap.New/Run lives in
// bridgeplan_linux.go and consumes this plan.
//
// Pseudo-instance scheme (see netmap.PseudoInstanceBase / AllocPseudoInstances):
// each bridged IOL endpoint gets a pseudo-instance id from the reserved high
// pool [500,1024], skipping any id a real node already uses. The bridged
// endpoint's NETMAP line is "<realInstance>:<iface> <pseudoInstance>:0/0", so
// IOL sends that interface's frames to /tmp/netio<uid>/<pseudoInstance> — the
// unix socket iouyap binds — instead of a peer IOL.
type bridgePlan struct {
	// links holds one entry per bridged link, in link-id order.
	links []bridgedLink
}

// bridgedLink is the wiring for one bridged link: its relay config plus the
// iouyap bridge each IOL endpoint needs. VPCS endpoints have no iouyap bridge
// (VPCS speaks UDP natively); their UDP ports come from the relay endpoint.
type bridgedLink struct {
	linkID   int
	relayCfg relay.Config
	// endpoints is parallel to relayCfg.Endpoints (same order), describing each
	// endpoint's node and, for IOL endpoints, its iouyap bridge.
	endpoints []bridgedEndpoint
}

// bridgedEndpoint is one side of a bridged link.
type bridgedEndpoint struct {
	nodeID    int
	iface     string
	kind      lab.Kind // the endpoint node's kind (iol/vpcs/nat/mgmt)
	isIOL     bool
	vpcs      bool
	pcIndex   int // 1-based PC index within the VPCS process (VPCS only)
	relayIdx  int // index into relayCfg.Endpoints
	relayEP   relay.UDPEndpoint
	pseudo    int          // pseudo-instance id (IOL only)
	iouyap    iouyapConfig // the bridge to start (IOL only)
	netioPath string       // /tmp/netio<uid>/<pseudo> (IOL only)
}

// iouyapConfig mirrors iouyap.Config without importing internal/iouyap into the
// pure plan (iouyap is imported only in bridgeplan_linux.go where the bridge is
// actually created). Field meanings match iouyap.Config exactly: the bridge
// constructs the netio header for delivered frames from LocalInstance /
// LocalAdapter / LocalPort / PseudoInstance (the UDP mesh itself carries raw
// ethernet frames, headerless).
type iouyapConfig struct {
	NetioPath      string
	UDPLocal       int
	UDPRemote      int
	Host           string
	LocalInstance  int
	LocalAdapter   int
	LocalPort      int
	PseudoInstance int
}

// linkAssign is one bridged link's sticky data-plane identity: the relay UDP
// port pair and (for IOL endpoints) the pseudo-instance, per endpoint, in the
// link's doc endpoint order. Held in loadedLab.assigns so every plan rebuild
// reproduces the SAME wiring for a surviving link (see the assigns field's
// comment for why that matters).
type linkAssign struct {
	eps []epAssign
}

// epAssign is one endpoint's sticky assignment.
type epAssign struct {
	local  int // relay's receiving UDP port for this endpoint
	remote int // this endpoint's delivery UDP port
	pseudo int // pseudo-instance id (0 = not an IOL endpoint)
}

// compatible reports whether an existing assignment still fits the link's
// current shape (same endpoint count, IOL-ness per slot) — an upserted link
// that changed endpoints gets fresh assignments instead of inheriting stale
// ones.
func (a *linkAssign) compatible(l *lab.Link, isIOL map[int]bool) bool {
	if a == nil || len(a.eps) != len(l.Endpoints) {
		return false
	}
	for i, ep := range l.Endpoints {
		if (a.eps[i].pseudo != 0) != isIOL[ep.Node] {
			return false
		}
	}
	return true
}

// release returns an assignment's UDP ports to the allocator.
func (a *linkAssign) release(udp *node.PortAllocator) {
	for _, ep := range a.eps {
		udp.Release(ep.local)
		udp.Release(ep.remote)
	}
}

// netioDir returns the per-uid netio socket directory IOL uses, /tmp/netio<uid>.
// Confirmed via lsof on real IOL (docs/p0-spike.md "IOL netio socket
// convention"): each IOL binds /tmp/netio<uid>/<instance-id>, so an iouyap
// pseudo-instance socket must live in the same directory to be reachable by the
// IOL that references it in its NETMAP line.
func netioDir(uid int) string {
	return filepath.Join(os.TempDir(), "netio"+strconv.Itoa(uid))
}

// netioPathFor returns the netio socket path for an instance id under the given
// uid: /tmp/netio<uid>/<instance>.
func netioPathFor(uid, instance int) string {
	return filepath.Join(netioDir(uid), strconv.Itoa(instance))
}

// realInstances returns the set of IOL instance ids in use by real nodes, so
// pseudo-instance allocation can avoid them.
func realInstances(doc *lab.Lab) map[int]bool {
	m := make(map[int]bool)
	for i := range doc.Nodes {
		if doc.Nodes[i].Kind == lab.KindIOL {
			m[netmap.InstanceID(doc.Nodes[i].ID)] = true
		}
	}
	return m
}

// buildBridgePlan computes the whole-lab bridge plan. For each bridged link it
// pairs:
//   - a pseudo-instance id (from the reserved pool, skipping real instances) for
//     each bridged IOL endpoint,
//   - a relay UDPEndpoint (local+remote UDP port) per link endpoint via udp, and
//   - the iouyap bridge config pairing that endpoint's netio socket to the relay.
//
// assigns is the lab's STICKY assignment table (loadedLab.assigns): a link that
// already has a compatible assignment reuses it verbatim, so its wiring is
// STABLE across rebuilds no matter how the rest of the link set changed; only
// links without one (new, or reshaped by an upsert) allocate fresh, and the
// fresh values are recorded back into assigns. Callers own releasing the
// assignments of links that left the doc (see rebuildBridgePlan).
//
// captures maps linkID -> capturePort for links that currently have a capture
// tee attached; the relay config carries the tee port so newRelay opens it.
// captureBind is the host tee listeners bind (empty = loopback; see
// relay.Config.CaptureBind / the supervisor's -capture-bind flag).
//
// It is pure except for port allocation (udp) — no sockets are bound and no
// iouyap bridge is created here; that is the Linux data-plane's job (see
// bridgeplan_linux.go). uid is os.Getuid() on Linux (the netio dir owner).
func buildBridgePlan(doc *lab.Lab, uid int, udp *node.PortAllocator, captures map[int]int, captureBind string, assigns map[int]*linkAssign, reserved map[int]bool) (*bridgePlan, error) {
	isIOL := isIOLMap(doc)
	kindByID := kindMap(doc)
	fabricOK := fabricNodes(doc)
	reals := realInstances(doc)
	captureReady := doc.CaptureReadyEnabled()

	linksSorted := make([]lab.Link, len(doc.Links))
	copy(linksSorted, doc.Links)
	sort.Slice(linksSorted, func(i, j int) bool { return linksSorted[i].ID < linksSorted[j].ID })

	// Pass 1 — decide which links keep their sticky assignment and how many NEW
	// pseudo-instances the rest need. An incompatible assignment (link reshaped
	// under the same id) is released now so its ports return to the pool before
	// fresh allocation. New pseudos must avoid real instances AND every pseudo a
	// kept assignment already owns.
	avoid := make(map[int]bool, len(reals)+len(reserved))
	for r := range reals {
		avoid[r] = true
	}
	// The static-tap fabric owns pseudo-instances from the same reserved pool;
	// avoid them so a legacy bridged endpoint never aliases a fabric tap's socket.
	for p := range reserved {
		avoid[p] = true
	}
	nNewIOL := 0
	for i := range linksSorted {
		l := &linksSorted[i]
		// Fabric IOL<->IOL links are realised by the static-tap fabric, not the
		// relay: skip them here so no pseudo-instance / relay ports are allocated.
		if isFabricLink(l, fabricOK) {
			continue
		}
		if wiringFor(l, isIOL, captureReady) != wiringBridged {
			continue
		}
		la := assigns[l.ID]
		if la != nil && !la.compatible(l, isIOL) {
			la.release(udp)
			delete(assigns, l.ID)
			la = nil
		}
		if la != nil {
			for _, ep := range la.eps {
				if ep.pseudo != 0 {
					avoid[ep.pseudo] = true
				}
			}
			continue
		}
		for _, ep := range l.Endpoints {
			if isIOL[ep.Node] {
				nNewIOL++
			}
		}
	}

	pseudos, err := netmap.AllocPseudoInstances(avoid, nNewIOL)
	if err != nil {
		return nil, err
	}

	plan := &bridgePlan{}
	pi := 0 // next NEW pseudo-instance index

	for i := range linksSorted {
		l := &linksSorted[i]
		if isFabricLink(l, fabricOK) {
			continue
		}
		if wiringFor(l, isIOL, captureReady) != wiringBridged {
			continue
		}
		kind := relay.KindP2P
		if l.EffectiveType() == lab.LinkSegment {
			kind = relay.KindHub
		}
		bl := bridgedLink{
			linkID:   l.ID,
			relayCfg: relay.Config{LinkID: l.ID, Kind: kind, CapturePort: captures[l.ID], CaptureBind: captureBind},
		}

		// Materialize (or mint) this link's sticky assignment.
		la := assigns[l.ID]
		if la == nil {
			la = &linkAssign{}
			for _, ep := range l.Endpoints {
				local, aerr := udp.Next()
				if aerr != nil {
					return nil, aerr
				}
				remote, aerr := udp.Next()
				if aerr != nil {
					return nil, aerr
				}
				ea := epAssign{local: local, remote: remote}
				if isIOL[ep.Node] {
					ea.pseudo = pseudos[pi]
					pi++
				}
				la.eps = append(la.eps, ea)
			}
			assigns[l.ID] = la
		}

		// VPCS PC index is 1-based per PC within a VPCS process; today one PC per
		// VPCS node, so it is always 1.
		for ri, ep := range l.Endpoints {
			ea := la.eps[ri]
			relayEP := relay.UDPEndpoint{Host: "127.0.0.1", LocalPort: ea.local, RemotePort: ea.remote}
			bl.relayCfg.Endpoints = append(bl.relayCfg.Endpoints, relayEP)

			be := bridgedEndpoint{
				nodeID:   ep.Node,
				iface:    ep.Interface,
				kind:     kindByID[ep.Node],
				isIOL:    isIOL[ep.Node],
				relayIdx: ri,
				relayEP:  relayEP,
			}
			if be.isIOL {
				iface, perr := netmap.ParseIface(ep.Interface)
				if perr != nil {
					return nil, fmt.Errorf("link %d node %d: %w", l.ID, ep.Node, perr)
				}
				be.pseudo = ea.pseudo
				be.netioPath = netioPathFor(uid, be.pseudo)
				// iouyap sends IOL's outbound frames to the relay's receiving
				// port (relayEP.LocalPort) and binds the relay's delivery port
				// (relayEP.RemotePort) so the relay's forward direction lands on
				// this bridge, which writes into IOL's netio socket. Frames it
				// delivers get a netio header addressed to the REAL instance +
				// interface, sourced from the pseudo-instance the IOL's NETMAP
				// names as this interface's peer.
				be.iouyap = iouyapConfig{
					NetioPath:      be.netioPath,
					UDPLocal:       relayEP.RemotePort,
					UDPRemote:      relayEP.LocalPort,
					Host:           "127.0.0.1",
					LocalInstance:  netmap.InstanceID(ep.Node),
					LocalAdapter:   iface.Adapter,
					LocalPort:      iface.Port,
					PseudoInstance: be.pseudo,
				}
			} else if be.kind == lab.KindVPCS {
				// VPCS endpoint: speaks UDP natively into the relay. Its send
				// target is the relay's receiving port (relayEP.LocalPort) and it
				// listens on the relay's delivery port (relayEP.RemotePort).
				be.vpcs = true
				be.pcIndex = 1
			}
			// nat/mgmt endpoints are ALSO non-IOL and bridged: they get relay UDP
			// ports (allocated above) but no pseudo-instance and no VPCS argv. The
			// supervisor pumps their tap/macvtap fd against these same ports (see
			// extnetUDPFor / extnet.Config), exactly mirroring VPCS topologically.
			bl.endpoints = append(bl.endpoints, be)
		}
		plan.links = append(plan.links, bl)
	}
	return plan, nil
}

// bridgedEndpointsForNetmap flattens the plan into the netmap.BridgedEndpoint
// values the whole-lab NETMAP needs (one per bridged IOL endpoint), so the
// NETMAP written before spawn points each bridged IOL interface at the same
// pseudo-instance the iouyap bridge binds.
func (p *bridgePlan) bridgedEndpointsForNetmap() []netmap.BridgedEndpoint {
	var out []netmap.BridgedEndpoint
	for i := range p.links {
		for _, be := range p.links[i].endpoints {
			if be.isIOL {
				out = append(out, netmap.BridgedEndpoint{
					NodeID:         be.nodeID,
					Interface:      be.iface,
					PseudoInstance: be.pseudo,
				})
			}
		}
	}
	return out
}

// relayConfigFor returns the relay.Config for a link id, or ok=false if the link
// is not bridged in this plan.
func (p *bridgePlan) relayConfigFor(linkID int) (relay.Config, bool) {
	for i := range p.links {
		if p.links[i].linkID == linkID {
			return p.links[i].relayCfg, true
		}
	}
	return relay.Config{}, false
}

// vpcsUDPFor returns the UDP send/listen ports a VPCS node's PC uses for a link,
// derived from the relay endpoint: the PC sends frames to sendPort (the relay's
// receiving LocalPort) and listens on listenPort (the relay's delivery
// RemotePort). ok is false if the node is not a VPCS endpoint on a bridged link
// in this plan. See node.Spec VPCS UDP fields and VPCSArgv.
func (p *bridgePlan) vpcsUDPFor(nodeID int) (sendPort, listenPort int, ok bool) {
	for i := range p.links {
		for _, be := range p.links[i].endpoints {
			if be.vpcs && be.nodeID == nodeID {
				return be.relayEP.LocalPort, be.relayEP.RemotePort, true
			}
		}
	}
	return 0, 0, false
}

// extnetUDPFor returns the UDP send/listen ports a nat/mgmt node's tap pump uses
// for its link, derived from the relay endpoint exactly like vpcsUDPFor: the
// endpoint SENDS tap frames to sendPort (the relay's receiving LocalPort) and
// LISTENS on listenPort (the relay's delivery RemotePort) for frames to write
// into the tap. ok is false if the node is not a nat/mgmt endpoint on a bridged
// link in this plan. See extnet.Config / extnet.Start.
func (p *bridgePlan) extnetUDPFor(nodeID int) (sendPort, listenPort int, ok bool) {
	for i := range p.links {
		for _, be := range p.links[i].endpoints {
			if be.kind == lab.KindNAT && be.nodeID == nodeID {
				return be.relayEP.LocalPort, be.relayEP.RemotePort, true
			}
		}
	}
	return 0, 0, false
}

// udpPorts returns every UDP port this plan allocated, so they can be released
// back to the allocator before the plan is rebuilt.
func (p *bridgePlan) udpPorts() []int {
	var out []int
	for i := range p.links {
		for _, ep := range p.links[i].relayCfg.Endpoints {
			out = append(out, ep.LocalPort, ep.RemotePort)
		}
	}
	return out
}

// rebuildBridgePlan (re)computes ll.bridge from the current lab doc + capture
// intents. Assignments are STICKY (ll.assigns): links still in the doc keep
// their exact ports/pseudos, so a rebuild never disturbs running endpoints;
// only links that LEFT the doc (or turned native) release theirs — and their
// now-orphaned iouyap bridges are closed so no stale pump keeps their old
// ports half-alive. It does NOT create sockets or iouyap bridges —
// startBridges (Linux) does that, consuming the plan this produces. Called
// under no lock; caller serialises.
func (s *Server) rebuildBridgePlan(ll *loadedLab) error {
	// Recompute the static-tap fabric first (deterministic, topology-independent)
	// so its pseudo-instances can be reserved out of the legacy plan below and the
	// whole-lab NETMAP can include a static line per fabric interface.
	ll.staticTaps = computeStaticTaps(ll.doc, currentUID())
	reserved := staticPseudoSet(ll.staticTaps)

	// Release assignments of links no longer bridged in the current doc.
	isIOL := isIOLMap(ll.doc)
	captureReady := ll.doc.CaptureReadyEnabled()
	bridged := make(map[int]bool)
	for i := range ll.doc.Links {
		l := &ll.doc.Links[i]
		if wiringFor(l, isIOL, captureReady) == wiringBridged {
			bridged[l.ID] = true
		}
	}
	for id, la := range ll.assigns {
		if bridged[id] {
			continue
		}
		la.release(s.udpPorts)
		delete(ll.assigns, id)
		// Close the departed link's iouyap bridges (keyed by pseudo netio path).
		for _, ep := range la.eps {
			if ep.pseudo == 0 {
				continue
			}
			path := netioPathFor(currentUID(), ep.pseudo)
			ll.mu.Lock()
			b := ll.bridges[path]
			delete(ll.bridges, path)
			ll.mu.Unlock()
			_ = b.close()
		}
	}

	ll.mu.Lock()
	captures := make(map[int]int, len(ll.captures))
	for k, v := range ll.captures {
		captures[k] = v
	}
	ll.mu.Unlock()
	plan, err := buildBridgePlan(ll.doc, currentUID(), s.udpPorts, captures, s.cfg.CaptureBind, ll.assigns, reserved)
	if err != nil {
		return err
	}
	ll.bridge = plan
	return nil
}

// String renders a bridged endpoint for diagnostics.
func (be bridgedEndpoint) String() string {
	if be.isIOL {
		return fmt.Sprintf("iol node %d %s pseudo=%d netio=%s udpLocal=%d udpRemote=%d",
			be.nodeID, be.iface, be.pseudo, be.netioPath, be.iouyap.UDPLocal, be.iouyap.UDPRemote)
	}
	return fmt.Sprintf("vpcs node %d %s send=%d listen=%d",
		be.nodeID, be.iface, be.relayEP.LocalPort, be.relayEP.RemotePort)
}

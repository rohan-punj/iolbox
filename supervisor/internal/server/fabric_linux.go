//go:build linux

package server

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/rohanpunj/iolbox/supervisor/internal/bcap"
	"github.com/rohanpunj/iolbox/supervisor/internal/dirstat"
	"github.com/rohanpunj/iolbox/supervisor/internal/fabric"
	"github.com/rohanpunj/iolbox/supervisor/internal/iouyap"
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/slowtee"
	"github.com/rohanpunj/iolbox/supervisor/internal/vtap"
)

// tapDeviceExists reports whether the named tap netdev currently exists, via a
// cheap sysfs stat (no privileged call). startFabric uses it to skip the
// `sudo ip` EnsureTap cost for taps that are already up, while still recreating
// any whose device has gone missing under a lingering bookkeeping entry.
func tapDeviceExists(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name)
	return err == nil
}

// tapMasterIs reports whether the named netdev's current bridge master
// (/sys/class/net/<dev>/master, a symlink to the master device's own sysfs
// dir) is exactly bridgeName. A cheap sysfs readlink — no privileged call —
// so fabricLinkFullyAttached can gate the skip decision on the KERNEL's
// current wiring, not just our bookkeeping. Any error (dev or master link
// missing, dev unbridged) reads as false: a mismatch, which the caller treats
// as "must re-attach".
func tapMasterIs(dev, bridgeName string) bool {
	target, err := os.Readlink("/sys/class/net/" + dev + "/master")
	if err != nil {
		return false
	}
	// target looks like "../br-12" (or similar relative path); compare the
	// final path element only.
	base := target
	if i := strings.LastIndexByte(target, '/'); i >= 0 {
		base = target[i+1:]
	}
	return base == bridgeName
}

// startFabric realises the static-tap fabric for the current plan, BEFORE any
// IOL spawns: it pre-creates every fabric-eligible IOL interface's tap
// (persistent, owned by this uid, unbridged) and starts a netio<->tap iouyap for
// it, so the pseudo-instance's /tmp/netio<uid>/<pseudo> socket exists when IOL
// connects to it per its static NETMAP line. It then (re)attaches each fabric
// link's member taps to their br-<linkid> bridge. Idempotent across restarts
// and mid-session link.add: EnsureTap/EnsureBridge tolerate existing objects,
// and an already-running tap bridge is left as-is. Called by prepareLabDir.
//
// ids scopes the LINK-ATTACH loop (not the static-tap loop, which stays
// whole-lab — taps are cheap-skipped already and topology-independent): when
// non-empty (a startNodes for specific node ids), only links that touch one of
// ids, or that have NEVER been attached, are processed — a per-node restart of
// an N-node lab no longer re-walks every one of the lab's other links. Pass
// nil/empty for a whole-lab start (lab.start, link.add) to process every link,
// the original behavior.
func (s *Server) startFabric(ll *loadedLab, ids []int) error {
	mgr := fabric.NewManager()
	ctx := context.Background()
	uid := currentUID()

	// /tmp/netio<uid> must exist before iouyap binds a socket in it (and before
	// IOL binds its own <instance> socket there). startBridges also ensures this;
	// harmless to repeat.
	if err := os.MkdirAll(netioDir(uid), 0o755); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "netio dir: %v", err)
	}

	for _, m := range ll.staticTaps {
		for _, t := range m {
			// startFabric runs on EVERY node.start (via startNodes) over the
			// WHOLE-lab static-tap set. Skip only taps that are FULLY realised —
			// the DEVICE is present AND its pump is running — so a repeat start
			// issues no redundant `sudo ip` (re-running EnsureTap for every tap of
			// every node made later starts slow: ~100 taps => ~200 sudo, ~2s).
			//
			// Gate on the actual device existing, NOT just the tapBridge map: a
			// device can be torn out from under a lingering map entry by a
			// mid-session reshape/cleanup, and trusting the map alone left the
			// device un-recreated so a later fabric attach failed outright
			// ("Cannot find device iolN_M"). If either the device or the pump is
			// missing, rebuild both — closing any stale pump first (its fd points
			// at the now-gone device) so the fresh device gets a live pump.
			ll.mu.Lock()
			lb, hasPump := ll.tapBridges[t.netioPath]
			ll.mu.Unlock()
			if hasPump && tapDeviceExists(t.tapName) {
				continue
			}
			if hasPump {
				_ = lb.close()
				ll.mu.Lock()
				delete(ll.tapBridges, t.netioPath)
				ll.mu.Unlock()
			}
			if err := mgr.EnsureTap(ctx, t.tapName, uid); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric tap %s: %v", t.tapName, err)
			}
			cfg := iouyap.Config{
				NetioPath:      t.netioPath,
				LocalInstance:  t.instance,
				LocalAdapter:   t.iface.Adapter,
				LocalPort:      t.iface.Port,
				PseudoInstance: t.pseudo,
			}
			tb, err := iouyap.NewTap(cfg, t.tapName)
			if err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric iouyap %s: %v", t.netioPath, err)
			}
			c, cancel := context.WithCancel(context.Background())
			go func(b *iouyap.TapBridge, ctx context.Context) { _ = b.Run(ctx) }(tb, c)
			ll.mu.Lock()
			ll.tapBridges[t.netioPath] = &labBridge{netioPath: t.netioPath, cancel: cancel, closer: tb}
			ll.mu.Unlock()
		}
	}

	// ids scoping: build a lookup set once so the loop below is O(1) per link
	// instead of O(len(ids)) per link. Empty/nil ids means "whole lab" (the set
	// stays empty and idSet[n] is always false, so the `len(idSet) > 0 &&`
	// guard below short-circuits to "process everything").
	idSet := make(map[int]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	// Attach every in-scope fabric link's taps to its bridge, and (re)start a
	// bridge capture for any fabric link that already has an armed capture port
	// (doc capture.enabled auto-armed at lab start, or a capture that survived a
	// hot-connect). The bridge exists now, so tcpdump can attach.
	fabricOK := fabricNodes(ll.doc)
	for i := range ll.doc.Links {
		l := &ll.doc.Links[i]
		if !isFabricLink(l, fabricOK) {
			continue
		}
		// Scoped start: skip a link that touches none of the started ids AND
		// has already been attached at least once. A link never attached must
		// still be processed even when out of scope — e.g. a link whose OTHER
		// endpoint just started for the first time — attachFabricLink itself
		// safely no-ops on any endpoint that isn't up yet.
		ll.mu.Lock()
		everAttached := ll.fabricLinks[l.ID]
		ll.mu.Unlock()
		if len(idSet) > 0 && everAttached && !linkTouchesAny(l, idSet) {
			continue
		}
		// Skip the attach itself (and the dirstat reopen it triggers) when the
		// link is ALREADY fully wired at the kernel level: gate on the actual
		// devices, not just the bookkeeping map, so a link whose bridge or
		// tap-membership was torn out from under a lingering map entry still
		// self-heals via the normal attachFabricLink path below.
		if !s.fabricLinkFullyAttached(ll, l) {
			if err := s.attachFabricLink(ll, l); err != nil {
				return err
			}
		}
		// Always run the armed-capture check, even for a skipped (already
		// fully-attached) link: a supervisor restart can lose the in-memory
		// bcap while the bridge device itself persists, so a link that looks
		// "already attached" can still need its capture restarted.
		ll.mu.Lock()
		port, armed := ll.captures[l.ID]
		_, capturing := ll.bcaps[l.ID]
		ll.mu.Unlock()
		if armed && port != 0 && !capturing {
			if _, err := s.startBridgeCapture(ll, l.ID, port); err != nil {
				return err
			}
		}
	}
	return nil
}

// linkTouchesAny reports whether l has an endpoint whose node id is in idSet.
func linkTouchesAny(l *lab.Link, idSet map[int]bool) bool {
	for _, ep := range l.Endpoints {
		if idSet[ep.Node] {
			return true
		}
	}
	return false
}

// fabricLinkFullyAttached reports whether a fabric link is already completely
// realised at the KERNEL level, so startFabric can skip the redundant
// EnsureBridge + per-endpoint Attach (each a `sudo ip` round-trip) and the
// dirstat reopen that follows a real attach. It requires ALL of:
//
//  1. bookkeeping agrees the link was attached (ll.fabricLinks[l.ID]);
//  2. the link's bridge device exists in the kernel;
//  3. EVERY endpoint that currently has a tap device is a MEMBER of that
//     bridge (master symlink matches).
//
// An endpoint with no tap device yet (a NAT/VPCS node not yet started) is
// treated as a MISMATCH — not "vacuously attached" — so the link falls
// through to attachFabricLink, whose per-endpoint switch already tolerates a
// not-yet-started NAT/VPCS by skipping it (idempotent; that endpoint gets
// wired when it starts, via attachFabricForNode). This mirrors the static-tap
// loop's "gate on the device, not just the map" invariant: never trust
// bookkeeping alone to skip a privileged step.
func (s *Server) fabricLinkFullyAttached(ll *loadedLab, l *lab.Link) bool {
	ll.mu.Lock()
	attached := ll.fabricLinks[l.ID]
	ll.mu.Unlock()
	if !attached {
		return false
	}
	br, err := fabric.BridgeName(l.ID)
	if err != nil || !tapDeviceExists(br) {
		return false
	}
	devs := s.fabricLinkTapDevs(ll, l)
	// fabricLinkTapDevs only returns devs for endpoints that currently HAVE a
	// tap (it silently skips a not-yet-started NAT/VPCS). A link whose
	// endpoint count exceeds the number of tap devs found has at least one
	// endpoint with no tap yet — that is a mismatch (fall through so
	// attachFabricLink wires whatever IS up and leaves the rest to
	// attachFabricForNode later), not a vacuous "fully attached".
	if len(devs) < len(l.Endpoints) {
		return false
	}
	for _, dev := range devs {
		if !tapMasterIs(dev, br) {
			return false
		}
	}
	return true
}

// startBridgeCapture starts (or reuses) a tcpdump-on-bridge pcapng capture for a
// fabric link and returns the TCP port serving it. Idempotent: a link already
// capturing returns its existing port.
func (s *Server) startBridgeCapture(ll *loadedLab, linkID, port int) (int, error) {
	ll.mu.Lock()
	if c, ok := ll.bcaps[linkID]; ok {
		p := c.Port()
		ll.mu.Unlock()
		return p, nil
	}
	ll.mu.Unlock()
	br, err := fabric.BridgeName(linkID)
	if err != nil {
		return 0, protocol.Errorf(protocol.CodeBadRequest, "fabric bridge name link %d: %v", linkID, err)
	}
	c, err := bcap.Start(br, s.cfg.CaptureBind, port)
	if err != nil {
		return 0, protocol.Errorf(protocol.CodeNodeSpawnFailed, "capture bridge %s: %v", br, err)
	}
	ll.mu.Lock()
	ll.bcaps[linkID] = c
	ll.mu.Unlock()
	return c.Port(), nil
}

// stopBridgeCapture stops a fabric link's bridge capture (kills tcpdump, closes
// the pcapng server). Idempotent.
func (s *Server) stopBridgeCapture(ll *loadedLab, linkID int) {
	ll.mu.Lock()
	c := ll.bcaps[linkID]
	delete(ll.bcaps, linkID)
	ll.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// attachFabricLink ensures the br-<linkid> bridge exists and both endpoint taps
// are attached to it. The taps already exist (created at boot by startFabric for
// every fabric-eligible interface, connected or not), so this is a pure runtime
// operation that never touches a running IOL — the hot-connect the spike proved.
func (s *Server) attachFabricLink(ll *loadedLab, l *lab.Link) error {
	mgr := fabric.NewManager()
	ctx := context.Background()
	br, err := fabric.BridgeName(l.ID)
	if err != nil {
		return protocol.Errorf(protocol.CodeBadRequest, "fabric bridge name link %d: %v", l.ID, err)
	}
	if err := mgr.EnsureBridge(ctx, br); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric bridge %s: %v", br, err)
	}
	for _, ep := range l.Endpoints {
		node := ll.findNode(ep.Node)
		switch {
		case node != nil && node.Kind == lab.KindNAT:
			// NAT: hand the bridge to its extnet endpoint, which attaches its own
			// tap + moves the gateway/NAT onto the bridge. A NAT not yet started is
			// skipped — startExtnetNode attaches it when it comes up (idempotent).
			nr := ll.get(ep.Node)
			if nr == nil || nr.extnet == nil {
				continue
			}
			if err := nr.extnet.AttachBridge(br); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric nat %d attach %s: %v", ep.Node, br, err)
			}
		case node != nil && node.Kind == lab.KindVPCS:
			// VPCS: attach its shim's tap. Not-yet-started VPCS is skipped —
			// startNodes attaches it when it comes up (idempotent).
			nr := ll.get(ep.Node)
			if nr == nil || nr.vtap == nil || nr.vtapName == "" {
				continue
			}
			if err := mgr.Attach(ctx, br, nr.vtapName); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric vpcs attach %s->%s: %v", nr.vtapName, br, err)
			}
		default:
			// IOL: attach its static tap.
			t, ok := tapForEndpoint(ll.staticTaps, ep)
			if !ok {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed,
					"fabric link %d: no static tap for node %d %s", l.ID, ep.Node, ep.Interface)
			}
			if err := mgr.Attach(ctx, br, t.tapName); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric attach %s->%s: %v", t.tapName, br, err)
			}
		}
	}
	ll.mu.Lock()
	ll.fabricLinks[l.ID] = true
	ll.mu.Unlock()
	s.openLinkDirstat(ll, l)
	s.openLinkSlowTee(ll, l)
	return nil
}

// openLinkDirstat (re)opens the always-on per-endpoint-tap directional
// classifier for a fabric link over its current endpoint taps. Called from
// attachFabricLink, which may run more than once for a link (mid-session
// link.add, or attachFabricForNode when a VPCS/NAT endpoint comes up after the
// bridge already existed): re-opening picks up an endpoint tap that didn't exist
// at first attach. Any existing classifier is closed first so sockets/goroutines
// don't leak across re-attaches. A link with fewer than one bindable tap yields
// a nil classifier and simply no directional data — the aggregate fps/bps glow
// is unaffected.
func (s *Server) openLinkDirstat(ll *loadedLab, l *lab.Link) {
	devs := s.fabricLinkTapDevs(ll, l)
	ll.mu.Lock()
	old := ll.dirstats[l.ID]
	delete(ll.dirstats, l.ID)
	ll.mu.Unlock()
	old.Close()

	dc, err := dirstat.Open(devs)
	if err != nil {
		// Non-fatal: a tap that can't be bound (missing, or the dev box lacks
		// CAP_NET_RAW) just costs this link its directional breakdown. Log once at
		// the link level so a systematic failure is visible without spamming.
		log.Printf("dirstat: link %d directional classifier degraded: %v", l.ID, err)
	}
	if dc != nil {
		ll.mu.Lock()
		ll.dirstats[l.ID] = dc
		ll.mu.Unlock()
	}
}

// closeLinkDirstat stops and drops a fabric link's directional classifier.
// Idempotent (nil-safe Close).
func (s *Server) closeLinkDirstat(ll *loadedLab, linkID int) {
	ll.mu.Lock()
	dc := ll.dirstats[linkID]
	delete(ll.dirstats, linkID)
	ll.mu.Unlock()
	dc.Close()
}

// openLinkSlowTee (re)opens the userspace LACP slow-protocols tee for a fabric
// link, mirroring openLinkDirstat: any existing tee is closed first so
// sockets/goroutines don't leak across re-attaches.
//
// SCOPE: p2p fabric links only. A tee only makes sense between exactly two
// endpoint taps (fabricLinkTapDevs returns 2) where BOTH endpoints are IOL
// nodes — a LACP port-channel is switch<->switch; a NAT or VPCS endpoint, or
// an N-port segment (hub), is skipped (N-port flood-to-all-members is out of
// scope for v1 — see the slowtee package doc). A link that doesn't qualify,
// or whose taps can't be bound, simply gets no tee and no LACP passthrough;
// every other protocol on the link is unaffected.
func (s *Server) openLinkSlowTee(ll *loadedLab, l *lab.Link) {
	ll.mu.Lock()
	old := ll.slowtees[l.ID]
	delete(ll.slowtees, l.ID)
	ll.mu.Unlock()
	old.Close()

	if !linkIsIOLToIOL(ll, l) {
		return
	}
	devs := s.fabricLinkTapDevs(ll, l)
	if len(devs) != 2 {
		return
	}

	t, err := slowtee.Open(devs)
	if err != nil {
		log.Printf("slowtee: link %d LACP tee degraded: %v", l.ID, err)
	}
	if t != nil {
		ll.mu.Lock()
		ll.slowtees[l.ID] = t
		ll.mu.Unlock()
	}
}

// closeLinkSlowTee stops and drops a fabric link's LACP slow-protocols tee.
// Idempotent (nil-safe Close). Mirrors closeLinkDirstat.
func (s *Server) closeLinkSlowTee(ll *loadedLab, linkID int) {
	ll.mu.Lock()
	t := ll.slowtees[linkID]
	delete(ll.slowtees, linkID)
	ll.mu.Unlock()
	t.Close()
}

// linkIsIOLToIOL reports whether both of a link's endpoints are IOL nodes —
// the only case a LACP port-channel is relevant for (switch<->switch).
func linkIsIOLToIOL(ll *loadedLab, l *lab.Link) bool {
	if len(l.Endpoints) != 2 {
		return false
	}
	for _, ep := range l.Endpoints {
		n := ll.findNode(ep.Node)
		if n == nil || n.Kind != lab.KindIOL {
			return false
		}
	}
	return true
}

// attachFabricForNode (re)attaches the fabric link that a just-started node
// participates in — used by startExtnetNode so a NAT that comes up after its
// bridge already exists gets wired in. No-op if the node is on no fabric link.
func (s *Server) attachFabricForNode(ll *loadedLab, nodeID int) error {
	fabricOK := fabricNodes(ll.doc)
	for i := range ll.doc.Links {
		l := &ll.doc.Links[i]
		if !isFabricLink(l, fabricOK) {
			continue
		}
		for _, ep := range l.Endpoints {
			if ep.Node == nodeID {
				return s.attachFabricLink(ll, l)
			}
		}
	}
	return nil
}

// detachFabricLink removes a fabric link's bridge (detaching its taps first). The
// taps themselves persist — the interfaces are simply unconnected again, still
// fabric-eligible and ready to be reattached to a new link with no restart.
func (s *Server) detachFabricLink(ll *loadedLab, l *lab.Link) {
	// Stop any bridge capture first (the bridge is about to go away).
	s.stopBridgeCapture(ll, l.ID)
	// Stop the directional classifier: its taps are about to be detached/removed.
	s.closeLinkDirstat(ll, l.ID)
	// Stop the LACP slow-protocols tee for the same reason.
	s.closeLinkSlowTee(ll, l.ID)
	mgr := fabric.NewManager()
	ctx := context.Background()
	for _, ep := range l.Endpoints {
		node := ll.findNode(ep.Node)
		switch {
		case node != nil && node.Kind == lab.KindNAT:
			// NAT: unwire its gateway/NAT + detach its tap via extnet (must happen
			// while the bridge still exists).
			if nr := ll.get(ep.Node); nr != nil && nr.extnet != nil {
				nr.extnet.DetachBridge()
			}
		case node != nil && node.Kind == lab.KindVPCS:
			if nr := ll.get(ep.Node); nr != nil && nr.vtapName != "" {
				_ = mgr.Detach(ctx, nr.vtapName)
			}
		default:
			if t, ok := tapForEndpoint(ll.staticTaps, ep); ok {
				_ = mgr.Detach(ctx, t.tapName)
			}
		}
	}
	if br, err := fabric.BridgeName(l.ID); err == nil {
		_ = mgr.DeleteBridge(ctx, br)
	}
	ll.mu.Lock()
	delete(ll.fabricLinks, l.ID)
	ll.mu.Unlock()
}

// setupVPCSFabric brings up a fabric VPCS node's udp<->tap shim BEFORE the VPCS
// process spawns: it allocates the udp port pair (vpcsBind, shimBind), creates
// the tap (unbridged) and starts the shim. buildSpec then launches VPCS with -s
// vpcsBind / -c shimBind; the tap is bridge-attached at link time (hot-connect).
// Idempotent per node.
func (s *Server) setupVPCSFabric(ll *loadedLab, nr *nodeRuntime, n *lab.Node) error {
	if nr.vtap != nil {
		return nil
	}
	vpcsBind, err := s.udpPorts.Next()
	if err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "vpcs %d: udp port: %v", n.ID, err)
	}
	shimBind, err := s.udpPorts.Next()
	if err != nil {
		s.udpPorts.Release(vpcsBind)
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "vpcs %d: udp port: %v", n.ID, err)
	}
	tapName := vtapDevName(n.ID)
	mgr := fabric.NewManager()
	ctx := context.Background()
	if err := mgr.EnsureTap(ctx, tapName, currentUID()); err != nil {
		s.udpPorts.Release(vpcsBind)
		s.udpPorts.Release(shimBind)
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "vpcs %d tap %s: %v", n.ID, tapName, err)
	}
	shim, err := vtap.Start(tapName, shimBind, vpcsBind)
	if err != nil {
		_ = mgr.DeleteTap(ctx, tapName)
		s.udpPorts.Release(vpcsBind)
		s.udpPorts.Release(shimBind)
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "vpcs %d shim: %v", n.ID, err)
	}
	nr.vtap = shim
	nr.vtapName = tapName
	nr.vtapPorts = [2]int{vpcsBind, shimBind}
	return nil
}

// fabStat is one fabric link's cumulative counters for the stats loop: frames +
// bytes summed across its endpoint taps (each frame ingresses at one tap, so the
// sum is the link's forwarded-frame count), plus the per-protocol counts from an
// active bridge capture (nil when the link isn't being captured).
type fabStat struct {
	frames uint64
	bytes  uint64
	protos map[string]uint64
	// dir is a cumulative snapshot of the link's always-on per-endpoint-tap
	// directional classifier (nil when the link has no classifier). The stats
	// loop diffs two consecutive snapshots into per-direction per-protocol rates.
	dir dirstat.Counters
}

// fabricStats snapshots every fabric link's cumulative counters (see fabStat).
// Frame/byte counts come from the endpoint taps' netdev statistics — always
// available, so a fabric link drives link-glow whether or not it is captured;
// per-protocol counts come from the link's live bridge capture when present.
func (s *Server) fabricStats(ll *loadedLab) map[int]fabStat {
	ll.mu.Lock()
	linkIDs := make([]int, 0, len(ll.fabricLinks))
	for id := range ll.fabricLinks {
		linkIDs = append(linkIDs, id)
	}
	bcaps := make(map[int]*bcap.Capture, len(ll.bcaps))
	for id, c := range ll.bcaps {
		bcaps[id] = c
	}
	dcs := make(map[int]*dirstat.Classifier, len(ll.dirstats))
	for id, c := range ll.dirstats {
		dcs[id] = c
	}
	ll.mu.Unlock()

	out := make(map[int]fabStat, len(linkIDs))
	for _, id := range linkIDs {
		l := ll.findLink(id)
		if l == nil {
			continue
		}
		var frames, bytes uint64
		for _, dev := range s.fabricLinkTapDevs(ll, l) {
			p, b := readTapCounters(dev)
			frames += p
			bytes += b
		}
		fs := fabStat{frames: frames, bytes: bytes}
		if c := bcaps[id]; c != nil {
			_, _, protos := c.Stats()
			fs.protos = protos
		}
		if dc := dcs[id]; dc != nil {
			fs.dir = dc.Snapshot()
		}
		out[id] = fs
	}
	return out
}

// fabricLinkTapDevs returns the tap device names of a fabric link's endpoints:
// an IOL interface's static tap, a VPCS node's shim tap, or a NAT node's tap.
func (s *Server) fabricLinkTapDevs(ll *loadedLab, l *lab.Link) []string {
	var devs []string
	for _, ep := range l.Endpoints {
		node := ll.findNode(ep.Node)
		switch {
		case node != nil && node.Kind == lab.KindVPCS:
			if nr := ll.get(ep.Node); nr != nil && nr.vtapName != "" {
				devs = append(devs, nr.vtapName)
			}
		case node != nil && node.Kind == lab.KindNAT:
			devs = append(devs, "iolnat"+strconv.Itoa(ep.Node)) // matches extnet tapName
		default:
			if t, ok := tapForEndpoint(ll.staticTaps, ep); ok {
				devs = append(devs, t.tapName)
			}
		}
	}
	return devs
}

// readTapCounters reads a netdev's cumulative rx packet + byte counters from
// sysfs. rx on a tap counts frames the node sent into the bridge (the fabric's
// forwarded-frame direction). Missing/unreadable counters read as 0.
func readTapCounters(dev string) (packets, bytes uint64) {
	base := "/sys/class/net/" + dev + "/statistics/"
	return readUintFile(base + "rx_packets"), readUintFile(base + "rx_bytes")
}

func readUintFile(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v
}

// teardownVPCS stops a fabric VPCS node's shim, deletes its tap and releases its
// udp ports. Idempotent (nil/zero guards), so stopNode and teardownFabric can
// both call it.
func (s *Server) teardownVPCS(nr *nodeRuntime) {
	if nr.vtap != nil {
		_ = nr.vtap.Close()
		nr.vtap = nil
	}
	if nr.vtapName != "" {
		_ = fabric.NewManager().DeleteTap(context.Background(), nr.vtapName)
		nr.vtapName = ""
	}
	if nr.vtapPorts[0] != 0 {
		s.udpPorts.Release(nr.vtapPorts[0])
	}
	if nr.vtapPorts[1] != 0 {
		s.udpPorts.Release(nr.vtapPorts[1])
	}
	nr.vtapPorts = [2]int{}
}

// teardownFabric closes every netio<->tap iouyap, deletes every fabric bridge,
// removes every static tap, and tears down any fabric VPCS shims/taps. Called on
// full lab teardown (stopBridges), so no fabric kernel objects or pump goroutines
// leak. Taps/bridges are recreated idempotently on the next start.
func (s *Server) teardownFabric(ll *loadedLab) {
	ll.mu.Lock()
	tbs := ll.tapBridges
	ll.tapBridges = make(map[string]*labBridge)
	links := ll.fabricLinks
	ll.fabricLinks = make(map[int]bool)
	taps := ll.staticTaps
	caps := ll.bcaps
	ll.bcaps = make(map[int]*bcap.Capture)
	dcs := ll.dirstats
	ll.dirstats = make(map[int]*dirstat.Classifier)
	tees := ll.slowtees
	ll.slowtees = make(map[int]*slowtee.Tee)
	nodes := make([]*nodeRuntime, 0, len(ll.nodes))
	for _, nr := range ll.nodes {
		nodes = append(nodes, nr)
	}
	ll.mu.Unlock()

	for _, c := range caps {
		_ = c.Close()
	}
	for _, dc := range dcs {
		dc.Close()
	}
	for _, t := range tees {
		t.Close()
	}
	for _, b := range tbs {
		_ = b.close()
	}
	mgr := fabric.NewManager()
	ctx := context.Background()
	for id := range links {
		if br, err := fabric.BridgeName(id); err == nil {
			_ = mgr.DeleteBridge(ctx, br)
		}
	}
	for _, m := range taps {
		for _, t := range m {
			_ = mgr.DeleteTap(ctx, t.tapName)
		}
	}
	for _, nr := range nodes {
		if nr.vtap != nil || nr.vtapName != "" {
			s.teardownVPCS(nr)
		}
	}
}

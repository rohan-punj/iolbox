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
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
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

// tapHasMaster reports whether the named netdev currently has ANY bridge
// master, regardless of which. Used to detect that a tap/bridge adopted from
// a pre-existing device (EnsureTap/EnsureBridge tolerating "already exists")
// carried over bridge membership it should not have — the mechanism behind a
// reported bug where a switch node saw MAC-table entries on ports it never
// wired: two labs on one host reuse the same tap/bridge names (they are not
// lab-scoped), and a leftover device from a prior/other lab run stayed
// bridged, so a freshly loaded lab silently inherited its traffic.
func tapHasMaster(name string) bool {
	_, err := os.Readlink("/sys/class/net/" + name + "/master")
	return err == nil
}

// bridgeMembers lists the netdevs currently enslaved to the named bridge, via
// a cheap sysfs directory read (no privileged call). Used the same way as
// tapHasMaster, but for a bridge being adopted: a leftover bridge can carry
// members from whatever lab created it.
func bridgeMembers(name string) ([]string, error) {
	entries, err := os.ReadDir("/sys/class/net/" + name + "/brif")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// deleteBridgeVerified/deleteTapVerified delete a device and, with one cheap
// sysfs stat, log when it's still present afterward. This is deliberately
// NOT a retry loop with a backoff budget: teardown runs synchronously inside
// request handlers, and a lab with many taps/bridges retrying a stuck delete
// for even a second each would make lab.stop/lab.load visibly hang. A single
// verified attempt gives operators a log line to act on without risking that
// latency blowup; teardown remains best-effort by design.
func deleteBridgeVerified(mgr *fabric.Manager, ctx context.Context, name string) {
	if err := mgr.DeleteBridge(ctx, name); err != nil {
		log.Printf("fabric: teardown: delete bridge %s: %v", name, err)
	} else if tapDeviceExists(name) {
		log.Printf("fabric: teardown: bridge %s still present after delete", name)
	}
}

func deleteTapVerified(mgr *fabric.Manager, ctx context.Context, name string) {
	if err := mgr.DeleteTap(ctx, name); err != nil {
		log.Printf("fabric: teardown: delete tap %s: %v", name, err)
	} else if tapDeviceExists(name) {
		log.Printf("fabric: teardown: tap %s still present after delete", name)
	}
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
	// Persisted faults are definitions until the lab is started. Activate the
	// explicitly initial ones here, before any link decision is made, so an
	// initially-down link still gets its bridge and an initially-impaired link
	// is applied during the same start transaction.
	ll.activateInitialFaults()
	staticTaps := ll.staticTapsSnapshot()

	// /tmp/netio<uid> must exist before iouyap binds a socket in it (and before
	// IOL binds its own <instance> socket there). startBridges also ensures this;
	// harmless to repeat.
	if err := os.MkdirAll(netioDir(uid), 0o755); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "netio dir: %v", err)
	}

	for _, m := range staticTaps {
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
			if hasPump && lb.tapName == t.tapName && tapDeviceExists(t.tapName) {
				continue
			}
			if hasPump {
				_ = lb.close()
				ll.mu.Lock()
				delete(ll.tapBridges, t.netioPath)
				ll.mu.Unlock()
			}
			// A pseudo-instance reshuffle (e.g. this tap's netio path was
			// recomputed differently than when its pump was first opened) can
			// leave a GHOST pump for this exact tapName parked under a different,
			// now-stale netioPath key — hasPump above only ever looked up this
			// tap's CURRENT path, so that ghost survives the check above even
			// though it's still holding this tap's fd open. Evict it by identity
			// (tapName), not by path, or the TUNSETIFF just below fails with
			// "device or resource busy" against a pump this very map already owns.
			ll.mu.Lock()
			for path, ghost := range ll.tapBridges {
				if path == t.netioPath || ghost.tapName != t.tapName {
					continue
				}
				_ = ghost.close()
				delete(ll.tapBridges, path)
			}
			ll.mu.Unlock()
			// The eviction above is scoped to THIS lab's tapBridges map, but tap
			// names are a process-global kernel namespace: another lab (another
			// *Server, or a lab whose teardown has not finished) can still hold a
			// live pump on this exact device, and no per-lab map can see it. Close
			// that foreign owner first or the TUNSETIFF below fails with "device or
			// resource busy" — finding #9, the cross-lab instance of finding #1.
			evictForeignTapClaim(t.tapName, ll)
			existed := tapDeviceExists(t.tapName)
			if err := retryTransientFabric(func() error { return mgr.EnsureTap(ctx, t.tapName, uid) }); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric tap %s: %v", t.tapName, err)
			}
			// A static tap is unbridged until attachFabricLink wires it; if we
			// just ADOPTED a pre-existing device (rather than creating a fresh
			// one) and it's still carrying a bridge master, that membership is
			// leftover from some earlier run, not this lab's own wiring. Strip
			// it so this lab can never silently inherit another lab's traffic.
			if existed && tapHasMaster(t.tapName) {
				log.Printf("fabric: tap %s already existed and was still bridged; detaching stale membership", t.tapName)
				_ = mgr.Detach(ctx, t.tapName)
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
			bridge := &labBridge{netioPath: t.netioPath, tapName: t.tapName, cancel: cancel, closer: tb}
			ll.mu.Lock()
			ll.tapBridges[t.netioPath] = bridge
			ll.mu.Unlock()
			// Publish ownership of the kernel device name process-wide, so a later
			// lab evicts this pump instead of colliding with it (and so a stale
			// fault timer can tell that the device is no longer its own).
			claimTap(t.tapName, ll, bridge)
			// Register before starting Run: a pump can fail immediately (for
			// example when the tap disappeared between EnsureTap and openTap),
			// and its eviction must not be beaten by this map insertion.
			go func(path string, lb *labBridge, b *iouyap.TapBridge, ctx context.Context) {
				s.evictTapBridge(ll, path, lb, b.Run(ctx))
			}(t.netioPath, bridge, tb, c)
		}
	}

	// Evict any tapBridges entry whose netioPath is no longer part of the
	// current static-tap set at all — a pump left over from a node/interface
	// that's since been removed (node.remove never tears these down itself).
	// Left alone, an orphan like this can still be holding open the exact tap
	// device a LATER pseudo-instance reshuffle reassigns to a different,
	// still-live interface, producing the same "device or resource busy" this
	// function otherwise guards against above.
	livePaths := make(map[string]bool)
	for _, m := range staticTaps {
		for _, t := range m {
			livePaths[t.netioPath] = true
		}
	}
	ll.mu.Lock()
	for path, lb := range ll.tapBridges {
		if !livePaths[path] {
			_ = lb.close()
			delete(ll.tapBridges, path)
		}
	}
	ll.mu.Unlock()

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
	doc := ll.docSnapshot()
	fabricOK := fabricNodes(doc)
	for i := range doc.Links {
		l := &doc.Links[i]
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
		// Check persisted fault state BEFORE the pure kernel predicate. An
		// admin-down link is intentionally missing bridge membership, but must
		// not be silently healed by a later node.start.
		if f, ok := ll.faultForLink(l.ID); ok && f.Active && f.Fault != nil && f.Fault.Down {
			if err := s.reconcileFabricLinkDown(ll, l, f); err != nil {
				return err
			}
		} else if !s.fabricLinkFullyAttached(ll, l) {
			if err := s.attachFabricLink(ll, l); err != nil {
				return err
			}
		} else if err := s.reconcileLinkFault(ll, l); err != nil {
			return err
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

// evictStaticTaps closes pumps and deletes the tap devices belonging to nodes
// removed from the loaded document. Their paths disappear on refreshFabric, so
// teardownFabric cannot discover them after the fact.
func (s *Server) evictStaticTaps(ll *loadedLab, names []string) {
	if len(names) == 0 {
		return
	}
	want := make(map[string]bool, len(names))
	for _, name := range names {
		want[name] = true
	}
	var bridges []*labBridge
	ll.mu.Lock()
	for path, bridge := range ll.tapBridges {
		if want[bridge.tapName] {
			bridges = append(bridges, bridge)
			delete(ll.tapBridges, path)
		}
	}
	ll.mu.Unlock()
	for _, bridge := range bridges {
		_ = bridge.close()
	}
	mgr := fabric.NewManager()
	ctx := context.Background()
	for name := range want {
		deleteTapVerified(mgr, ctx, name)
	}
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
	// For a tool endpoint this is the bridge-side veth in the root namespace;
	// the kernel puts the bridge master symlink on that end, so this check is
	// the restart-skip proof of actual bridge attachment.
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
	staticTaps := ll.staticTapsSnapshot()
	br, err := fabric.BridgeName(l.ID)
	if err != nil {
		return protocol.Errorf(protocol.CodeBadRequest, "fabric bridge name link %d: %v", l.ID, err)
	}
	existed := tapDeviceExists(br)
	if err := mgr.EnsureBridge(ctx, br); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric bridge %s: %v", br, err)
	}
	// Bridge names are not lab-scoped (iolbr<linkid>, and link ids restart at
	// 0 in every lab), so ADOPTING a pre-existing bridge here can mean we just
	// inherited another lab's (or a crashed previous run's) leftover bridge,
	// members and all. Strip every existing member before the loop below
	// attaches this lab's own endpoints — any member that legitimately
	// belongs to this link (e.g. a partial attach from earlier in this same
	// session) is simply re-attached a moment later.
	if existed {
		if members, err := bridgeMembers(br); err == nil && len(members) > 0 {
			log.Printf("fabric: bridge %s already existed with %d member(s); detaching before rewiring link %d", br, len(members), l.ID)
			for _, m := range members {
				_ = mgr.Detach(ctx, m)
			}
		}
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
		case node != nil && (node.Kind == lab.KindTool || node.Kind == lab.KindPC):
			// Tool: hand the bridge to its netns endpoint. A tool not yet started
			// is skipped; startToolNode attaches it when it comes up.
			nr := ll.get(ep.Node)
			if nr == nil || nr.tool == nil {
				continue
			}
			if err := nr.tool.AttachBridge(br); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric tool %d attach %s: %v", ep.Node, br, err)
			}
		default:
			// IOL: attach its static tap.
			t, ok := tapForEndpoint(staticTaps, ep)
			if !ok {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed,
					"fabric link %d: no static tap for node %d %s", l.ID, ep.Node, ep.Interface)
			}
			if err := retryTransientFabric(func() error { return mgr.Attach(ctx, br, t.tapName) }); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric attach %s->%s: %v", t.tapName, br, err)
			}
		}
	}
	ll.mu.Lock()
	ll.fabricLinks[l.ID] = true
	ll.mu.Unlock()
	s.openLinkDirstat(ll, l)
	s.openLinkSlowTee(ll, l)
	return s.reconcileLinkFault(ll, l)
}

func netemProtocolError(linkID int, dev string, err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unknown qdisc") || strings.Contains(msg, "qdisc kind is unknown") {
		return protocol.Errorf(protocol.CodeUnsupported,
			"link %d: impairment unavailable because the sch_netem kernel module is not available", linkID)
	}
	return protocol.Errorf(protocol.CodeNodeSpawnFailed, "link %d: netem on %s: %v", linkID, dev, err)
}

// clearLinkNetem clears every mapped endpoint device, including devices that
// have disappeared since the fault was applied. ClearNetem deliberately treats
// both missing qdiscs and missing devices as successful no-ops.
func (s *Server) clearLinkNetem(ll *loadedLab, l *lab.Link) error {
	mgr := fabric.NewManager()
	ctx := context.Background()
	for _, e := range s.fabricLinkEndpointDevs(ll, l) {
		if err := mgr.ClearNetem(ctx, e.Dev); err != nil {
			return netemProtocolError(l.ID, e.Dev, err)
		}
	}
	return nil
}

// reconcileLinkFault is the only function that applies an impairment. It
// existence-filters devices, selects by endpoint index, and clears every
// present non-target so edits and late endpoint starts cannot leave stale
// asymmetric qdiscs behind.
func (s *Server) reconcileLinkFault(ll *loadedLab, l *lab.Link) error {
	f, ok := ll.faultForLink(l.ID)
	if !ok || f.Fault == nil || !f.Active || f.Fault.Down {
		return s.clearLinkNetem(ll, l)
	}
	mgr := fabric.NewManager()
	ctx := context.Background()
	netem := faultNetem(f.Fault)
	for _, e := range s.fabricLinkFaultTargets(ll, l) {
		var err error
		if faultTargetsEndpoint(f.Fault, e.EndpointIndex) {
			err = mgr.SetNetem(ctx, e.Dev, netem)
		} else {
			err = mgr.ClearNetem(ctx, e.Dev)
		}
		if err != nil {
			return netemProtocolError(l.ID, e.Dev, err)
		}
	}
	return nil
}

func (s *Server) attachFabricEndpoint(ll *loadedLab, l *lab.Link, epIndex int, br string) error {
	if epIndex < 0 || epIndex >= len(l.Endpoints) {
		return nil
	}
	ep := l.Endpoints[epIndex]
	n := ll.findNode(ep.Node)
	mgr := fabric.NewManager()
	ctx := context.Background()
	staticTaps := ll.staticTapsSnapshot()
	switch {
	case n != nil && n.Kind == lab.KindNAT:
		if nr := ll.get(ep.Node); nr != nil && nr.extnet != nil {
			if err := nr.extnet.AttachBridge(br); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric nat %d attach %s: %v", ep.Node, br, err)
			}
		}
	case n != nil && n.Kind == lab.KindVPCS:
		if nr := ll.get(ep.Node); nr != nil && nr.vtap != nil && nr.vtapName != "" {
			if err := mgr.Attach(ctx, br, nr.vtapName); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric vpcs attach %s->%s: %v", nr.vtapName, br, err)
			}
		}
	case n != nil && (n.Kind == lab.KindTool || n.Kind == lab.KindPC):
		if nr := ll.get(ep.Node); nr != nil && nr.tool != nil {
			if err := nr.tool.AttachBridge(br); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric tool %d attach %s: %v", ep.Node, br, err)
			}
		}
	default:
		t, ok := tapForEndpoint(staticTaps, ep)
		if !ok {
			return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric link %d: no static tap for node %d %s", l.ID, ep.Node, ep.Interface)
		}
		if err := retryTransientFabric(func() error { return mgr.Attach(ctx, br, t.tapName) }); err != nil {
			return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric attach %s->%s: %v", t.tapName, br, err)
		}
	}
	return nil
}

func (s *Server) detachFabricEndpoint(ll *loadedLab, l *lab.Link, epIndex int) {
	if epIndex < 0 || epIndex >= len(l.Endpoints) {
		return
	}
	ep := l.Endpoints[epIndex]
	n := ll.findNode(ep.Node)
	mgr := fabric.NewManager()
	ctx := context.Background()
	staticTaps := ll.staticTapsSnapshot()
	switch {
	case n != nil && n.Kind == lab.KindNAT:
		if nr := ll.get(ep.Node); nr != nil && nr.extnet != nil {
			nr.extnet.DetachBridge()
		}
	case n != nil && n.Kind == lab.KindVPCS:
		if nr := ll.get(ep.Node); nr != nil && nr.vtapName != "" {
			_ = mgr.Detach(ctx, nr.vtapName)
		}
	case n != nil && (n.Kind == lab.KindTool || n.Kind == lab.KindPC):
		if nr := ll.get(ep.Node); nr != nil && nr.tool != nil {
			nr.tool.DetachBridge()
		}
	default:
		if t, ok := tapForEndpoint(staticTaps, ep); ok {
			_ = mgr.Detach(ctx, t.tapName)
		}
	}
}

// reconcileFabricLinkDown realizes an admin-down link without making the
// kernel-state predicate lie: the bridge exists, non-target members remain
// attached, and targeted members are detached. This also leaves the link in
// fabricLinks so dirstat/capture bookkeeping remains coherent.
func (s *Server) reconcileFabricLinkDown(ll *loadedLab, l *lab.Link, f activeFault) error {
	br, err := fabric.BridgeName(l.ID)
	if err != nil {
		return protocol.Errorf(protocol.CodeBadRequest, "fabric bridge name link %d: %v", l.ID, err)
	}
	mgr := fabric.NewManager()
	if err := mgr.EnsureBridge(context.Background(), br); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric bridge %s: %v", br, err)
	}
	if err := s.clearLinkNetem(ll, l); err != nil {
		return err
	}
	for _, e := range s.fabricLinkFaultTargets(ll, l) {
		if faultTargetsEndpoint(f.Fault, e.EndpointIndex) {
			s.detachFabricEndpoint(ll, l, e.EndpointIndex)
		} else if err := s.attachFabricEndpoint(ll, l, e.EndpointIndex, br); err != nil {
			return err
		}
	}
	ll.mu.Lock()
	ll.fabricLinks[l.ID] = true
	ll.mu.Unlock()
	s.openLinkDirstat(ll, l)
	s.openLinkSlowTee(ll, l)
	return s.reconcileLinkFault(ll, l)
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
	indexed := s.fabricLinkEndpointDevs(ll, l)
	devs := make([]dirstat.EndpointDev, 0, len(indexed))
	for _, e := range indexed {
		devs = append(devs, dirstat.EndpointDev{Index: e.EndpointIndex, Dev: e.Dev})
	}
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

	// Conscious skip: linkIsIOLToIOL excludes tool endpoints, which have no
	// LACP slow-protocol tee.
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
	doc := ll.docSnapshot()
	fabricOK := fabricNodes(doc)
	for i := range doc.Links {
		l := &doc.Links[i]
		if !isFabricLink(l, fabricOK) {
			continue
		}
		for _, ep := range l.Endpoints {
			if ep.Node == nodeID {
				if f, ok := ll.faultForLink(l.ID); ok && f.Active && f.Fault != nil && f.Fault.Down {
					if err := s.reconcileFabricLinkDown(ll, l, f); err != nil {
						return err
					}
				} else if !s.fabricLinkFullyAttached(ll, l) {
					if err := s.attachFabricLink(ll, l); err != nil {
						return err
					}
				} else if err := s.reconcileLinkFault(ll, l); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// detachFabricLink removes a fabric link's bridge (detaching its taps first). The
// taps themselves persist — the interfaces are simply unconnected again, still
// fabric-eligible and ready to be reattached to a new link with no restart.
func (s *Server) detachFabricLink(ll *loadedLab, l *lab.Link) {
	s.cancelFaultTimer(ll, l.ID)
	if err := s.clearLinkNetem(ll, l); err != nil {
		log.Printf("fabric: link %d: clear netem during detach: %v", l.ID, err)
	}
	// Stop any bridge capture first (the bridge is about to go away).
	s.stopBridgeCapture(ll, l.ID)
	// Stop the directional classifier: its taps are about to be detached/removed.
	s.closeLinkDirstat(ll, l.ID)
	// Stop the LACP slow-protocols tee for the same reason.
	s.closeLinkSlowTee(ll, l.ID)
	mgr := fabric.NewManager()
	ctx := context.Background()
	staticTaps := ll.staticTapsSnapshot()
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
		case node != nil && (node.Kind == lab.KindTool || node.Kind == lab.KindPC):
			if nr := ll.get(ep.Node); nr != nil && nr.tool != nil {
				nr.tool.DetachBridge()
			}
		default:
			if t, ok := tapForEndpoint(staticTaps, ep); ok {
				_ = mgr.Detach(ctx, t.tapName)
			}
		}
	}
	if br, err := fabric.BridgeName(l.ID); err == nil {
		deleteBridgeVerified(mgr, ctx, br)
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
	existed := tapDeviceExists(tapName)
	if err := retryTransientFabric(func() error { return mgr.EnsureTap(ctx, tapName, currentUID()) }); err != nil {
		s.udpPorts.Release(vpcsBind)
		s.udpPorts.Release(shimBind)
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "vpcs %d tap %s: %v", n.ID, tapName, err)
	}
	// Same leftover-bridge-membership hazard as the IOL static taps (see
	// startFabric): a VPCS shim tap name is also unscoped by lab id.
	if existed && tapHasMaster(tapName) {
		log.Printf("fabric: vpcs tap %s already existed and was still bridged; detaching stale membership", tapName)
		_ = mgr.Detach(ctx, tapName)
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
	// attrib is the slowly-changing source-MAC hint sent with link.stats.
	attrib []dirstat.EndpointAttrib
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
			fs.attrib = dc.Attribution()
		}
		out[id] = fs
	}
	return out
}

// fabricLinkTapDevs returns the tap device names of a fabric link's endpoints:
// an IOL interface's static tap, a VPCS node's shim tap, a NAT node's tap, or
// a running tool's root-side veth.
func (s *Server) fabricLinkTapDevs(ll *loadedLab, l *lab.Link) []string {
	indexed := s.fabricLinkEndpointDevs(ll, l)
	devs := make([]string, 0, len(indexed))
	for _, e := range indexed {
		devs = append(devs, e.Dev)
	}
	return devs
}

// fabricLinkEndpointDevs is the endpoint-indexed form of the existing compact
// device lookup. It intentionally preserves the old skip rules so stats,
// dirstat and slowtee consumers see byte-identical slices through the wrapper.
func (s *Server) fabricLinkEndpointDevs(ll *loadedLab, l *lab.Link) []endpointDev {
	var devs []endpointDev
	staticTaps := ll.staticTapsSnapshot()
	for endpointIndex, ep := range l.Endpoints {
		node := ll.findNode(ep.Node)
		switch {
		case node != nil && node.Kind == lab.KindVPCS:
			if nr := ll.get(ep.Node); nr != nil && nr.vtapName != "" {
				devs = append(devs, endpointDev{EndpointIndex: endpointIndex, Dev: nr.vtapName})
			}
		case node != nil && node.Kind == lab.KindNAT:
			devs = append(devs, endpointDev{EndpointIndex: endpointIndex, Dev: "iolnat" + strconv.Itoa(ep.Node)}) // matches extnet tapName
		case node != nil && (node.Kind == lab.KindTool || node.Kind == lab.KindPC):
			if nr := ll.get(ep.Node); nr != nil && nr.tool != nil {
				devs = append(devs, endpointDev{EndpointIndex: endpointIndex, Dev: tool.HostVethName(ep.Node)})
			}
		default:
			if t, ok := tapForEndpoint(staticTaps, ep); ok {
				devs = append(devs, endpointDev{EndpointIndex: endpointIndex, Dev: t.tapName})
			}
		}
	}
	return devs
}

// fabricLinkFaultTargets filters the endpoint-indexed mapping by actual
// kernel device existence. NAT contributes a name even while stopped for the
// existing stats semantics, but a fault must never send that phantom name to
// tc; it is picked up when the endpoint starts.
func (s *Server) fabricLinkFaultTargets(ll *loadedLab, l *lab.Link) []endpointDev {
	all := s.fabricLinkEndpointDevs(ll, l)
	out := make([]endpointDev, 0, len(all))
	for _, e := range all {
		if tapDeviceExists(e.Dev) {
			out = append(out, e)
		}
	}
	return out
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
		deleteTapVerified(fabric.NewManager(), context.Background(), nr.vtapName)
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
	for id, f := range ll.linkFaults {
		if f.Timer != nil {
			f.Timer.Stop()
			f.Timer = nil
		}
		f.Active = false
		ll.linkFaults[id] = f
	}
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
	// Clear qdiscs before endpoint teardown removes veths/taps. Missing-device
	// errors are intentionally benign inside Manager.ClearNetem, so this is
	// safe after a partial startup or crash and safe to repeat.
	for i := range ll.doc.Links {
		s.cancelFaultTimer(ll, ll.doc.Links[i].ID)
		if err := s.clearLinkNetem(ll, &ll.doc.Links[i]); err != nil {
			log.Printf("fabric: teardown: clear link %d netem: %v", ll.doc.Links[i].ID, err)
		}
	}
	for id := range links {
		if br, err := fabric.BridgeName(id); err == nil {
			deleteBridgeVerified(mgr, ctx, br)
		}
	}
	for _, m := range taps {
		for _, t := range m {
			deleteTapVerified(mgr, ctx, t.tapName)
		}
	}
	for _, nr := range nodes {
		if nr.tool != nil {
			_ = nr.tool.Stop()
			nr.tool = nil
		}
		if nr.vtap != nil || nr.vtapName != "" {
			s.teardownVPCS(nr)
		}
	}
}

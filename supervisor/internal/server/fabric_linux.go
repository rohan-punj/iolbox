//go:build linux

package server

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/rohanpunj/iolab/supervisor/internal/bcap"
	"github.com/rohanpunj/iolab/supervisor/internal/fabric"
	"github.com/rohanpunj/iolab/supervisor/internal/iouyap"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
	"github.com/rohanpunj/iolab/supervisor/internal/vtap"
)

// startFabric realises the static-tap fabric for the current plan, BEFORE any
// IOL spawns: it pre-creates every fabric-eligible IOL interface's tap
// (persistent, owned by this uid, unbridged) and starts a netio<->tap iouyap for
// it, so the pseudo-instance's /tmp/netio<uid>/<pseudo> socket exists when IOL
// connects to it per its static NETMAP line. It then (re)attaches each fabric
// link's member taps to their br-<linkid> bridge. Idempotent across restarts
// and mid-session link.add: EnsureTap/EnsureBridge tolerate existing objects,
// and an already-running tap bridge is left as-is. Called by prepareLabDir.
func (s *Server) startFabric(ll *loadedLab) error {
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
			if err := mgr.EnsureTap(ctx, t.tapName, uid); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric tap %s: %v", t.tapName, err)
			}
			ll.mu.Lock()
			_, exists := ll.tapBridges[t.netioPath]
			ll.mu.Unlock()
			if exists {
				continue
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

	// Attach every fabric link's taps to its bridge, and (re)start a bridge
	// capture for any fabric link that already has an armed capture port (doc
	// capture.enabled auto-armed at lab start, or a capture that survived a
	// hot-connect). The bridge exists now, so tcpdump can attach.
	fabricOK := fabricNodes(ll.doc)
	for i := range ll.doc.Links {
		l := &ll.doc.Links[i]
		if !isFabricLink(l, fabricOK) {
			continue
		}
		if err := s.attachFabricLink(ll, l); err != nil {
			return err
		}
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
	return nil
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
	nodes := make([]*nodeRuntime, 0, len(ll.nodes))
	for _, nr := range ll.nodes {
		nodes = append(nodes, nr)
	}
	ll.mu.Unlock()

	for _, c := range caps {
		_ = c.Close()
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

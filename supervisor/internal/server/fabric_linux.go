//go:build linux

package server

import (
	"context"
	"os"

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
// IOL<->IOL link's two taps to their br-<linkid> bridge. Idempotent across
// restarts and mid-session link.add: EnsureTap/EnsureBridge tolerate existing
// objects, and an already-running tap bridge is left as-is. Called by
// prepareLabDir after startBridges.
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

	// Attach every fabric link's taps to its bridge.
	fabricOK := fabricNodes(ll.doc)
	for i := range ll.doc.Links {
		l := &ll.doc.Links[i]
		if !isFabricLink(l, fabricOK) {
			continue
		}
		if err := s.attachFabricLink(ll, l); err != nil {
			return err
		}
	}
	return nil
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
	nodes := make([]*nodeRuntime, 0, len(ll.nodes))
	for _, nr := range ll.nodes {
		nodes = append(nodes, nr)
	}
	ll.mu.Unlock()

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

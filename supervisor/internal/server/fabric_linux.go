//go:build linux

package server

import (
	"context"
	"os"

	"github.com/rohanpunj/iolab/supervisor/internal/fabric"
	"github.com/rohanpunj/iolab/supervisor/internal/iouyap"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
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
		// IOL endpoint: attach its static tap. NAT endpoint: hand the bridge to
		// its extnet endpoint, which attaches its own tap + moves the gateway/NAT
		// onto the bridge. A NAT not yet started is skipped here — startExtnetNode
		// attaches it when it comes up (this whole function is idempotent).
		if node := ll.findNode(ep.Node); node != nil && node.Kind == lab.KindNAT {
			nr := ll.get(ep.Node)
			if nr == nil || nr.extnet == nil {
				continue
			}
			if err := nr.extnet.AttachBridge(br); err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric nat %d attach %s: %v", ep.Node, br, err)
			}
			continue
		}
		t, ok := tapForEndpoint(ll.staticTaps, ep)
		if !ok {
			return protocol.Errorf(protocol.CodeNodeSpawnFailed,
				"fabric link %d: no static tap for node %d %s", l.ID, ep.Node, ep.Interface)
		}
		if err := mgr.Attach(ctx, br, t.tapName); err != nil {
			return protocol.Errorf(protocol.CodeNodeSpawnFailed, "fabric attach %s->%s: %v", t.tapName, br, err)
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
		// NAT endpoint: unwire its gateway/NAT + detach its tap via extnet (must
		// happen while the bridge still exists). IOL endpoint: detach its tap.
		if node := ll.findNode(ep.Node); node != nil && node.Kind == lab.KindNAT {
			if nr := ll.get(ep.Node); nr != nil && nr.extnet != nil {
				nr.extnet.DetachBridge()
			}
			continue
		}
		if t, ok := tapForEndpoint(ll.staticTaps, ep); ok {
			_ = mgr.Detach(ctx, t.tapName)
		}
	}
	if br, err := fabric.BridgeName(l.ID); err == nil {
		_ = mgr.DeleteBridge(ctx, br)
	}
	ll.mu.Lock()
	delete(ll.fabricLinks, l.ID)
	ll.mu.Unlock()
}

// teardownFabric closes every netio<->tap iouyap, deletes every fabric bridge,
// and removes every static tap. Called on full lab teardown (stopBridges), so no
// fabric kernel objects or pump goroutines leak. Taps/bridges are recreated
// idempotently on the next start.
func (s *Server) teardownFabric(ll *loadedLab) {
	ll.mu.Lock()
	tbs := ll.tapBridges
	ll.tapBridges = make(map[string]*labBridge)
	links := ll.fabricLinks
	ll.fabricLinks = make(map[int]bool)
	taps := ll.staticTaps
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
}

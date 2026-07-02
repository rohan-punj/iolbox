//go:build linux

package server

import (
	"context"
	"os"

	"github.com/rohanpunj/iolab/supervisor/internal/iouyap"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// currentUID returns the process uid, which names the netio socket directory
// /tmp/netio<uid> IOL and the iouyap bridges share (confirmed via lsof on real
// IOL, docs/p0-spike.md).
func currentUID() int { return os.Getuid() }

// startBridges creates the netio dir and starts one iouyap bridge per bridged
// IOL endpoint in ll.bridge, BEFORE any IOL spawns, so the pseudo-instance's
// unix socket exists when IOL connects to it (per its NETMAP line). Idempotent:
// a bridge already running for a netio path is left as-is. Called by
// prepareLabDir on Linux after rebuildBridgePlan.
func (s *Server) startBridges(ll *loadedLab) error {
	if ll.bridge == nil {
		return nil
	}
	// /tmp/netio<uid> must exist and be writable before iouyap.New binds a socket
	// inside it (and before IOL, which binds its own <instance> socket there).
	dir := netioDir(currentUID())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return protocol.Errorf(protocol.CodeNodeSpawnFailed, "netio dir %s: %v", dir, err)
	}

	for i := range ll.bridge.links {
		for _, be := range ll.bridge.links[i].endpoints {
			if !be.isIOL {
				continue
			}
			ll.mu.Lock()
			_, exists := ll.bridges[be.netioPath]
			ll.mu.Unlock()
			if exists {
				continue
			}
			cfg := iouyap.Config{
				NetioPath:      be.iouyap.NetioPath,
				UDPLocal:       be.iouyap.UDPLocal,
				UDPRemote:      be.iouyap.UDPRemote,
				Host:           be.iouyap.Host,
				LocalInstance:  be.iouyap.LocalInstance,
				LocalAdapter:   be.iouyap.LocalAdapter,
				LocalPort:      be.iouyap.LocalPort,
				PseudoInstance: be.iouyap.PseudoInstance,
			}
			br, err := iouyap.New(cfg)
			if err != nil {
				return protocol.Errorf(protocol.CodeNodeSpawnFailed, "iouyap %s: %v", be.netioPath, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			go func(b *iouyap.Bridge, c context.Context) { _ = b.Run(c) }(br, ctx)
			lb := &labBridge{netioPath: be.netioPath, cancel: cancel, closer: br}
			ll.mu.Lock()
			ll.bridges[be.netioPath] = lb
			ll.mu.Unlock()
		}
	}
	return nil
}

// stopBridges closes every iouyap bridge tracked for the lab. Called on lab/node
// teardown (shutdown, stopAll) so no netio sockets or pump goroutines leak.
func (s *Server) stopBridges(ll *loadedLab) {
	ll.mu.Lock()
	bridges := ll.bridges
	ll.bridges = make(map[string]*labBridge)
	ll.mu.Unlock()
	for _, b := range bridges {
		_ = b.close()
	}
}

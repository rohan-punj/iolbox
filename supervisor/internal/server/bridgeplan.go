package server

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/netmap"
)

// labBridge is one running netio<->tap iouyap bridge tracked in a lab's
// lifecycle. The closer field abstracts internal/iouyap.TapBridge so this type
// (and loadedLab) compile on any OS; only fabric_linux.go populates it with a
// real bridge. cancel stops the bridge's Run goroutine.
type labBridge struct {
	netioPath string
	tapName   string
	cancel    context.CancelFunc
	closer    interface{ Close() error }
}

func (s *Server) evictTapBridge(ll *loadedLab, path string, bridge *labBridge, runErr error) {
	ll.mu.Lock()
	current, ok := ll.tapBridges[path]
	if ok && current == bridge {
		delete(ll.tapBridges, path)
	}
	ll.mu.Unlock()
	if !ok || current != bridge {
		return
	}
	if runErr != nil {
		log.Printf("fabric: iouyap %s exited; evicting bridge: %v", path, runErr)
	} else {
		log.Printf("fabric: iouyap %s exited; evicting bridge", path)
	}
	_ = bridge.close()
}

// close cancels the bridge's pump loop and closes its sockets/socket file. Safe
// to call once per bridge on stop. Every pump-removal path (startFabric's stale
// /ghost eviction, the orphan sweep, evictTapBridge, evictStaticTaps,
// teardownFabric) funnels through here, so this is also the single place the
// process-global tap-name claim is released (see tapowner.go, finding #9).
func (b *labBridge) close() error {
	if b == nil {
		return nil
	}
	releaseTap(b.tapName, b)
	if b.cancel != nil {
		b.cancel()
	}
	if b.closer != nil {
		return b.closer.Close()
	}
	return nil
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

// refreshFabric recomputes the whole-lab static-tap fabric identities from the
// current lab doc. It is deterministic and topology-INDEPENDENT (drawn from the
// node set + adapter counts alone), so a given interface's tap/pseudo-instance
// is stable across every rebuild for the lab's lifetime — the property that
// lets a link drawn to a running IOL be realised as a pure tap-to-bridge attach
// with no NETMAP re-read / node restart. Called before every (re)start and on
// any node/link doc change.
func (s *Server) refreshFabric(ll *loadedLab) {
	doc := ll.docSnapshot()
	taps := computeStaticTaps(doc, currentUID())
	ll.mu.Lock()
	ll.staticTaps = taps
	ll.mu.Unlock()
}

package server

import (
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rohanpunj/iolab/supervisor/internal/extnet"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
)

// loadedLab holds runtime state for one loaded lab: the document, per-node
// console ports and runtime records, and capture bookkeeping.
type loadedLab struct {
	doc *lab.Lab

	mu       sync.Mutex
	nodes    map[int]*nodeRuntime // by node id
	captures map[int]int          // linkID -> capturePort
	runDir   string

	// bridge is the whole-lab bridged-link wiring (pseudo-instances + iouyap +
	// relay pairing). It is (re)computed by prepareLabDir before every spawn so
	// the NETMAP and the iouyap bridges agree; nil until first prepared.
	bridge *bridgePlan
	// bridges holds the live iouyap bridges started for this lab, keyed by netio
	// socket path, so they can be Closed on stop (Linux only). Guarded by mu.
	bridges map[string]*labBridge
	// assigns is each bridged link's STICKY data-plane identity (relay UDP port
	// pair per endpoint + pseudo-instance per IOL endpoint), keyed by link id.
	// Once assigned, a link keeps these values across every plan rebuild for the
	// lab's lifetime; only a link removed from the doc (or reshaped) releases
	// them. Without stickiness, rebuilds re-allocated in link-id order, so ANY
	// change to the link set (a mid-session link.remove, say) shifted every
	// later link's ports/pseudos — silently desyncing long-running endpoints
	// whose configs were frozen at start (VPCS argv, NAT/MGMT endpoint, IOL
	// NETMAPs written at earlier boots, stale iouyap bridges). Observed as "R1
	// stops getting DHCP offers after some topology edits". Accessed only from
	// the dispatch path (like doc); not guarded by mu.
	assigns map[int]*linkAssign
}

// nodeRuntime is the runtime state of a single node.
type nodeRuntime struct {
	id          int
	consolePort int
	machine     *node.Machine
	proc        *node.Process // nil until started (Linux only); IOL/VPCS
	imageID     string
	ram         int

	// extnet is the running tap/macvtap endpoint for a nat/mgmt node (nil for
	// IOL/VPCS and until started). It is process-less: it owns an fd + pump
	// goroutines the server drives via Start/Close, but the node state machine
	// still reports running/stopped like any other node.
	extnet *extnet.Endpoint
	// natSubnet is the allocated 172.31.<n>.0/24 index for a nat node (0 until
	// started / for non-nat nodes), released back to the server on stop.
	natSubnet int
}

func newLoadedLab(doc *lab.Lab, runDir string) *loadedLab {
	return &loadedLab{
		doc:      doc,
		nodes:    make(map[int]*nodeRuntime),
		captures: make(map[int]int),
		runDir:   runDir,
		bridges:  make(map[string]*labBridge),
		assigns:  make(map[int]*linkAssign),
	}
}

// labDir returns the SHARED per-lab working directory. Every IOL instance in a
// lab runs with this as its cwd so that (a) they all read the one whole-lab
// NETMAP file, (b) they share the one iourc license, and (c) their unix-socket
// netio endpoints land in the same directory and can therefore find each other
// for native same-host IOL<->IOL links (confirmed in P0). Per-node NVRAM files
// (nvram_<id>) also live here.
func (ll *loadedLab) labDir() string {
	return filepath.Join(ll.runDir, ll.doc.ID)
}

// workDir returns the working directory a node is spawned in.
//
// IOL nodes share ll.labDir() (see labDir for why: co-located netio sockets +
// one NETMAP + one iourc). VPCS and any other UDP-tunnelled node get their own
// per-node subdir, since they never participate in native netio and don't need
// to see the NETMAP file.
func (ll *loadedLab) workDir(nodeID int) string {
	if n := ll.findNode(nodeID); n != nil && n.Kind == lab.KindIOL {
		return ll.labDir()
	}
	return filepath.Join(ll.labDir(), "n"+strconv.Itoa(nodeID))
}

// get returns the runtime for a node id, or nil.
func (ll *loadedLab) get(id int) *nodeRuntime {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	return ll.nodes[id]
}

// stopAll stops every running node.
func (ll *loadedLab) stopAll() {
	ll.mu.Lock()
	nodes := make([]*nodeRuntime, 0, len(ll.nodes))
	for _, nr := range ll.nodes {
		nodes = append(nodes, nr)
	}
	ll.mu.Unlock()
	for _, nr := range nodes {
		if nr.proc != nil {
			_ = nr.proc.Stop()
		}
		if nr.extnet != nil {
			_ = nr.extnet.Close()
			nr.extnet = nil
			nr.machine.To(node.StateStopped)
		}
	}
}

// findNode returns the lab node document by id.
func (ll *loadedLab) findNode(id int) *lab.Node {
	for i := range ll.doc.Nodes {
		if ll.doc.Nodes[i].ID == id {
			return &ll.doc.Nodes[i]
		}
	}
	return nil
}

// findLink returns the lab link document by id.
func (ll *loadedLab) findLink(id int) *lab.Link {
	for i := range ll.doc.Links {
		if ll.doc.Links[i].ID == id {
			return &ll.doc.Links[i]
		}
	}
	return nil
}

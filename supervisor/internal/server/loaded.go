package server

import (
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rohanpunj/iolab/supervisor/internal/bcap"
	"github.com/rohanpunj/iolab/supervisor/internal/dirstat"
	"github.com/rohanpunj/iolab/supervisor/internal/extnet"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
	"github.com/rohanpunj/iolab/supervisor/internal/vtap"
)

// loadedLab holds runtime state for one loaded lab: the document, per-node
// console ports and runtime records, and capture bookkeeping.
type loadedLab struct {
	doc *lab.Lab

	mu       sync.Mutex
	nodes    map[int]*nodeRuntime // by node id
	captures map[int]int          // linkID -> capturePort
	runDir   string

	// staticTaps is the whole-lab STATIC-TAP FABRIC: every IOL interface's stable
	// {tap, pseudo-instance, netio path} identity, keyed by node id then canonical
	// interface string. Recomputed deterministically from the node set + adapter
	// counts alone (never the link set) on every refreshFabric, so it never
	// changes while a lab runs — which is what lets a link drawn to a running IOL
	// be realised as a pure tap-to-bridge attach with no NETMAP re-read / node
	// restart. See fabric.go (computeStaticTaps).
	staticTaps map[int]map[string]ifaceTap
	// tapBridges holds the live netio<->tap iouyap bridges (one per static tap),
	// keyed by netio socket path, so they can be Closed on stop (Linux only).
	// Guarded by mu.
	tapBridges map[string]*labBridge
	// fabricLinks records which link ids currently have a Linux bridge created +
	// their endpoint taps attached. Guarded by mu.
	fabricLinks map[int]bool
	// bcaps holds the live bridge captures (tcpdump -i br-<linkid> -> pcapng TCP
	// server) for links that have an active capture, keyed by link id. Guarded by
	// mu.
	bcaps map[int]*bcap.Capture
	// dirstats holds the always-on per-endpoint-tap directional classifier for
	// each fabric link (one per link id), opened at attach and closed at
	// detach/teardown, so link.stats can carry per-direction per-protocol rates
	// whether or not a bridge capture is running. A nil entry (or a link with no
	// classifier, e.g. the non-root dev box) simply yields no directional data.
	// Guarded by mu (Linux only; the classifier is a no-op stub off Linux).
	dirstats map[int]*dirstat.Classifier
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

	// vtap is the running UDP<->tap shim for a FABRIC VPCS node (nil for legacy
	// VPCS and non-VPCS nodes). VPCS speaks UDP; the shim bridges its UDP tunnel
	// to a tap that joins the link bridge, so a VPCS on the fabric hot-connects
	// like an IOL. vtapName is its tap device; vtapPorts is [vpcsBind, shimBind]
	// (the udp port pair), held so buildSpec can pass them to VPCS's -s/-c and
	// stop can release them.
	vtap      *vtap.Shim
	vtapName  string
	vtapPorts [2]int
}

func newLoadedLab(doc *lab.Lab, runDir string) *loadedLab {
	return &loadedLab{
		doc:      doc,
		nodes:    make(map[int]*nodeRuntime),
		captures: make(map[int]int),
		runDir:   runDir,

		staticTaps:  make(map[int]map[string]ifaceTap),
		tapBridges:  make(map[string]*labBridge),
		fabricLinks: make(map[int]bool),
		bcaps:       make(map[int]*bcap.Capture),
		dirstats:    make(map[int]*dirstat.Classifier),
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
		if nr.vtap != nil {
			// Stop the VPCS shim's pump goroutines. The tap device + udp ports are
			// reclaimed by teardownFabric / stopNode (mirrors extnet, whose subnet
			// index is likewise released by stopNode not here).
			_ = nr.vtap.Close()
			nr.vtap = nil
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

package server

import (
	"path/filepath"
	"strconv"
	"sync"

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
}

// nodeRuntime is the runtime state of a single node.
type nodeRuntime struct {
	id          int
	consolePort int
	machine     *node.Machine
	proc        *node.Process // nil until started (Linux only)
	imageID     string
	ram         int
}

func newLoadedLab(doc *lab.Lab, runDir string) *loadedLab {
	return &loadedLab{
		doc:      doc,
		nodes:    make(map[int]*nodeRuntime),
		captures: make(map[int]int),
		runDir:   runDir,
	}
}

// workDir returns the per-node working directory (holds NETMAP, iourc, nvram).
func (ll *loadedLab) workDir(nodeID int) string {
	return filepath.Join(ll.runDir, ll.doc.ID, "n"+strconv.Itoa(nodeID))
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

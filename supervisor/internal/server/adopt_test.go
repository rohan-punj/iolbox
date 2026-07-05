package server

import (
	"encoding/json"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// ---- sameTopology unit tests (WS2) ----

func twoNodeLab(id string) *lab.Lab {
	return &lab.Lab{
		Version: 1, ID: id, Name: "n",
		Nodes: []lab.Node{
			{ID: 0, Kind: lab.KindIOL, Name: "R1", Image: &lab.ImageRef{ID: "img-a"}},
			{ID: 1, Kind: lab.KindVPCS, Name: "PC1"},
		},
		Links: []lab.Link{
			{ID: 0, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
				{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"},
			}},
		},
	}
}

func TestSameTopologyIdentical(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	if !sameTopology(a, b) {
		t.Fatal("identical docs must compare equal")
	}
}

func TestSameTopologyChangedNodeKind(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	b.Nodes[1].Kind = lab.KindNAT
	if sameTopology(a, b) {
		t.Fatal("changed node kind must NOT be adoptable")
	}
}

func TestSameTopologyChangedImage(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	b.Nodes[0].Image = &lab.ImageRef{ID: "img-b"}
	if sameTopology(a, b) {
		t.Fatal("changed image id must NOT be adoptable")
	}
}

func TestSameTopologyChangedIfaceCount(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	two := 2
	b.Nodes[0].Ethernet = &two
	if sameTopology(a, b) {
		t.Fatal("changed ethernet adapter count must NOT be adoptable")
	}
}

func TestSameTopologyAddedLink(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	b.Links = append(b.Links, lab.Link{ID: 1, Endpoints: []lab.Endpoint{
		{Node: 0, Interface: "e0/1"}, {Node: 1, Interface: "eth1"},
	}})
	if sameTopology(a, b) {
		t.Fatal("added link must NOT be adoptable")
	}
}

func TestSameTopologyRemovedLink(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	b.Links = nil
	if sameTopology(a, b) {
		t.Fatal("removed link must NOT be adoptable")
	}
}

func TestSameTopologyReorderedNodesAndLinks(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	// Reverse node and link order, and flip one link's endpoint order — none of
	// this changes the SET of nodes/links, only their listed order.
	b.Nodes[0], b.Nodes[1] = b.Nodes[1], b.Nodes[0]
	b.Links[0].Endpoints[0], b.Links[0].Endpoints[1] = b.Links[0].Endpoints[1], b.Links[0].Endpoints[0]
	if !sameTopology(a, b) {
		t.Fatal("reordered-but-same-set nodes/links must still be adoptable")
	}
}

func TestSameTopologyRenamedNodeMovedPosition(t *testing.T) {
	a := twoNodeLab("l")
	b := twoNodeLab("l")
	b.Nodes[0].Name = "R1-renamed"
	b.Nodes[0].X, b.Nodes[0].Y = 999, 888
	if !sameTopology(a, b) {
		t.Fatal("renamed/moved node must still be adoptable (cosmetic only)")
	}
}

// ---- handleLabLoad adopt-path integration test ----

// TestLabLoadAdoptsRunningLabNoTeardown confirms that re-sending the SAME lab
// (same id, same topology, one node actually running) does NOT tear down the
// running node, returns the EXISTING console port (not a freshly allocated
// one), sets Adopted=true, and persists the cosmetic rename into the stored
// doc — the core WS2 guarantee (a GUI refresh / second tab must not kill a
// running lab).
func TestLabLoadAdoptsRunningLabNoTeardown(t *testing.T) {
	s := newTestServer()
	doc := twoNodeLab("lab-adopt")
	ll := loadLab(t, s, doc)

	nr0 := ll.get(0)
	if nr0 == nil {
		t.Fatal("node 0 runtime missing after initial load")
	}
	originalPort := nr0.consolePort
	// Drive node 0 to running so the lab counts as "actually up".
	nr0.machine.To(node.StateStarting)
	nr0.machine.To(node.StateRunning)

	// Re-send the same topology with a cosmetic rename.
	reload := twoNodeLab("lab-adopt")
	reload.Nodes[0].Name = "R1-renamed"
	resp := dispatch(t, s, "lab.load", protocol.LabLoadArgs{Lab: *reload})
	if !resp.OK {
		t.Fatalf("lab.load (adopt) failed: %+v", resp.Error)
	}
	var r protocol.LabLoadResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Adopted {
		t.Fatalf("expected adopted=true, got %+v", r)
	}

	// The SAME loadedLab/runtime must still be in place — no teardown, no
	// fresh nodeRuntime, no fresh console port.
	if s.lab != ll {
		t.Fatal("adopt path must keep the same loadedLab instance (no teardown/reload)")
	}
	nrAfter := ll.get(0)
	if nrAfter != nr0 {
		t.Fatal("adopt path must keep the same nodeRuntime (no teardown/reload)")
	}
	if nrAfter.machine.State() != node.StateRunning {
		t.Fatalf("adopt path must not stop the running node, got state=%s", nrAfter.machine.State())
	}
	var gotPort int
	for _, n := range r.Nodes {
		if n.ID == 0 {
			gotPort = n.ConsolePort
		}
	}
	if gotPort != originalPort {
		t.Fatalf("adopt path must return the EXISTING console port %d, got %d", originalPort, gotPort)
	}

	// The cosmetic rename must have been persisted into the loaded doc.
	if got := ll.findNode(0).Name; got != "R1-renamed" {
		t.Fatalf("adopt path must persist cosmetic doc changes, node 0 name = %q", got)
	}
}

// TestLabLoadDifferentTopologyStillTearsDown is the orphan-fix regression
// guard: loading a DIFFERENT topology under the same lab id must still go
// through the normal teardown-and-reload path (the running node must stop),
// even though the id matches.
func TestLabLoadDifferentTopologyStillTearsDown(t *testing.T) {
	s := newTestServer()
	doc := twoNodeLab("lab-diff")
	ll := loadLab(t, s, doc)
	nr0 := ll.get(0)
	nr0.machine.To(node.StateStarting)
	nr0.machine.To(node.StateRunning)

	changed := twoNodeLab("lab-diff")
	changed.Nodes = changed.Nodes[:1] // drop the vpcs node -> different topology
	changed.Links = nil
	resp := dispatch(t, s, "lab.load", protocol.LabLoadArgs{Lab: *changed})
	if !resp.OK {
		t.Fatalf("lab.load (different topology) failed: %+v", resp.Error)
	}
	var r protocol.LabLoadResult
	json.Unmarshal(resp.Result, &r)
	if r.Adopted {
		t.Fatal("a genuinely different topology must NOT be reported as adopted")
	}
	if s.lab == ll {
		t.Fatal("different topology must replace the loadedLab (teardown path), not adopt")
	}
}

// TestLabLoadIdleLabStillReloads confirms that when nothing is actually
// running (all nodes stopped), re-sending the same topology takes the normal
// reload path rather than adopting — adoption is only meaningful (and only
// needed) when there's a live process to protect.
func TestLabLoadIdleLabStillReloads(t *testing.T) {
	s := newTestServer()
	doc := twoNodeLab("lab-idle")
	ll := loadLab(t, s, doc)
	// Nothing driven to running: every node is stopped.

	resp := dispatch(t, s, "lab.load", protocol.LabLoadArgs{Lab: *twoNodeLab("lab-idle")})
	if !resp.OK {
		t.Fatalf("lab.load (idle reload) failed: %+v", resp.Error)
	}
	var r protocol.LabLoadResult
	json.Unmarshal(resp.Result, &r)
	if r.Adopted {
		t.Fatal("an idle lab reload must not be reported as adopted")
	}
	if s.lab == ll {
		t.Fatal("idle reload must go through the normal replace path")
	}
}

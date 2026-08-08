//go:build linux

package server

import (
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// TestLinkTouchesAny pins the ids-scoping predicate startFabric's link loop
// uses to decide whether an out-of-scope, already-attached link can be
// skipped entirely for a per-node restart.
func TestLinkTouchesAny(t *testing.T) {
	l := &lab.Link{ID: 1, Endpoints: []lab.Endpoint{{Node: 3, Interface: "e0/0"}, {Node: 7, Interface: "e0/0"}}}

	if !linkTouchesAny(l, map[int]bool{7: true}) {
		t.Fatal("link touching node 7 must match idSet{7}")
	}
	if linkTouchesAny(l, map[int]bool{9: true}) {
		t.Fatal("link touching neither 9 must not match idSet{9}")
	}
	if linkTouchesAny(l, map[int]bool{}) {
		t.Fatal("an empty idSet must match nothing (caller treats empty as whole-lab, not via this predicate)")
	}
}

// TestTapMasterIsNoDevice confirms a nonexistent device reads as "not the
// master" (false) rather than panicking or erroring out — startFabric's
// self-heal gate relies on this: a torn-down tap must read as a mismatch, not
// crash the fully-attached check.
func TestTapMasterIsNoDevice(t *testing.T) {
	if tapMasterIs("iol-definitely-does-not-exist-9999", "iolbr1") {
		t.Fatal("a nonexistent device must not report itself as bridged")
	}
}

// TestTapMasterIsUnbridgedLoopback uses the real "lo" interface (always
// present on Linux, never bridged) to confirm an unbridged device reads as a
// mismatch against any bridge name — exercising the real sysfs Readlink path
// (no /master symlink exists for an unbridged device) without needing root or
// a real tap/bridge pair.
func TestTapMasterIsUnbridgedLoopback(t *testing.T) {
	if tapMasterIs("lo", "iolbr1") {
		t.Fatal("loopback is never bridged; must read as a mismatch")
	}
}

// TestFabricLinkFullyAttachedRequiresBookkeeping confirms the first gate: a
// link the bookkeeping map has never marked attached is always a mismatch,
// with no sysfs calls needed (the cheapest possible false).
func TestFabricLinkFullyAttachedRequiresBookkeeping(t *testing.T) {
	s := &Server{}
	doc := &lab.Lab{
		ID:    "test-lab",
		Nodes: []lab.Node{{ID: 1, Kind: lab.KindIOL}, {ID: 2, Kind: lab.KindIOL}},
		Links: []lab.Link{{ID: 5, Endpoints: []lab.Endpoint{{Node: 1, Interface: "e0/0"}, {Node: 2, Interface: "e0/0"}}}},
	}
	ll := newLoadedLab(doc, t.TempDir())
	l := &ll.doc.Links[0]

	if s.fabricLinkFullyAttached(ll, l) {
		t.Fatal("a link never recorded as attached must not read as fully attached")
	}
}

// TestFabricLinkTapDevsTool uses the root-side veth because that is the device
// attached to the bridge and therefore the one whose stats/master symlink prove
// the tool endpoint is wired into the fabric.
func TestFabricLinkTapDevsTool(t *testing.T) {
	doc := &lab.Lab{
		Nodes: []lab.Node{{ID: 17, Kind: lab.KindTool}},
		Links: []lab.Link{{ID: 18, Endpoints: []lab.Endpoint{{Node: 17, Interface: "eth1"}}}},
	}
	ll := newLoadedLab(doc, t.TempDir())
	ll.nodes[17] = &nodeRuntime{tool: &tool.Endpoint{}}

	devs := (&Server{}).fabricLinkTapDevs(ll, &ll.doc.Links[0])
	if len(devs) != 1 || devs[0] != tool.HostVethName(17) {
		t.Fatalf("tool fabric tap = %#v, want [%q]", devs, tool.HostVethName(17))
	}
}

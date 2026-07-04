package server

import (
	"strings"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
)

// TestNetmapIncludesBridgedLines confirms the whole-lab NETMAP carries a
// static-tap line for every fabric IOL interface (and no native direct-netio
// line for a plain IOL<->IOL link, which is a fabric link now).
func TestNetmapIncludesBridgedLines(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "lab-z", Name: "n",
		Nodes: []lab.Node{iolNode(0), iolNode(1), vpcsNode(2)},
		Links: []lab.Link{
			{ID: 0, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
				{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "e0/0"}}},
			{ID: 1, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
				{Node: 0, Interface: "e0/1"}, {Node: 2, Interface: "eth0"}}},
		},
	}
	s := newTestServer()
	ll := newLoadedLab(doc, "/run/iolab")
	if err := s.rebuildBridgePlan(ll); err != nil {
		t.Fatal(err)
	}
	got := s.netmapFor(ll)
	// Link 0 (plain IOL<->IOL) is a FABRIC link: NO native "1:0/0 2:0/0" line;
	// each of its interfaces gets a static-tap line instead.
	if strings.Contains(got, "1:0/0 2:0/0\n") {
		t.Fatalf("fabric IOL<->IOL must not emit a native line: %q", got)
	}
	if !strings.Contains(got, "1:0/0 ") {
		t.Fatalf("static tap line for node0 e0/0 missing: %q", got)
	}
	// Node0 e0/1 (on the VPCS<->IOL fabric link) also gets a static-tap line.
	if !strings.Contains(got, "1:0/1 ") {
		t.Fatalf("static tap line for node0 e0/1 missing: %q", got)
	}
}

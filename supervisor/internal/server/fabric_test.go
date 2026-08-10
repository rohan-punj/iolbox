package server

import (
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

// TestFabricToolEligibility keeps the portable fabric plan aware of tool
// endpoints, so a tool link is admitted before any Linux bridge operation runs.
func TestFabricToolEligibility(t *testing.T) {
	doc := &lab.Lab{
		Nodes: []lab.Node{
			{ID: 1, Kind: lab.KindTool},
			{ID: 2, Kind: lab.KindIOL},
		},
		Links: []lab.Link{{
			ID:        7,
			Endpoints: []lab.Endpoint{{Node: 1, Interface: "eth1"}, {Node: 2, Interface: "e0/0"}},
		}},
	}

	fabricOK := fabricNodes(doc)
	if !fabricOK[1] {
		t.Fatal("tool node must be fabric-eligible")
	}
	if !isFabricLink(&doc.Links[0], fabricOK) {
		t.Fatal("a link between a tool and an IOL node must be fabric-eligible")
	}
}

// TestFabricToolWithoutLink confirms that eligibility does not invent a link
// for an unconnected tool node while preserving its place in the plan inputs.
func TestFabricToolWithoutLink(t *testing.T) {
	doc := &lab.Lab{Nodes: []lab.Node{{ID: 3, Kind: lab.KindTool}}}
	fabricOK := fabricNodes(doc)

	if !fabricOK[3] {
		t.Fatal("an unconnected tool node must remain fabric-eligible")
	}
	if len(doc.Links) != 0 {
		t.Fatal("an unconnected tool node must produce no fabric link")
	}
	if isFabricLink(&lab.Link{Endpoints: []lab.Endpoint{{Node: 3, Interface: "eth1"}}}, fabricOK) {
		t.Fatal("a single tool endpoint must not become a fabric link")
	}
}

// TestFabricExistingKindEligibility prevents the tool addition from changing
// the established IOL, NAT, and VPCS eligibility set or admitting unknown kinds.
func TestFabricExistingKindEligibility(t *testing.T) {
	doc := &lab.Lab{Nodes: []lab.Node{
		{ID: 10, Kind: lab.KindIOL},
		{ID: 11, Kind: lab.KindNAT},
		{ID: 12, Kind: lab.KindVPCS},
		{ID: 13, Kind: lab.KindTool},
		{ID: 14, Kind: lab.Kind("unknown")},
	}}
	fabricOK := fabricNodes(doc)

	for _, id := range []int{10, 11, 12, 13} {
		if !fabricOK[id] {
			t.Fatalf("node %d should be fabric-eligible", id)
		}
	}
	if fabricOK[14] {
		t.Fatal("an unknown node kind must not be fabric-eligible")
	}
	if len(fabricOK) != 4 {
		t.Fatalf("fabric eligibility contains %d nodes, want 4", len(fabricOK))
	}
}

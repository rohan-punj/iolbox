package server

import (
	"encoding/json"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/extnet"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

func natNode(id int) lab.Node  { return lab.Node{ID: id, Kind: lab.KindNAT, Name: "NAT"} }
func mgmtNode(id int) lab.Node { return lab.Node{ID: id, Kind: lab.KindMgmt, Name: "MGMT"} }

// TestHelloAdvertisesGatedFeatures confirms the hello features array carries the
// base set and appends natgw/mgmt exactly when the server's detected caps allow.
func TestHelloAdvertisesGatedFeatures(t *testing.T) {
	s := newTestServer()
	// Inject caps directly (Detect returns all-false off Linux / without sudo).
	s.caps = extnet.Capabilities{NAT: true, Mgmt: true}
	resp := dispatch(t, s, "hello", protocol.HelloArgs{Client: "gui"})
	if !resp.OK {
		t.Fatalf("hello failed: %+v", resp.Error)
	}
	var r protocol.HelloResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, f := range r.Features {
		has[f] = true
	}
	for _, want := range []string{"nvram", "capture", "i386", "natgw", "mgmt"} {
		if !has[want] {
			t.Fatalf("hello features %v missing %q", r.Features, want)
		}
	}

	// With no caps, natgw/mgmt must be absent.
	s.caps = extnet.Capabilities{}
	resp = dispatch(t, s, "hello", protocol.HelloArgs{Client: "gui"})
	json.Unmarshal(resp.Result, &r)
	for _, f := range r.Features {
		if f == "natgw" || f == "mgmt" {
			t.Fatalf("unsupported feature %q advertised", f)
		}
	}
}

// TestStartNatUnsupported confirms lab.start of a nat node on a runtime that did
// not advertise support returns a clear unsupported error.
func TestStartNatUnsupported(t *testing.T) {
	s := newTestServer()
	s.caps = extnet.Capabilities{} // nat/mgmt unsupported
	doc := lab.Lab{Version: 1, ID: "lab-nat", Name: "n",
		Nodes: []lab.Node{iolNode(0), natNode(1)},
		Links: []lab.Link{{ID: 0, Type: lab.LinkP2P,
			Endpoints: []lab.Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"}}}},
	}
	if resp := dispatch(t, s, "lab.load", protocol.LabLoadArgs{Lab: doc}); !resp.OK {
		t.Fatalf("lab.load failed: %+v", resp.Error)
	}
	// Start only the nat node.
	resp := dispatch(t, s, "node.start", protocol.NodeArgs{LabID: "lab-nat", Node: 1})
	if resp.OK || resp.Error.Code != protocol.CodeUnsupported {
		t.Fatalf("expected unsupported for nat start, got %+v", resp)
	}
}

// TestBridgePlanExtnetUDP pins that a nat endpoint is bridged like VPCS: it gets
// relay UDP ports (no pseudo-instance, not marked vpcs), and extnetUDPFor maps
// send=relay.LocalPort / listen=relay.RemotePort.
func TestBridgePlanExtnetUDP(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n",
		Nodes: []lab.Node{iolNode(0), natNode(1)},
		Links: []lab.Link{{ID: 5, Type: lab.LinkP2P,
			Endpoints: []lab.Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"}}}},
	}
	plan, err := buildBridgePlan(doc, 1000, newUDP(), nil, "", map[int]*linkAssign{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.links) != 1 {
		t.Fatalf("expected 1 bridged link, got %d", len(plan.links))
	}
	var natEP *bridgedEndpoint
	for i := range plan.links[0].endpoints {
		ep := &plan.links[0].endpoints[i]
		if ep.kind == lab.KindNAT {
			natEP = ep
		}
	}
	if natEP == nil {
		t.Fatal("nat endpoint missing from plan")
	}
	if natEP.isIOL || natEP.vpcs || natEP.pseudo != 0 {
		t.Fatalf("nat endpoint must be non-IOL, non-vpcs, no pseudo: %+v", natEP)
	}
	send, listen, ok := plan.extnetUDPFor(1)
	if !ok {
		t.Fatal("extnetUDPFor(1) must resolve")
	}
	if send != natEP.relayEP.LocalPort || listen != natEP.relayEP.RemotePort {
		t.Fatalf("extnet UDP mapping wrong: send=%d listen=%d ep=%+v", send, listen, natEP.relayEP)
	}
}

// TestExtnetLinkIsBridged confirms a link touching a nat/mgmt node is bridged
// (never native), like any non-IOL endpoint.
func TestExtnetLinkIsBridged(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n",
		Nodes: []lab.Node{iolNode(0), natNode(1), mgmtNode(2)}}
	isIOL := isIOLMap(doc)
	natLink := lab.Link{ID: 0, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
		{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"}}}
	mgmtLink := lab.Link{ID: 1, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
		{Node: 0, Interface: "e0/1"}, {Node: 2, Interface: "eth0"}}}
	// nat/mgmt endpoints are bridged intrinsically; capture-ready off proves the
	// reason is the non-IOL endpoint, not capture-ready mode.
	if wiringFor(&natLink, isIOL, false) != wiringBridged {
		t.Fatal("nat link must be bridged")
	}
	if wiringFor(&mgmtLink, isIOL, false) != wiringBridged {
		t.Fatal("mgmt link must be bridged")
	}
}

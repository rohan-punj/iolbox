package server

import (
	"encoding/json"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/extnet"
	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

func natNode(id int) lab.Node { return lab.Node{ID: id, Kind: lab.KindNAT, Name: "NAT"} }

// TestHelloAdvertisesGatedFeatures confirms the hello features array carries the
// base set and appends natgw exactly when the server's detected caps allow.
func TestHelloAdvertisesGatedFeatures(t *testing.T) {
	s := newTestServer()
	// Inject caps directly (Detect returns all-false off Linux / without sudo).
	s.caps = extnet.Capabilities{NAT: true}
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
	for _, want := range []string{"nvram", "capture", "i386", "natgw"} {
		if !has[want] {
			t.Fatalf("hello features %v missing %q", r.Features, want)
		}
	}

	// With no caps, natgw must be absent.
	s.caps = extnet.Capabilities{}
	resp = dispatch(t, s, "hello", protocol.HelloArgs{Client: "gui"})
	json.Unmarshal(resp.Result, &r)
	for _, f := range r.Features {
		if f == "natgw" {
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

// TestExtnetLinkIsBridged confirms a link touching a nat node is bridged (never
// native), like any non-IOL endpoint.
func TestExtnetLinkIsBridged(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n",
		Nodes: []lab.Node{iolNode(0), natNode(1)}}
	isIOL := isIOLMap(doc)
	natLink := lab.Link{ID: 0, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
		{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"}}}
	// A nat endpoint is bridged intrinsically; capture-ready off proves the
	// reason is the non-IOL endpoint, not capture-ready mode.
	if wiringFor(&natLink, isIOL, false) != wiringBridged {
		t.Fatal("nat link must be bridged")
	}
}

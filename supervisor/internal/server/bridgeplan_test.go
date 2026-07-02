package server

import (
	"strings"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
	"github.com/rohanpunj/iolab/supervisor/internal/relay"
)

// newUDP returns a fresh UDP port allocator matching the server's real base.
func newUDP() *node.PortAllocator { return node.NewPortAllocator(10000, 20000) }

// TestBridgePlanCapturedIOLtoIOL is the headline case: a capture-enabled
// IOL<->IOL link becomes TWO bridged IOL endpoints -> 2 iouyap bridges + 1 p2p
// relay carrying the pcapng tee. It pins the pseudo-instance assignment, the
// netio socket paths, the iouyap<->relay UDP pairing, and the capture port.
func TestBridgePlanCapturedIOLtoIOL(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n",
		Nodes: []lab.Node{iolNode(0), iolNode(1)},
		Links: []lab.Link{{ID: 7, Type: lab.LinkP2P,
			Capture:   &lab.Capture{Enabled: true},
			Endpoints: []lab.Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "e0/0"}}}},
	}
	captures := map[int]int{7: 5500}
	plan, err := buildBridgePlan(doc, 1000, newUDP(), captures)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.links) != 1 {
		t.Fatalf("expected 1 bridged link, got %d", len(plan.links))
	}
	bl := plan.links[0]
	if bl.linkID != 7 || bl.relayCfg.Kind != relay.KindP2P {
		t.Fatalf("relay cfg wrong: %+v", bl.relayCfg)
	}
	if bl.relayCfg.CapturePort != 5500 {
		t.Fatalf("capture port not threaded into relay cfg: %d", bl.relayCfg.CapturePort)
	}
	if len(bl.endpoints) != 2 || len(bl.relayCfg.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d/%d", len(bl.endpoints), len(bl.relayCfg.Endpoints))
	}

	// Both endpoints are IOL -> both get a pseudo-instance from the reserved
	// pool, distinct, both bound under /tmp/netio1000/.
	a, b := bl.endpoints[0], bl.endpoints[1]
	if !a.isIOL || !b.isIOL {
		t.Fatalf("both endpoints of a captured IOL<->IOL link must be bridged IOL")
	}
	if a.pseudo == b.pseudo {
		t.Fatalf("pseudo-instances must be distinct: %d %d", a.pseudo, b.pseudo)
	}
	if a.pseudo < netmap.PseudoInstanceBase || b.pseudo < netmap.PseudoInstanceBase {
		t.Fatalf("pseudo-instances must come from the reserved pool: %d %d", a.pseudo, b.pseudo)
	}
	// Real instances 1,2 must NOT be reused as pseudo-instances.
	if a.pseudo == 1 || a.pseudo == 2 || b.pseudo == 1 || b.pseudo == 2 {
		t.Fatalf("pseudo-instance collides with a real instance: %d %d", a.pseudo, b.pseudo)
	}
	if !strings.HasPrefix(a.netioPath, "/tmp/netio1000/") && !strings.Contains(a.netioPath, "netio1000") {
		t.Fatalf("netio path not under /tmp/netio1000: %q", a.netioPath)
	}

	// iouyap pairing: each bridge sends to the relay's receiving LocalPort and
	// binds the relay's delivery RemotePort for the SAME endpoint.
	if a.iouyap.UDPRemote != a.relayEP.LocalPort || a.iouyap.UDPLocal != a.relayEP.RemotePort {
		t.Fatalf("endpoint A iouyap<->relay pairing wrong: %+v vs %+v", a.iouyap, a.relayEP)
	}
	if b.iouyap.UDPRemote != b.relayEP.LocalPort || b.iouyap.UDPLocal != b.relayEP.RemotePort {
		t.Fatalf("endpoint B iouyap<->relay pairing wrong: %+v vs %+v", b.iouyap, b.relayEP)
	}
	if a.iouyap.NetioPath != a.netioPath {
		t.Fatalf("iouyap cfg wrong: %+v", a.iouyap)
	}
	// Header addressing: each bridge must deliver frames addressed to ITS real
	// IOL instance+interface, sourced from ITS pseudo-instance (nodes 0,1 ->
	// instances 1,2; both interfaces e0/0).
	if a.iouyap.LocalInstance != 1 || b.iouyap.LocalInstance != 2 {
		t.Fatalf("iouyap LocalInstance wrong: a=%+v b=%+v", a.iouyap, b.iouyap)
	}
	if a.iouyap.LocalAdapter != 0 || a.iouyap.LocalPort != 0 {
		t.Fatalf("iouyap interface coords wrong: %+v", a.iouyap)
	}
	if a.iouyap.PseudoInstance != a.pseudo || b.iouyap.PseudoInstance != b.pseudo {
		t.Fatalf("iouyap PseudoInstance must match the endpoint's pseudo: a=%+v b=%+v", a.iouyap, b.iouyap)
	}
}

// TestBridgePlanVPCStoIOL pins the mixed-segment mapping: the IOL endpoint is
// bridged (pseudo-instance + iouyap), the VPCS endpoint speaks UDP natively with
// send=relay.LocalPort / listen=relay.RemotePort.
func TestBridgePlanVPCStoIOL(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n",
		Nodes: []lab.Node{iolNode(0), vpcsNode(1)},
		Links: []lab.Link{{ID: 3, Type: lab.LinkP2P,
			Endpoints: []lab.Endpoint{{Node: 0, Interface: "e0/1"}, {Node: 1, Interface: "eth0"}}}},
	}
	plan, err := buildBridgePlan(doc, 1000, newUDP(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.links) != 1 {
		t.Fatalf("expected 1 bridged link, got %d", len(plan.links))
	}
	var iolEP, vpcsEP *bridgedEndpoint
	for i := range plan.links[0].endpoints {
		ep := &plan.links[0].endpoints[i]
		if ep.isIOL {
			iolEP = ep
		} else {
			vpcsEP = ep
		}
	}
	if iolEP == nil || vpcsEP == nil {
		t.Fatalf("expected one IOL and one VPCS endpoint")
	}
	if iolEP.pseudo < netmap.PseudoInstanceBase || iolEP.netioPath == "" {
		t.Fatalf("IOL endpoint must be bridged with a pseudo-instance: %+v", iolEP)
	}
	// e0/1 on node 0 -> instance 1, adapter 0, port 1.
	if iolEP.iouyap.LocalInstance != 1 || iolEP.iouyap.LocalAdapter != 0 || iolEP.iouyap.LocalPort != 1 {
		t.Fatalf("IOL endpoint header addressing wrong: %+v", iolEP.iouyap)
	}
	if iolEP.iouyap.PseudoInstance != iolEP.pseudo {
		t.Fatalf("IOL endpoint PseudoInstance must match pseudo: %+v", iolEP.iouyap)
	}
	if !vpcsEP.vpcs || vpcsEP.pseudo != 0 {
		t.Fatalf("VPCS endpoint must not get a pseudo-instance: %+v", vpcsEP)
	}

	send, listen, ok := plan.vpcsUDPFor(1)
	if !ok {
		t.Fatal("vpcsUDPFor(1) must resolve")
	}
	if send != vpcsEP.relayEP.LocalPort || listen != vpcsEP.relayEP.RemotePort {
		t.Fatalf("vpcs UDP mapping wrong: send=%d listen=%d ep=%+v", send, listen, vpcsEP.relayEP)
	}
}

// TestNetmapIncludesBridgedLines confirms the whole-lab NETMAP rendered from the
// plan carries a bridged pseudo-instance line for a VPCS<->IOL link's IOL side,
// in addition to any native lines.
func TestNetmapIncludesBridgedLines(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "lab-z", Name: "n",
		Nodes: []lab.Node{iolNode(0), iolNode(1), vpcsNode(2)},
		Links: []lab.Link{
			{ID: 0, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{ // native
				{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "e0/0"}}},
			{ID: 1, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{ // vpcs -> IOL bridged
				{Node: 0, Interface: "e0/1"}, {Node: 2, Interface: "eth0"}}},
		},
	}
	s := newTestServer()
	ll := newLoadedLab(doc, "/run/iolab")
	if err := s.rebuildBridgePlan(ll); err != nil {
		t.Fatal(err)
	}
	got := s.netmapFor(ll)
	// Native line: instances 1,2 (nodes 0,1). Bridged line: instance 1 e0/1 ->
	// pseudo-instance 500 (first from the pool; reals are 1,2).
	if !strings.Contains(got, "1:0/0 2:0/0\n") {
		t.Fatalf("native line missing: %q", got)
	}
	if !strings.Contains(got, "1:0/1 500:0/0\n") {
		t.Fatalf("bridged pseudo-instance line missing: %q", got)
	}
}

// TestBridgePlanReleasesPortsOnRebuild guards against UDP port leaks across
// restarts: rebuilding the plan releases the previous plan's ports so a second
// build reuses the same low ports.
func TestBridgePlanReleasesPortsOnRebuild(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n",
		Nodes: []lab.Node{iolNode(0), vpcsNode(1)},
		Links: []lab.Link{{ID: 0, Type: lab.LinkP2P,
			Endpoints: []lab.Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"}}}},
	}
	s := newTestServer()
	ll := newLoadedLab(doc, "/run/iolab")
	if err := s.rebuildBridgePlan(ll); err != nil {
		t.Fatal(err)
	}
	first := ll.bridge.udpPorts()
	if err := s.rebuildBridgePlan(ll); err != nil {
		t.Fatal(err)
	}
	second := ll.bridge.udpPorts()
	if len(first) != len(second) || len(first) == 0 {
		t.Fatalf("port counts differ: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("rebuild leaked ports: first %v second %v", first, second)
		}
	}
}

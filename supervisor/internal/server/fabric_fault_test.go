package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

func TestValidateLinkFault(t *testing.T) {
	valid := &lab.LinkFault{DelayMs: 50, JitterMs: 5, LossPct: 1, DuplicatePct: 2, ReorderPct: 3, RateKbit: 1000}
	if err := validateLinkFault(valid, 3); err != nil {
		t.Fatalf("valid full fault rejected: %v", err)
	}
	zero := 0
	if err := validateLinkFault(&lab.LinkFault{LossPct: 20, TargetEndpoint: &zero}, 3); err != nil {
		t.Fatalf("explicit targetEndpoint=0 rejected: %v", err)
	}
	for name, f := range map[string]*lab.LinkFault{
		"reorder without delay": {ReorderPct: 1},
		"down with delay":       {Down: true, DelayMs: 1},
		"jitter without delay":  {JitterMs: 1},
		"bad loss":              {LossPct: 100.01},
		"bad rate":              {RateKbit: 10_000_001},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateLinkFault(f, 2); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
	badTarget := 3
	if err := validateLinkFault(&lab.LinkFault{LossPct: 1, TargetEndpoint: &badTarget}, 3); err == nil || !strings.Contains(err.Error(), "targetEndpoint") {
		t.Fatalf("out-of-range target should name targetEndpoint, got %v", err)
	}
}

func TestLinkFaultExplicitTargetZeroRoundTrips(t *testing.T) {
	zero := 0
	in := &lab.LinkFault{LossPct: 30, TargetEndpoint: &zero}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out lab.LinkFault
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.TargetEndpoint == nil || *out.TargetEndpoint != 0 {
		t.Fatalf("targetEndpoint=0 did not survive JSON round trip: %s -> %#v", b, out)
	}
}

func TestUnsupportedSerialFaultDoesNotMutateState(t *testing.T) {
	doc := &lab.Lab{
		ID:    "serial-fault",
		Nodes: []lab.Node{{ID: 1, Kind: lab.KindIOL}, {ID: 2, Kind: lab.KindIOL}},
		Links: []lab.Link{{ID: 4, Endpoints: []lab.Endpoint{{Node: 1, Interface: "s0/0"}, {Node: 2, Interface: "e0/0"}}}},
	}
	s := newTestServer()
	ll := newLoadedLab(doc, t.TempDir())
	s.lab = ll
	args := protocol.LinkFaultArgs{LabID: doc.ID, Link: 4, Fault: &lab.LinkFault{LossPct: 10}}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleLinkSetFault(raw); err == nil {
		t.Fatal("serial fault unexpectedly accepted")
	} else if pe, ok := err.(*protocol.Error); !ok || pe.Code != protocol.CodeUnsupported || !strings.Contains(pe.Message, "node 1") || !strings.Contains(pe.Message, "s0/0") {
		t.Fatalf("serial fault error = %v, want unsupported naming node/interface", err)
	}
	if doc.Links[0].Fault != nil || len(ll.linkFaults) != 0 {
		t.Fatalf("unsupported fault mutated state: doc=%#v runtime=%#v", doc.Links[0].Fault, ll.linkFaults)
	}
}

func TestLinkSetFaultRejectsOutOfRangeTargetAtHandler(t *testing.T) {
	doc := &lab.Lab{
		ID:    "target-validation",
		Nodes: []lab.Node{{ID: 1, Kind: lab.KindIOL}, {ID: 2, Kind: lab.KindIOL}},
		Links: []lab.Link{{ID: 5, Endpoints: []lab.Endpoint{{Node: 1, Interface: "e0/0"}, {Node: 2, Interface: "e0/0"}}}},
	}
	s := newTestServer()
	ll := newLoadedLab(doc, t.TempDir())
	s.lab = ll
	target := 2
	raw, err := json.Marshal(protocol.LinkFaultArgs{
		LabID: doc.ID,
		Link:  doc.Links[0].ID,
		Fault: &lab.LinkFault{LossPct: 1, TargetEndpoint: &target},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleLinkSetFault(raw); err == nil {
		t.Fatal("out-of-range target unexpectedly accepted")
	} else if pe, ok := err.(*protocol.Error); !ok || pe.Code != protocol.CodeBadRequest || !strings.Contains(pe.Message, "targetEndpoint") {
		t.Fatalf("handler error = %v, want bad_request naming targetEndpoint", err)
	}
	if doc.Links[0].Fault != nil || len(ll.linkFaults) != 0 {
		t.Fatalf("invalid target mutated state: doc=%#v runtime=%#v", doc.Links[0].Fault, ll.linkFaults)
	}
}

func TestEndpointIndexedDeviceMappingPreservesDocumentIndex(t *testing.T) {
	doc := &lab.Lab{
		ID: "fault-index",
		Nodes: []lab.Node{
			{ID: 1, Kind: lab.KindTool},
			{ID: 2, Kind: lab.KindTool},
			{ID: 3, Kind: lab.KindTool},
		},
		Links: []lab.Link{{ID: 7, Endpoints: []lab.Endpoint{
			{Node: 1, Interface: "eth1"},
			{Node: 2, Interface: "eth1"},
			{Node: 3, Interface: "eth1"},
		}}},
	}
	ll := newLoadedLab(doc, t.TempDir())
	// Endpoint 0 is stopped; endpoint 1 and 2 are present. The returned
	// compacted slice must retain 1/2 so targetEndpoint=2 cannot hit endpoint 1.
	ll.nodes[2] = &nodeRuntime{tool: &tool.Endpoint{}}
	ll.nodes[3] = &nodeRuntime{tool: &tool.Endpoint{}}
	devs := (&Server{}).fabricLinkEndpointDevs(ll, &doc.Links[0])
	if len(devs) != 2 || devs[0].EndpointIndex != 1 || devs[1].EndpointIndex != 2 {
		t.Fatalf("endpoint-indexed mapping = %#v, want indexes 1 and 2", devs)
	}
	target := 2
	f := &lab.LinkFault{LossPct: 20, TargetEndpoint: &target}
	for _, d := range devs {
		if got := faultTargetsEndpoint(f, d.EndpointIndex); got != (d.EndpointIndex == 2) {
			t.Fatalf("targetEndpoint=2 selection for %#v = %v", d, got)
		}
	}
	for _, d := range devs {
		if !faultTargetsEndpoint(&lab.LinkFault{LossPct: 20}, d.EndpointIndex) {
			t.Fatalf("omitted targetEndpoint did not select present endpoint %#v", d)
		}
	}
}

func TestInitialFaultActivationIsSeparateFromDefinition(t *testing.T) {
	doc := &lab.Lab{
		ID:    "initial-fault",
		Nodes: []lab.Node{{ID: 1, Kind: lab.KindIOL}, {ID: 2, Kind: lab.KindIOL}},
		Links: []lab.Link{{ID: 9, Endpoints: []lab.Endpoint{{Node: 1, Interface: "e0/0"}, {Node: 2, Interface: "e0/0"}}, Fault: &lab.LinkFault{Down: true, Initial: true}}},
	}
	ll := newLoadedLab(doc, t.TempDir())
	if f, ok := ll.faultForLink(9); !ok || f.Active {
		t.Fatalf("persisted fault must begin inactive before start: %#v %v", f, ok)
	}
	ids := ll.activateInitialFaults()
	if len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("activated ids = %#v, want [9]", ids)
	}
	if f, _ := ll.faultForLink(9); !f.Active {
		t.Fatal("initial fault was not activated")
	}
}

func TestCancelFaultTimersForNodeClearsPendingTimer(t *testing.T) {
	doc := &lab.Lab{
		ID:    "timer-cancel",
		Nodes: []lab.Node{{ID: 1, Kind: lab.KindTool}, {ID: 2, Kind: lab.KindTool}},
		Links: []lab.Link{{ID: 8, Endpoints: []lab.Endpoint{{Node: 1}, {Node: 2}}}},
	}
	ll := newLoadedLab(doc, t.TempDir())
	ll.linkFaults[8] = activeFault{
		Fault: &lab.LinkFault{LossPct: 1},
		Timer: time.AfterFunc(time.Hour, func() {}),
	}
	(&Server{}).cancelFaultTimersForNode(ll, 1)
	ll.mu.Lock()
	timer := ll.linkFaults[8].Timer
	ll.mu.Unlock()
	if timer != nil {
		t.Fatal("node teardown left a pending link fault timer")
	}
}

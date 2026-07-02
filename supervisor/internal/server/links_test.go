package server

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
	"github.com/rohanpunj/iolab/supervisor/internal/node"
	"github.com/rohanpunj/iolab/supervisor/internal/nvram"
)

// twoIOL builds a lab with two IOL nodes (0,1) and an optional third node.
func iolNode(id int) lab.Node {
	return lab.Node{ID: id, Kind: lab.KindIOL, Name: "R", Image: &lab.ImageRef{ID: "img"}}
}

func vpcsNode(id int) lab.Node {
	return lab.Node{ID: id, Kind: lab.KindVPCS, Name: "PC"}
}

func TestWiringForNativeIOLtoIOL(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n",
		Nodes: []lab.Node{iolNode(0), iolNode(1)},
		Links: []lab.Link{{ID: 0, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
			{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "e0/0"},
		}}},
	}
	isIOL := isIOLMap(doc)
	if w := wiringFor(&doc.Links[0], isIOL); w != wiringNative {
		t.Fatalf("plain IOL<->IOL p2p must be native, got %s", w)
	}
}

func TestWiringForBridgedCases(t *testing.T) {
	base := []lab.Node{iolNode(0), iolNode(1), vpcsNode(2)}
	cases := []struct {
		name string
		link lab.Link
	}{
		{"vpcs endpoint", lab.Link{ID: 1, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{
			{Node: 0, Interface: "e0/1"}, {Node: 2, Interface: "eth0"}}}},
		{"segment", lab.Link{ID: 2, Type: lab.LinkSegment, Endpoints: []lab.Endpoint{
			{Node: 0, Interface: "e0/2"}, {Node: 1, Interface: "e0/2"}}}},
		{"capture enabled", lab.Link{ID: 3, Type: lab.LinkP2P,
			Capture: &lab.Capture{Enabled: true},
			Endpoints: []lab.Endpoint{
				{Node: 0, Interface: "e0/3"}, {Node: 1, Interface: "e0/3"}}}},
	}
	doc := &lab.Lab{Version: 1, ID: "l", Name: "n", Nodes: base}
	isIOL := isIOLMap(doc)
	for _, c := range cases {
		link := c.link
		if w := wiringFor(&link, isIOL); w != wiringBridged {
			t.Fatalf("%s must be bridged, got %s", c.name, w)
		}
	}
}

// TestNetmapForOnlyNativeLinks confirms the whole-lab NETMAP contains a line for
// the native IOL<->IOL link and NONE for the VPCS/segment/capture links.
func TestNetmapForOnlyNativeLinks(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "lab-x", Name: "n",
		Nodes: []lab.Node{iolNode(1), iolNode(2), vpcsNode(3)},
		Links: []lab.Link{
			{ID: 0, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{ // native
				{Node: 1, Interface: "e0/0"}, {Node: 2, Interface: "e0/0"}}},
			{ID: 1, Type: lab.LinkP2P, Endpoints: []lab.Endpoint{ // vpcs -> bridged
				{Node: 1, Interface: "e0/1"}, {Node: 3, Interface: "eth0"}}},
			{ID: 2, Type: lab.LinkSegment, Endpoints: []lab.Endpoint{ // segment -> bridged
				{Node: 1, Interface: "e0/2"}, {Node: 2, Interface: "e0/2"}}},
		},
	}
	s := newTestServer()
	ll := newLoadedLab(doc, "/run/iolab")
	got := s.netmapFor(ll)
	// Lab nodes 1,2 -> IOL instances 2,3 (nodeID+1).
	want := "2:0/0 3:0/0\n"
	if got != want {
		t.Fatalf("netmap:\n got %q\nwant %q", got, want)
	}
}

// TestLabDirIsShared confirms every IOL node shares the one lab dir (so their
// netio sockets co-locate) while VPCS gets a per-node subdir.
func TestLabDirIsShared(t *testing.T) {
	doc := &lab.Lab{Version: 1, ID: "lab-y", Name: "n",
		Nodes: []lab.Node{iolNode(1), iolNode(2), vpcsNode(3)}}
	ll := newLoadedLab(doc, "/run/iolab")
	if ll.workDir(1) != ll.labDir() || ll.workDir(2) != ll.labDir() {
		t.Fatalf("IOL nodes must share lab dir: %q %q vs %q", ll.workDir(1), ll.workDir(2), ll.labDir())
	}
	if ll.workDir(3) == ll.labDir() {
		t.Fatalf("VPCS node must get its own subdir, got shared %q", ll.workDir(3))
	}
}

// TestNVRAMInjectionRoundTrip is the platform-independent guarantee behind
// injectNVRAM: the config we would write into nvram_<id> decodes back to the
// same text (the Linux injector uses exactly this codec + size).
func TestNVRAMInjectionRoundTrip(t *testing.T) {
	cfg := "hostname R1\n!\ninterface Ethernet0/0\n ip address 10.0.0.1 255.255.255.252\n no shutdown\nend\n"
	data, err := nvram.Encode(cfg, nvram.Options{Size: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 64*1024 {
		t.Fatalf("nvram not sized to -n: %d", len(data))
	}
	got, err := nvram.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("round-trip mismatch")
	}
}

// TestInstanceIDConsistentAcrossArgvNetmapNvram is the regression guard for the
// "IOL rejects instance id 0" fix: a lab node with id 0 must map to IOL instance
// id 1, and that same instance id must appear in (a) the IOL argv positional,
// (b) the NETMAP node id, and (c) the nvram_<id> filename — all three in sync.
func TestInstanceIDConsistentAcrossArgvNetmapNvram(t *testing.T) {
	const nodeID = 0
	const wantInstance = 1

	if netmap.InstanceID(nodeID) != wantInstance {
		t.Fatalf("node %d must map to instance %d, got %d", nodeID, wantInstance, netmap.InstanceID(nodeID))
	}

	// (a) argv positional (last element) is the instance id, not the node id.
	spec := node.Spec{NodeID: nodeID, Kind: "iol", ImagePath: "/img/L3.bin", Ethernet: 1}
	argv := spec.IOLArgv()
	if last := argv[len(argv)-1]; last != strconv.Itoa(wantInstance) {
		t.Fatalf("argv positional = %q, want %q (instance id, not node id)", last, strconv.Itoa(wantInstance))
	}

	// (b) NETMAP node id for a node-0 endpoint is the instance id.
	nm := netmap.Build([]netmap.LinkSpec{{P2P: true, Endpoints: []netmap.EndpointSpec{
		{NodeID: nodeID, Interface: "e0/0", IsIOL: true},
		{NodeID: 1, Interface: "e0/0", IsIOL: true},
	}}})
	if !strings.HasPrefix(nm, strconv.Itoa(wantInstance)+":0/0 ") {
		t.Fatalf("NETMAP must start with instance id %d: got %q", wantInstance, nm)
	}
	// The peer (lab node 1) must be instance 2 in the same line.
	if !strings.Contains(nm, " 2:0/0\n") {
		t.Fatalf("NETMAP peer must be instance 2: got %q", nm)
	}

	// (c) nvram filename is named for the instance id (5-digit).
	if fn := nvramFilename(nodeID); fn != "nvram_00001" {
		t.Fatalf("nvram filename = %q, want nvram_00001 (instance id)", fn)
	}
}

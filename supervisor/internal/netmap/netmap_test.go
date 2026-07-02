package netmap

import "testing"

func TestParseIface(t *testing.T) {
	cases := []struct {
		in      string
		typ     IfaceType
		adapter int
		port    int
		index   int
	}{
		{"e0/0", Ethernet, 0, 0, 0},
		{"e0/1", Ethernet, 0, 1, 1},
		{"e1/0", Ethernet, 1, 0, 16},
		{"s1/2", Serial, 1, 2, 18},
		{"Ethernet2/3", Ethernet, 2, 3, 35},
		{"Serial0/15", Serial, 0, 15, 15},
	}
	for _, c := range cases {
		i, err := ParseIface(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if i.Type != c.typ || i.Adapter != c.adapter || i.Port != c.port {
			t.Fatalf("%s -> %+v", c.in, i)
		}
		if i.Index() != c.index {
			t.Fatalf("%s index=%d want %d", c.in, i.Index(), c.index)
		}
	}
}

func TestParseIfaceErrors(t *testing.T) {
	for _, in := range []string{"", "x0/0", "e0", "e0/16", "eabc/0", "e0/x", "e-1/0"} {
		if _, err := ParseIface(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestBuildNETMAP(t *testing.T) {
	// Node 0 e0/0 <-> node 1 e0/0 (index 0 both) as p2p.
	// A segment link and a vpcs endpoint must NOT produce NETMAP lines.
	links := []LinkSpec{
		{P2P: true, Endpoints: []EndpointSpec{
			{NodeID: 0, Interface: "e0/0", IsIOL: true},
			{NodeID: 1, Interface: "e0/0", IsIOL: true},
		}},
		{P2P: true, Endpoints: []EndpointSpec{
			{NodeID: 1, Interface: "s1/2", IsIOL: true}, // index 18
			{NodeID: 2, Interface: "s1/3", IsIOL: true}, // index 19
		}},
		{P2P: false, Endpoints: []EndpointSpec{ // segment => no line
			{NodeID: 0, Interface: "e0/1", IsIOL: true},
			{NodeID: 1, Interface: "e0/1", IsIOL: true},
		}},
		{P2P: true, Endpoints: []EndpointSpec{ // vpcs side => not a pair
			{NodeID: 0, Interface: "e0/2", IsIOL: true},
			{NodeID: 3, Interface: "eth0", IsIOL: false},
		}},
	}
	got := Build(links)
	// The NETMAP node id is the IOL *instance* id = nodeID+1 (IOL rejects 0), and
	// the interface token is IOL's adapter/port form: e0/0 -> 0/0, s1/2 -> 1/2.
	// So lab nodes 0,1 -> instances 1,2 and nodes 1,2 -> instances 2,3.
	want := "1:0/0 2:0/0\n2:1/2 3:1/3\n"
	if got != want {
		t.Fatalf("NETMAP mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestBuildMatchesP0Format pins the exact NETMAP line format the P0 manual test
// used to carry traffic between two real IOL 17.18.02 instances. Lab node ids
// 0 and 1 map to IOL instances 1 and 2, producing "1:0/0 2:0/0".
func TestBuildMatchesP0Format(t *testing.T) {
	links := []LinkSpec{
		{P2P: true, Endpoints: []EndpointSpec{
			{NodeID: 0, Interface: "e0/0", IsIOL: true},
			{NodeID: 1, Interface: "e0/0", IsIOL: true},
		}},
	}
	if got := Build(links); got != "1:0/0 2:0/0\n" {
		t.Fatalf("P0 NETMAP format mismatch: got %q want %q", got, "1:0/0 2:0/0\n")
	}
}

// TestAllocPseudoInstances checks the reserved pseudo-instance pool: ids start
// at PseudoInstanceBase, are handed out ascending, and never collide with a real
// instance id already in use.
func TestAllocPseudoInstances(t *testing.T) {
	// No real instances in the pool range: first ids are the base upward.
	got, err := AllocPseudoInstances(map[int]bool{1: true, 2: true}, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{PseudoInstanceBase, PseudoInstanceBase + 1, PseudoInstanceBase + 2}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %v want %v", got, want)
	}

	// A real instance sitting inside the pool must be skipped.
	reals := map[int]bool{PseudoInstanceBase: true, PseudoInstanceBase + 2: true}
	got, err = AllocPseudoInstances(reals, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range got {
		if reals[id] {
			t.Fatalf("pseudo-instance %d collides with a real instance", id)
		}
	}
	if got[0] != PseudoInstanceBase+1 || got[1] != PseudoInstanceBase+3 {
		t.Fatalf("skip-collision alloc wrong: %v", got)
	}

	// Exhaustion: asking for more than the pool can supply errors.
	if _, err := AllocPseudoInstances(nil, MaxIOLInstance); err == nil {
		t.Fatal("expected pool exhaustion error")
	}
}

// TestBuildBridgedLines confirms a bridged IOL endpoint produces the
// "<realInstance>:<iface> <pseudoInstance>:0/0" NETMAP line, alongside native
// lines, sorted.
func TestBuildBridgedLines(t *testing.T) {
	native := []LinkSpec{{P2P: true, Endpoints: []EndpointSpec{
		{NodeID: 0, Interface: "e0/0", IsIOL: true},
		{NodeID: 1, Interface: "e0/0", IsIOL: true},
	}}}
	// Lab node 0 -> instance 1; bridged interface e0/1 -> pseudo-instance 500.
	bridged := []BridgedEndpoint{{NodeID: 0, Interface: "e0/1", PseudoInstance: 500}}
	got := Build(native, bridged...)
	want := "1:0/0 2:0/0\n1:0/1 500:0/0\n"
	if got != want {
		t.Fatalf("bridged NETMAP:\n got %q\nwant %q", got, want)
	}
}

// TestInstanceIDMapping pins the node.id -> IOL instance id mapping and its
// range validation (IOL refuses 0; valid 1..1024).
func TestInstanceIDMapping(t *testing.T) {
	if InstanceID(0) != 1 || InstanceID(1) != 2 || InstanceID(1023) != 1024 {
		t.Fatalf("InstanceID mapping wrong: %d %d %d", InstanceID(0), InstanceID(1), InstanceID(1023))
	}
	if err := ValidateInstance(0); err != nil {
		t.Fatalf("node 0 must be valid (-> instance 1): %v", err)
	}
	if err := ValidateInstance(1023); err != nil {
		t.Fatalf("node 1023 must be valid (-> instance 1024): %v", err)
	}
	if err := ValidateInstance(1024); err == nil {
		t.Fatal("node 1024 -> instance 1025 must be rejected")
	}
}

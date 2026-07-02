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
	// Interface token is IOL's adapter/port form: e0/0 -> 0/0, s1/2 -> 1/2,
	// s1/3 -> 1/3. This is the format P0 proved works ("1:0/0 2:0/0").
	want := "0:0/0 1:0/0\n1:1/2 2:1/3\n"
	if got != want {
		t.Fatalf("NETMAP mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestBuildMatchesP0Format pins the exact NETMAP line format the P0 manual test
// used to carry traffic between two real IOL 17.18.02 instances.
func TestBuildMatchesP0Format(t *testing.T) {
	links := []LinkSpec{
		{P2P: true, Endpoints: []EndpointSpec{
			{NodeID: 1, Interface: "e0/0", IsIOL: true},
			{NodeID: 2, Interface: "e0/0", IsIOL: true},
		}},
	}
	if got := Build(links); got != "1:0/0 2:0/0\n" {
		t.Fatalf("P0 NETMAP format mismatch: got %q want %q", got, "1:0/0 2:0/0\n")
	}
}

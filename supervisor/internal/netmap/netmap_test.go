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

// TestIfacesForCounts pins the deterministic interface enumeration a node with
// given ethernet/serial adapter group counts exposes at boot: ethernet first
// (adapter-major, port-minor), then serial, 4 ports per group (PortsPerGroup).
func TestIfacesForCounts(t *testing.T) {
	toStrs := func(ifs []Iface) []string {
		out := make([]string, len(ifs))
		for i, f := range ifs {
			out[i] = f.String()
		}
		return out
	}
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	cases := []struct {
		eth, serial int
		want        []string
	}{
		{1, 0, []string{"e0/0", "e0/1", "e0/2", "e0/3"}},
		{2, 1, []string{
			"e0/0", "e0/1", "e0/2", "e0/3",
			"e1/0", "e1/1", "e1/2", "e1/3",
			"s0/0", "s0/1", "s0/2", "s0/3",
		}},
		{0, 0, nil},
		{0, 2, []string{
			"s0/0", "s0/1", "s0/2", "s0/3",
			"s1/0", "s1/1", "s1/2", "s1/3",
		}},
	}
	for _, c := range cases {
		got := toStrs(IfacesForCounts(c.eth, c.serial))
		if !eq(got, c.want) {
			t.Fatalf("IfacesForCounts(%d,%d) = %v, want %v", c.eth, c.serial, got, c.want)
		}
	}

	// Negative counts must behave like zero (no interfaces of that type).
	if got := IfacesForCounts(-1, -1); len(got) != 0 {
		t.Fatalf("negative counts must yield no interfaces, got %v", got)
	}

	// Ports must never exceed PortsPerGroup-1 within a group.
	for _, f := range IfacesForCounts(3, 3) {
		if f.Port < 0 || f.Port >= PortsPerGroup {
			t.Fatalf("iface %v port out of [0,%d) range", f, PortsPerGroup)
		}
	}
}

// TestBuildStatic checks BuildStatic's exact line format and deterministic
// (sorted) ordering for a 2-node example: node 0 (instance 1) with a single
// ethernet interface, node 1 (instance 2) with two ethernet interfaces, each
// wired to its own reserved pseudo-instance.
func TestBuildStatic(t *testing.T) {
	entries := []StaticEntry{
		{InstanceID: 2, Iface: Iface{Type: Ethernet, Adapter: 0, Port: 1}, PseudoInstance: 501},
		{InstanceID: 1, Iface: Iface{Type: Ethernet, Adapter: 0, Port: 0}, PseudoInstance: 500},
		{InstanceID: 2, Iface: Iface{Type: Ethernet, Adapter: 0, Port: 0}, PseudoInstance: 502},
	}
	got := BuildStatic(entries)
	want := "1:0/0 500:0/0\n2:0/0 502:0/0\n2:0/1 501:0/0\n"
	if got != want {
		t.Fatalf("BuildStatic mismatch:\n got %q\nwant %q", got, want)
	}

	// No entries -> empty string.
	if got := BuildStatic(nil); got != "" {
		t.Fatalf("BuildStatic(nil) = %q, want empty", got)
	}
}

// TestBuildStaticFullNode wires every interface of a node with ethGroups=1
// (via IfacesForCounts) to sequential pseudo-instances, pinning the
// combination of both new functions end to end. Ethernet-only here: the
// NETMAP token format is type-less ("<adapter>/<port>", see Entry.String), so
// mixing ethernet and serial groups with overlapping adapter/port numbers
// would produce ambiguous (colliding) line text — an inherent NETMAP format
// property, not a bug in BuildStatic.
func TestBuildStaticFullNode(t *testing.T) {
	ifaces := IfacesForCounts(1, 0)
	pseudoBase := 500
	entries := make([]StaticEntry, len(ifaces))
	for i, f := range ifaces {
		entries[i] = StaticEntry{InstanceID: 1, Iface: f, PseudoInstance: pseudoBase + i}
	}
	got := BuildStatic(entries)
	want := "" +
		"1:0/0 500:0/0\n" +
		"1:0/1 501:0/0\n" +
		"1:0/2 502:0/0\n" +
		"1:0/3 503:0/0\n"
	if got != want {
		t.Fatalf("BuildStatic full-node mismatch:\n got %q\nwant %q", got, want)
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

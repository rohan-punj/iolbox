package painter

import "testing"

const eigrpTopo = `
IP-EIGRP (AS 1): Topology entry for 10.0.99.0/24
  State is Passive, Query origin flag is 1, 1 Successor(s), FD is 3072000
  Routing Descriptor Blocks:
  10.0.12.2 (Ethernet0/0), from 10.0.12.2, Send flag is 0x0
      Composite metric is (3072000/2816000), route is Internal
      Vector metric:
        Minimum bandwidth is 1000 Kbit
  10.0.13.3 (Ethernet0/1), from 10.0.13.3, Send flag is 0x0
      Composite metric is (3584000/2560000), route is Internal
      Vector metric:
        Minimum bandwidth is 1000 Kbit
`

const eigrpErr = `% IP-EIGRP topology entry for 10.0.99.0/24 not found`

func TestParseEIGRPTopology(t *testing.T) {
	r := ParseEIGRPTopology(eigrpTopo)
	if r.Prefix != "10.0.99.0/24" {
		t.Errorf("prefix = %q", r.Prefix)
	}
	if r.FD != 3072000 {
		t.Errorf("FD = %d, want 3072000", r.FD)
	}
	if len(r.Paths) != 2 {
		t.Fatalf("got %d paths, want 2", len(r.Paths))
	}
	// Successor: FD == entry FD (3072000), via 10.0.12.2.
	if !r.Paths[0].Successor {
		t.Errorf("path0 should be successor: %+v", r.Paths[0])
	}
	if r.NextHop != "10.0.12.2" {
		t.Errorf("NextHop = %q, want 10.0.12.2", r.NextHop)
	}
	if r.Paths[0].Interface != "Ethernet0/0" || r.Paths[0].InterfaceNorm != "ethernet0/0" {
		t.Errorf("path0 iface = %q/%q", r.Paths[0].Interface, r.Paths[0].InterfaceNorm)
	}
	if r.Paths[0].FD != 3072000 || r.Paths[0].RD != 2816000 {
		t.Errorf("path0 FD/RD = %d/%d", r.Paths[0].FD, r.Paths[0].RD)
	}
	// Second path: RD 2560000 < entry FD 3072000 -> feasible successor.
	if !r.Paths[1].FeasibleSuccessor {
		t.Errorf("path1 should be feasible successor: %+v", r.Paths[1])
	}
	if r.Paths[1].Successor {
		t.Errorf("path1 should NOT be successor")
	}
}

func TestParseEIGRPTopologyError(t *testing.T) {
	if r := ParseEIGRPTopology(eigrpErr); !r.Empty() {
		t.Errorf("error output not empty: %+v", r)
	}
	if r := ParseEIGRPTopology(""); !r.Empty() {
		t.Errorf("empty output not empty: %+v", r)
	}
}

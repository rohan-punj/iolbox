package painter

import "testing"

const ospfNeighbors = `
Neighbor ID     Pri   State           Dead Time   Address         Interface
2.2.2.2           1   FULL/DR         00:00:35    10.0.12.2       Ethernet0/0
3.3.3.3           1   FULL/BDR        00:00:33    10.0.13.3       Ethernet0/1
4.4.4.4           1   FULL/DROTHER    00:00:31    10.0.14.4       Ethernet0/2
`

const ospfNeighborsErr = `% OSPF instance not configured`

const ospfRoute = `
Codes: L - local, C - connected, S - static, O - OSPF
Gateway of last resort is not set

      10.0.0.0/24 is subnetted, 3 subnets
O        10.0.99.0/24 [110/20] via 10.0.12.2, 00:05:11, Ethernet0/0
O        10.0.88.0/24 [110/30] via 10.0.13.3, 00:05:11, Ethernet0/1
`

func TestParseOSPFNeighbors(t *testing.T) {
	nbrs := ParseOSPFNeighbors(ospfNeighbors)
	if len(nbrs) != 3 {
		t.Fatalf("got %d neighbors, want 3", len(nbrs))
	}
	if nbrs[0].NeighborID != "2.2.2.2" || nbrs[0].State != "FULL" || nbrs[0].Role != "DR" {
		t.Errorf("nbr0 = %+v", nbrs[0])
	}
	if nbrs[0].Interface != "Ethernet0/0" || nbrs[0].InterfaceNorm != "ethernet0/0" {
		t.Errorf("nbr0 iface = %q/%q", nbrs[0].Interface, nbrs[0].InterfaceNorm)
	}
	if nbrs[1].Role != "BDR" || nbrs[2].Role != "DROTHER" {
		t.Errorf("roles = %q %q", nbrs[1].Role, nbrs[2].Role)
	}
	if nbrs[0].Address != "10.0.12.2" {
		t.Errorf("addr = %q", nbrs[0].Address)
	}
}

func TestParseOSPFNeighborsError(t *testing.T) {
	if n := ParseOSPFNeighbors(ospfNeighborsErr); len(n) != 0 {
		t.Errorf("error output produced %d neighbors, want 0", len(n))
	}
	if n := ParseOSPFNeighbors(""); len(n) != 0 {
		t.Errorf("empty output produced %d neighbors, want 0", len(n))
	}
}

func TestParseOSPFRoute(t *testing.T) {
	r := ParseOSPFRoute(ospfRoute, "10.0.99.0/24")
	if r.NextHop != "10.0.12.2" {
		t.Errorf("NextHop = %q, want 10.0.12.2", r.NextHop)
	}
	if r.Cost != 20 {
		t.Errorf("Cost = %d, want 20", r.Cost)
	}
	if r.Interface != "Ethernet0/0" || r.InterfaceNorm != "ethernet0/0" {
		t.Errorf("iface = %q/%q", r.Interface, r.InterfaceNorm)
	}
	if r.Prefix != "10.0.99.0/24" {
		t.Errorf("prefix = %q", r.Prefix)
	}

	// Host-in-network match (dest is a host inside the /24).
	r2 := ParseOSPFRoute(ospfRoute, "10.0.99.5")
	if r2.NextHop != "10.0.12.2" {
		t.Errorf("host dest NextHop = %q, want 10.0.12.2", r2.NextHop)
	}

	// No dest -> first OSPF route.
	r3 := ParseOSPFRoute(ospfRoute, "")
	if r3.NextHop != "10.0.12.2" {
		t.Errorf("no-dest NextHop = %q", r3.NextHop)
	}
}

func TestParseOSPFRouteError(t *testing.T) {
	if r := ParseOSPFRoute("% Network not in table", "10.0.99.0/24"); r.NextHop != "" {
		t.Errorf("error output produced next-hop %q", r.NextHop)
	}
	res := OSPFResult{}
	if !res.Empty() {
		t.Error("zero OSPFResult should be Empty")
	}
}

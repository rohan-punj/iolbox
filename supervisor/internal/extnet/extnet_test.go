package extnet

import (
	"net"
	"testing"
)

// TestSubnetAllocatorDistinct pins that indices come out distinct, ascending
// from 1, and are reusable after Release (so a long-lived supervisor never
// exhausts the /16 across start/stop cycles).
func TestSubnetAllocatorDistinct(t *testing.T) {
	a := NewSubnetAllocator()
	n1, err := a.Next()
	if err != nil || n1 != 1 {
		t.Fatalf("first index = %d, %v; want 1", n1, err)
	}
	n2, err := a.Next()
	if err != nil || n2 != 2 {
		t.Fatalf("second index = %d, %v; want 2", n2, err)
	}
	a.Release(n1)
	n3, err := a.Next()
	if err != nil || n3 != 1 {
		t.Fatalf("after releasing 1, next = %d, %v; want 1 (reused)", n3, err)
	}
}

// TestSubnetAddressing pins the gateway/CIDR/network/pool strings derived from
// an index — these feed both `ip addr add` and the iptables rules, so they must
// be exact.
func TestSubnetAddressing(t *testing.T) {
	s := Subnet{Index: 7}
	if got := s.GatewayIP(); got != "172.31.7.1" {
		t.Fatalf("gateway = %q", got)
	}
	if got := s.CIDR(); got != "172.31.7.1/24" {
		t.Fatalf("cidr = %q", got)
	}
	if got := s.Network(); got != "172.31.7.0/24" {
		t.Fatalf("network = %q", got)
	}
	if s.PoolStart() != "172.31.7.100" || s.PoolEnd() != "172.31.7.199" {
		t.Fatalf("pool = %q..%q", s.PoolStart(), s.PoolEnd())
	}
}

// TestGateFeatures pins the hello-feature strings for each capability combo, the
// contract the GUI keys off. Injected Capabilities keep it OS-independent.
func TestGateFeatures(t *testing.T) {
	cases := []struct {
		caps Capabilities
		want []string
	}{
		{Capabilities{}, nil},
		{Capabilities{NAT: true}, []string{"natgw"}},
		{Capabilities{Mgmt: true}, []string{"mgmt"}},
		{Capabilities{NAT: true, Mgmt: true}, []string{"natgw", "mgmt"}},
	}
	for _, c := range cases {
		got := c.caps.GateFeatures()
		if len(got) != len(c.want) {
			t.Fatalf("caps %+v -> %v, want %v", c.caps, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("caps %+v -> %v, want %v", c.caps, got, c.want)
			}
		}
	}
}

// TestSupports pins per-kind capability gating.
func TestSupports(t *testing.T) {
	caps := Capabilities{NAT: true}
	if !caps.Supports(KindNAT) {
		t.Fatal("NAT cap must support nat kind")
	}
	if caps.Supports(KindMgmt) {
		t.Fatal("without Mgmt cap, mgmt kind must be unsupported")
	}
	if caps.Supports("bogus") {
		t.Fatal("unknown kind must be unsupported")
	}
}

// TestLeaserStickyByMAC confirms a MAC that leases twice gets the SAME address
// (so DISCOVER then REQUEST agree), and distinct MACs get distinct addresses
// starting at the pool base.
func TestLeaserStickyByMAC(t *testing.T) {
	l := newLeaser(net.ParseIP("172.31.1.100"), net.ParseIP("172.31.1.199"))
	macA := net.HardwareAddr{0x02, 0, 0, 0, 0, 1}
	macB := net.HardwareAddr{0x02, 0, 0, 0, 0, 2}
	a1 := l.lease(macA)
	if a1.String() != "172.31.1.100" {
		t.Fatalf("first lease = %s, want 172.31.1.100", a1)
	}
	a2 := l.lease(macA)
	if !a1.Equal(a2) {
		t.Fatalf("same MAC got different leases: %s vs %s", a1, a2)
	}
	b := l.lease(macB)
	if b.String() != "172.31.1.101" {
		t.Fatalf("second MAC lease = %s, want 172.31.1.101", b)
	}
}

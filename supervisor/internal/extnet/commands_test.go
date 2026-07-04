package extnet

import (
	"strings"
	"testing"
)

// joinCmds renders a command list as newline-joined strings for substring
// assertions.
func joinCmds(cmds []cmd) string {
	var b strings.Builder
	for _, c := range cmds {
		b.WriteString(c.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// TestNatBridgeTapCommands pins the at-Start tap creation: create the tap owned
// by us and bring it up, UNBRIDGED and unaddressed (attach happens at link time).
func TestNatBridgeTapCommands(t *testing.T) {
	got := joinCmds(natBridgeTapCmds("iolnat9", "iolbox"))
	for _, want := range []string{
		"ip tuntap add dev iolnat9 mode tap user iolbox",
		"ip link set iolnat9 up",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nat tap setup missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "master") || strings.Contains(got, "addr add") {
		t.Fatalf("tap must be created unbridged/unaddressed:\n%s", got)
	}
}

// TestNatBridgeAttachCommands pins the at-link-draw sequence: attach the tap to
// the link bridge, put the gateway address + NAT on the BRIDGE iface, enable
// forwarding, and install MASQUERADE + the FORWARD pair scoped to the subnet.
func TestNatBridgeAttachCommands(t *testing.T) {
	sub := Subnet{Index: 4}
	got := joinCmds(natBridgeAttachCmds("iolnat9", "iolbr7", sub, "ens160"))
	for _, want := range []string{
		"ip link set iolnat9 master iolbr7",
		"ip addr add 172.31.4.1/24 dev iolbr7",
		"sysctl -w net.ipv4.ip_forward=1",
		"iptables -t nat -A POSTROUTING -s 172.31.4.0/24 -o ens160 -j MASQUERADE",
		"iptables -A FORWARD -i iolbr7 -o ens160 -s 172.31.4.0/24 -j ACCEPT",
		"iptables -A FORWARD -i ens160 -o iolbr7 -d 172.31.4.0/24 -m state --state RELATED,ESTABLISHED -j ACCEPT",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nat bridge attach missing %q in:\n%s", want, got)
		}
	}
}

// TestNatBridgeDetachMirrorsAttach confirms every detach rule is the exact -D
// inverse of an attach -A rule, the gateway address is removed, and the tap is
// detached (nomaster) — the bridge device itself is fabric-owned, not deleted here.
func TestNatBridgeDetachMirrorsAttach(t *testing.T) {
	sub := Subnet{Index: 4}
	got := joinCmds(natBridgeDetachCmds("iolnat9", "iolbr7", sub, "ens160"))
	for _, want := range []string{
		"iptables -t nat -D POSTROUTING -s 172.31.4.0/24 -o ens160 -j MASQUERADE",
		"iptables -D FORWARD -i iolbr7 -o ens160 -s 172.31.4.0/24 -j ACCEPT",
		"iptables -D FORWARD -i ens160 -o iolbr7 -d 172.31.4.0/24 -m state --state RELATED,ESTABLISHED -j ACCEPT",
		"ip addr del 172.31.4.1/24 dev iolbr7",
		"ip link set iolnat9 nomaster",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nat bridge detach missing %q in:\n%s", want, got)
		}
	}
	// ip_forward is deliberately NOT reverted (shared global); the bridge is not
	// deleted here (fabric-owned).
	if strings.Contains(got, "ip_forward") || strings.Contains(got, "link delete iolbr7") {
		t.Fatalf("detach must not revert ip_forward or delete the bridge:\n%s", got)
	}
}

// TestDevName pins the device names (bounded by IFNAMSIZ) per kind.
func TestDevName(t *testing.T) {
	if n, _ := devName(KindNAT, 3); n != "iolnat3" {
		t.Fatalf("nat dev = %q", n)
	}
	if _, err := devName("bogus", 1); err == nil {
		t.Fatal("unknown kind must error")
	}
}

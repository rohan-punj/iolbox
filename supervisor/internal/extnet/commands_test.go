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

// TestNatSetupCommands pins the exact privileged sequence a nat node runs: tap
// create (owned by us), gateway address, up, ip_forward, MASQUERADE + FORWARD
// pair — all scoped to the node's own subnet and the VM default iface.
func TestNatSetupCommands(t *testing.T) {
	sub := Subnet{Index: 4}
	got := joinCmds(natSetupCmds("iolnat9", sub, "ens160", "iolab"))
	for _, want := range []string{
		"ip tuntap add dev iolnat9 mode tap user iolab",
		"ip addr add 172.31.4.1/24 dev iolnat9",
		"ip link set iolnat9 up",
		"sysctl -w net.ipv4.ip_forward=1",
		"iptables -t nat -A POSTROUTING -s 172.31.4.0/24 -o ens160 -j MASQUERADE",
		"iptables -A FORWARD -i iolnat9 -o ens160 -s 172.31.4.0/24 -j ACCEPT",
		"iptables -A FORWARD -i ens160 -o iolnat9 -d 172.31.4.0/24 -m state --state RELATED,ESTABLISHED -j ACCEPT",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nat setup missing %q in:\n%s", want, got)
		}
	}
}

// TestNatTeardownMirrorsSetup confirms every iptables teardown rule is the exact
// -D inverse of a setup -A rule (so teardown removes precisely what setup added),
// and that the tap is deleted.
func TestNatTeardownMirrorsSetup(t *testing.T) {
	sub := Subnet{Index: 4}
	got := joinCmds(natTeardownCmds("iolnat9", sub, "ens160"))
	for _, want := range []string{
		"iptables -t nat -D POSTROUTING -s 172.31.4.0/24 -o ens160 -j MASQUERADE",
		"iptables -D FORWARD -i iolnat9 -o ens160 -s 172.31.4.0/24 -j ACCEPT",
		"iptables -D FORWARD -i ens160 -o iolnat9 -d 172.31.4.0/24 -m state --state RELATED,ESTABLISHED -j ACCEPT",
		"ip tuntap del dev iolnat9 mode tap",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nat teardown missing %q in:\n%s", want, got)
		}
	}
	// ip_forward is deliberately NOT reverted (shared global).
	if strings.Contains(got, "ip_forward") {
		t.Fatalf("teardown must not revert ip_forward:\n%s", got)
	}
}

// TestMgmtCommands pins the macvtap bridge create/up and its delete.
func TestMgmtCommands(t *testing.T) {
	setup := joinCmds(mgmtSetupCmds("iolmgmt5", "ens192"))
	if !strings.Contains(setup, "ip link add link ens192 name iolmgmt5 type macvtap mode bridge") {
		t.Fatalf("mgmt setup wrong:\n%s", setup)
	}
	if !strings.Contains(setup, "ip link set iolmgmt5 up") {
		t.Fatalf("mgmt setup missing up:\n%s", setup)
	}
	teardown := joinCmds(mgmtTeardownCmds("iolmgmt5"))
	if !strings.Contains(teardown, "ip link delete iolmgmt5 type macvtap") {
		t.Fatalf("mgmt teardown wrong:\n%s", teardown)
	}
}

// TestDevName pins the device names (bounded by IFNAMSIZ) per kind.
func TestDevName(t *testing.T) {
	if n, _ := devName(KindNAT, 3); n != "iolnat3" {
		t.Fatalf("nat dev = %q", n)
	}
	if n, _ := devName(KindMgmt, 3); n != "iolmgmt3" {
		t.Fatalf("mgmt dev = %q", n)
	}
	if _, err := devName("bogus", 1); err == nil {
		t.Fatal("unknown kind must error")
	}
}

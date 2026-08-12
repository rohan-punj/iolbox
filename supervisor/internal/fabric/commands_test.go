package fabric

import (
	"strings"
	"testing"
)

// joinArgv renders a [][]string as newline-joined space-joined strings for
// substring assertions, mirroring extnet's joinCmds test helper.
func joinArgv(cmds [][]string) string {
	var b strings.Builder
	for _, c := range cmds {
		b.WriteString(strings.Join(c, " "))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestTapCreateCmds(t *testing.T) {
	got := joinArgv(tapCreateCmds("iol3_17", 1000))
	for _, want := range []string{
		"ip tuntap add dev iol3_17 mode tap user 1000",
		// disable_ipv6 must run BEFORE the device comes up, so the kernel never
		// gets a chance to start emitting ND/MLD background traffic on it (see
		// tapCreateCmds's doc for why that traffic otherwise leaks into IOL as
		// phantom learned MACs, floodable to every other port on the switch).
		"sysctl -w net.ipv6.conf.iol3_17.disable_ipv6=1",
		"ip link set iol3_17 up",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("tapCreateCmds missing %q in:\n%s", want, got)
		}
	}
	sysctlIdx := strings.Index(got, "sysctl")
	upIdx := strings.Index(got, "ip link set iol3_17 up")
	if sysctlIdx < 0 || upIdx < 0 || sysctlIdx > upIdx {
		t.Fatalf("disable_ipv6 must be set before the device is brought up:\n%s", got)
	}
}

func TestTapDeleteCmds(t *testing.T) {
	got := joinArgv(tapDeleteCmds("iol3_17"))
	if !strings.Contains(got, "ip link del iol3_17") {
		t.Fatalf("tapDeleteCmds wrong:\n%s", got)
	}
}

func TestBridgeCreateCmds(t *testing.T) {
	got := joinArgv(bridgeCreateCmds("iolbr12"))
	for _, want := range []string{
		"ip link add iolbr12 type bridge group_fwd_mask 0xfff8",
		"sysctl -w net.ipv6.conf.iolbr12.disable_ipv6=1",
		"ip link set iolbr12 up",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("bridgeCreateCmds missing %q in:\n%s", want, got)
		}
	}
}

func TestBridgeDeleteCmds(t *testing.T) {
	got := joinArgv(bridgeDeleteCmds("iolbr12"))
	if !strings.Contains(got, "ip link del iolbr12") {
		t.Fatalf("bridgeDeleteCmds wrong:\n%s", got)
	}
}

func TestAttachCmds(t *testing.T) {
	got := joinArgv(attachCmds("iolbr12", "iol3_17"))
	if !strings.Contains(got, "ip link set iol3_17 master iolbr12") {
		t.Fatalf("attachCmds wrong:\n%s", got)
	}
}

func TestDetachCmds(t *testing.T) {
	got := joinArgv(detachCmds("iol3_17"))
	if !strings.Contains(got, "ip link set iol3_17 nomaster") {
		t.Fatalf("detachCmds wrong:\n%s", got)
	}
}

func TestNetemCmds(t *testing.T) {
	tests := []struct {
		name string
		in   Netem
		want []string
	}{
		{name: "delay", in: Netem{DelayMs: 100}, want: []string{"tc", "qdisc", "replace", "dev", "tap0", "root", "netem", "delay", "100ms"}},
		{name: "delay jitter", in: Netem{DelayMs: 100, JitterMs: 2.5}, want: []string{"tc", "qdisc", "replace", "dev", "tap0", "root", "netem", "delay", "100ms", "2.5ms"}},
		{name: "loss", in: Netem{LossPct: 1.25}, want: []string{"tc", "qdisc", "replace", "dev", "tap0", "root", "netem", "loss", "1.25%"}},
		{name: "rate", in: Netem{RateKbit: 1000}, want: []string{"tc", "qdisc", "replace", "dev", "tap0", "root", "netem", "rate", "1000kbit"}},
		{name: "all", in: Netem{DelayMs: 50, JitterMs: 5, LossPct: 1, DuplicatePct: 2, ReorderPct: 3, RateKbit: 1000}, want: []string{"tc", "qdisc", "replace", "dev", "tap0", "root", "netem", "delay", "50ms", "5ms", "loss", "1%", "duplicate", "2%", "reorder", "3%", "50%", "rate", "1000kbit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := netemCmds("tap0", tt.in)
			if len(got) != 1 {
				t.Fatalf("got %d commands, want one: %#v", len(got), got)
			}
			if len(got[0]) != len(tt.want) {
				t.Fatalf("argv length = %d, want %d: %#v", len(got[0]), len(tt.want), got[0])
			}
			for i := range tt.want {
				if got[0][i] != tt.want[i] {
					t.Fatalf("argv[%d] = %q, want %q; full argv %#v", i, got[0][i], tt.want[i], got[0])
				}
			}
		})
	}
}

func TestNetemClearCmds(t *testing.T) {
	want := []string{"tc", "qdisc", "del", "dev", "tap0", "root"}
	got := netemClearCmds("tap0")
	if len(got) != 1 || strings.Join(got[0], " ") != strings.Join(want, " ") {
		t.Fatalf("netemClearCmds = %#v, want %#v", got, [][]string{want})
	}
}

// TestTapName pins the format and the IFNAMSIZ length guard at its boundary.
// Prefix "iol" + "_" = 4 fixed chars, leaving 11 bytes for the two decimal
// numbers combined (15 usable bytes total, IFNAMSIZ=16 including the NUL).
func TestTapName(t *testing.T) {
	name, err := TapName(3, 17)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "iol3_17" {
		t.Fatalf("TapName = %q, want iol3_17", name)
	}

	// Exactly 15 bytes: "iol" + "12345" + "_" + "12345" = 3+5+1+5 = 14... find
	// exact boundary by construction instead of guessing digit counts.
	// 11 digit-bytes available for instanceID+portFlatIndex combined.
	okName, err := TapName(1234567, 890) // 7 + 3 = 10 digit bytes -> len 14
	if err != nil {
		t.Fatalf("unexpected error at 14 bytes: %v (name %q)", err, okName)
	}
	if len(okName) != 14 {
		t.Fatalf("expected 14-byte name, got %q (%d bytes)", okName, len(okName))
	}

	boundaryName, err := TapName(12345678, 890) // 8 + 3 = 11 digit bytes -> len 15 (exactly at limit)
	if err != nil {
		t.Fatalf("unexpected error at exactly 15 bytes: %v (name %q)", err, boundaryName)
	}
	if len(boundaryName) != 15 {
		t.Fatalf("expected 15-byte name, got %q (%d bytes)", boundaryName, len(boundaryName))
	}

	// One byte over the limit must error.
	_, err = TapName(123456789, 890) // 9 + 3 = 12 digit bytes -> len 16 (over)
	if err == nil {
		t.Fatal("expected error for 16-byte tap name, got nil")
	}
}

// TestSudoArgv pins the euid-branch: root execs the argv directly (no sudo
// fork+exec overhead), any non-root euid keeps the `sudo -n` wrapper the
// builder's NOPASSWD-sudo smoke user relies on.
func TestSudoArgv(t *testing.T) {
	argv := []string{"ip", "link", "set", "iol3_17", "master", "iolbr12"}

	name, args := sudoArgv(0, argv)
	if name != "ip" {
		t.Fatalf("root: name = %q, want %q", name, "ip")
	}
	if strings.Join(args, " ") != "link set iol3_17 master iolbr12" {
		t.Fatalf("root: args = %q", args)
	}

	for _, euid := range []int{1, 1000, 65534} {
		name, args = sudoArgv(euid, argv)
		if name != "sudo" {
			t.Fatalf("euid %d: name = %q, want %q", euid, name, "sudo")
		}
		want := "-n ip link set iol3_17 master iolbr12"
		if strings.Join(args, " ") != want {
			t.Fatalf("euid %d: args = %q, want %q", euid, strings.Join(args, " "), want)
		}
	}
}

// TestBridgeName pins the format and the IFNAMSIZ length guard at its
// boundary. Prefix "iolbr" = 5 fixed chars, leaving 10 bytes for the decimal
// linkID.
func TestBridgeName(t *testing.T) {
	name, err := BridgeName(12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "iolbr12" {
		t.Fatalf("BridgeName = %q, want iolbr12", name)
	}

	// Exactly 15 bytes: "iolbr" (5) + 10-digit number = 15.
	boundaryName, err := BridgeName(1234567890)
	if err != nil {
		t.Fatalf("unexpected error at exactly 15 bytes: %v (name %q)", err, boundaryName)
	}
	if len(boundaryName) != 15 {
		t.Fatalf("expected 15-byte name, got %q (%d bytes)", boundaryName, len(boundaryName))
	}

	// One byte over (11-digit number) must error.
	_, err = BridgeName(12345678901)
	if err == nil {
		t.Fatal("expected error for 16-byte bridge name, got nil")
	}
}

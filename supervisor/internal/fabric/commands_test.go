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
		"ip link set iol3_17 up",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("tapCreateCmds missing %q in:\n%s", want, got)
		}
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
		"ip link add iolbr12 type bridge",
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

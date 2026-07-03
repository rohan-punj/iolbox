package main

import (
	"net"
	"strings"
	"testing"
)

func TestProbeFreePorts(t *testing.T) {
	// Occupy a port, then probe it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	busyPort := l.Addr().(*net.TCPAddr).Port

	free, busy := probeFreePorts([]int{busyPort})
	if len(busy) != 1 || busy[0] != busyPort {
		t.Errorf("port %d should be busy: free=%v busy=%v", busyPort, free, busy)
	}

	// Close it — now it should probe free.
	l.Close()
	free, busy = probeFreePorts([]int{busyPort})
	if len(free) != 1 || len(busy) != 0 {
		t.Errorf("port %d should be free after close: free=%v busy=%v", busyPort, free, busy)
	}
}

func TestForwardedPorts(t *testing.T) {
	p := defaultPortRanges()
	got := p.forwardedPorts()
	// 1 GUI + 50 consoles + 30 capture = 81
	if len(got) != 1+50+30 {
		t.Fatalf("want 81 ports, got %d", len(got))
	}
	if got[0] != 4001 {
		t.Errorf("first port should be GUI 4001, got %d", got[0])
	}
	// consoles 9000..9049
	if got[1] != 9000 || got[50] != 9049 {
		t.Errorf("console range wrong: [1]=%d [50]=%d", got[1], got[50])
	}
	// capture 5500..5529
	if got[51] != 5500 || got[80] != 5529 {
		t.Errorf("capture range wrong: [51]=%d [80]=%d", got[51], got[80])
	}
}

func TestQemuNetdevArg(t *testing.T) {
	p := defaultPortRanges()
	arg := p.qemuNetdevArg()
	if !strings.HasPrefix(arg, "user,id=net0") {
		t.Fatalf("netdev must start with user,id=net0: %q", arg)
	}
	// GUI hostfwd present and bound to 127.0.0.1 (never 0.0.0.0).
	if !strings.Contains(arg, "hostfwd=tcp:127.0.0.1:4001-:4001") {
		t.Errorf("missing GUI hostfwd: %q", arg)
	}
	if strings.Contains(arg, "0.0.0.0") {
		t.Errorf("hostfwd must not bind 0.0.0.0 (no-auth GUI): %q", arg)
	}
	// A console and a capture port present.
	if !strings.Contains(arg, "hostfwd=tcp:127.0.0.1:9000-:9000") {
		t.Errorf("missing console 9000 hostfwd")
	}
	if !strings.Contains(arg, "hostfwd=tcp:127.0.0.1:5500-:5500") {
		t.Errorf("missing capture 5500 hostfwd")
	}
	// hostfwd count == forwarded port count.
	if n := strings.Count(arg, "hostfwd="); n != 81 {
		t.Errorf("want 81 hostfwd entries, got %d", n)
	}
}

func TestParsePortsOverride(t *testing.T) {
	base := defaultPortRanges()

	// empty keeps defaults
	got, err := parsePortsOverride("", base)
	if err != nil || got != base {
		t.Fatalf("empty override should keep base: %v %+v", err, got)
	}

	// change GUI only
	got, err = parsePortsOverride("4100", base)
	if err != nil {
		t.Fatal(err)
	}
	if got.guiPort != 4100 || got.consoleStart != base.consoleStart {
		t.Errorf("gui-only override wrong: %+v", got)
	}

	// full override
	got, err = parsePortsOverride("4100:9100:10:5600:5", base)
	if err != nil {
		t.Fatal(err)
	}
	if got.guiPort != 4100 || got.consoleStart != 9100 || got.consoleCount != 10 ||
		got.captureStart != 5600 || got.captureCount != 5 {
		t.Errorf("full override wrong: %+v", got)
	}

	// empty middle field keeps default
	got, err = parsePortsOverride("4100::20", base)
	if err != nil {
		t.Fatal(err)
	}
	if got.guiPort != 4100 || got.consoleStart != base.consoleStart || got.consoleCount != 20 {
		t.Errorf("sparse override wrong: %+v", got)
	}

	// errors
	if _, err := parsePortsOverride("nope", base); err == nil {
		t.Error("expected error on non-numeric")
	}
	if _, err := parsePortsOverride("1:2:3:4:5:6", base); err == nil {
		t.Error("expected error on too many fields")
	}
	if _, err := parsePortsOverride("70000", base); err == nil {
		t.Error("expected error on out-of-range port")
	}
}

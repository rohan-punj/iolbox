package node

import "testing"

func TestStateMachineTransitions(t *testing.T) {
	if !CanTransition(StateStopped, StateStarting) {
		t.Fatal("stopped->starting must be legal")
	}
	if !CanTransition(StateStarting, StateRunning) {
		t.Fatal("starting->running must be legal")
	}
	if !CanTransition(StateRunning, StateCrashed) {
		t.Fatal("running->crashed must be legal")
	}
	if !CanTransition(StateCrashed, StateStarting) {
		t.Fatal("crashed->starting must be legal")
	}
	if CanTransition(StateStopped, StateRunning) {
		t.Fatal("stopped->running must be illegal")
	}
	if CanTransition(StateRunning, StateStarting) {
		t.Fatal("running->starting must be illegal")
	}
}

func TestMachineCallback(t *testing.T) {
	var seen []State
	m := NewMachine(func(s State) { seen = append(seen, s) })
	if m.State() != StateStopped {
		t.Fatal("initial state must be stopped")
	}
	if !m.To(StateStarting) || !m.To(StateRunning) {
		t.Fatal("legal transitions failed")
	}
	if m.To(StateStarting) {
		t.Fatal("illegal running->starting accepted")
	}
	if len(seen) != 2 || seen[0] != StateStarting || seen[1] != StateRunning {
		t.Fatalf("callbacks: %v", seen)
	}
}

func TestPortAllocator(t *testing.T) {
	pa := NewPortAllocator(9000, 3)
	a, _ := pa.Next()
	b, _ := pa.Next()
	c, _ := pa.Next()
	if a != 9000 || b != 9001 || c != 9002 {
		t.Fatalf("ports %d %d %d", a, b, c)
	}
	if _, err := pa.Next(); err == nil {
		t.Fatal("expected exhaustion")
	}
	pa.Release(9001)
	d, err := pa.Next()
	if err != nil || d != 9001 {
		t.Fatalf("reuse released: %d %v", d, err)
	}
}

func TestPortAllocatorReserve(t *testing.T) {
	pa := NewPortAllocator(4000, 10)
	if err := pa.Reserve(4005); err != nil {
		t.Fatal(err)
	}
	if err := pa.Reserve(4005); err == nil {
		t.Fatal("double reserve should fail")
	}
}

func TestIOLArgvStripsKeepalive(t *testing.T) {
	s := Spec{NodeID: 3, Kind: "iol", ImagePath: "/img/l3.bin", Ethernet: 2, Serial: 1}
	argv := s.IOLArgv()
	for _, a := range argv {
		if a == "-l" {
			t.Fatal("argv must NOT contain -l keepalive flag")
		}
	}
	if argv[0] != "/img/l3.bin" {
		t.Fatalf("image path first: %v", argv)
	}
	// Node id 3 maps to IOL instance id 4 (nodeID+1); the positional is the
	// instance id, not the raw node id (IOL rejects 0).
	if argv[len(argv)-1] != "4" {
		t.Fatalf("instance id (nodeID+1) must be last positional: %v", argv)
	}
	// -e 2 and -s 1 present
	joined := ""
	for _, a := range argv {
		joined += a + " "
	}
	if !contains(joined, "-e 2 ") || !contains(joined, "-s 1 ") {
		t.Fatalf("adapter flags missing: %s", joined)
	}
}

func TestEnvironHasNoConsolePort(t *testing.T) {
	// P0: the console is a pty bridged by the supervisor, NOT an IOL-opened TCP
	// port. Environ must not invent a console port env var.
	s := Spec{NodeID: 3, Kind: "iol", WorkDir: "/run/iolab/lab1", ConsolePort: 9003}
	for _, e := range s.Environ() {
		if len(e) >= 8 && e[:8] == "IOL_CONS" {
			t.Fatalf("Environ must not set a console port env: %q", e)
		}
	}
	// IOURC must still point into the (shared) work dir.
	found := false
	for _, e := range s.Environ() {
		if e == "IOURC=/run/iolab/lab1/iourc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("IOURC env missing: %v", s.Environ())
	}
}

func TestNVRAMKiBFor(t *testing.T) {
	if got := NVRAMKiBFor(0); got != DefaultNVRAMKiB {
		t.Fatalf("empty config: got %d want %d", got, DefaultNVRAMKiB)
	}
	if got := NVRAMKiBFor(10); got < DefaultNVRAMKiB {
		t.Fatalf("small config must not shrink below default: %d", got)
	}
	// A config larger than the default headroom grows the size.
	big := 200 * 1024
	if got := NVRAMKiBFor(big); got <= DefaultNVRAMKiB {
		t.Fatalf("large config must grow -n: got %d", got)
	}
	// The size must be able to hold the config bytes.
	if NVRAMKiBFor(big)*1024 < big {
		t.Fatalf("nvram size %d KiB cannot hold %d bytes", NVRAMKiBFor(big), big)
	}
}

func TestIOLArgvNVRAMSize(t *testing.T) {
	s := Spec{NodeID: 1, Kind: "iol", ImagePath: "/i", NVRAMKiB: 512}
	argv := s.IOLArgv()
	joined := ""
	for _, a := range argv {
		joined += a + " "
	}
	if !contains(joined, "-n 512 ") {
		t.Fatalf("expected -n 512, got %s", joined)
	}
}

func TestVPCSArgv(t *testing.T) {
	s := Spec{NodeID: 1, Kind: "vpcs", ConsolePort: 9000, VPCSCount: 1}
	argv, err := s.VPCSArgv("pc1")
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "vpcs" {
		t.Fatalf("argv0=%s", argv[0])
	}
	joined := ""
	for _, a := range argv {
		joined += a + " "
	}
	// vpcs 0.8.3 is its own telnet console server: it MUST get -p <ConsolePort>
	// and -i <count>. It has NO name flag, so -N must never appear (vpcs rejects
	// it and exits).
	if !contains(joined, "-p 9000 ") {
		t.Fatalf("VPCS argv must contain -p <ConsolePort>: %v", argv)
	}
	if !contains(joined, "-i 1 ") {
		t.Fatalf("VPCS argv must contain -i <count>: %v", argv)
	}
	for _, a := range argv {
		if a == "-N" {
			t.Fatalf("VPCS argv must NOT contain -N (vpcs 0.8.3 has no name flag): %v", argv)
		}
	}
	s.VPCSCount = 20
	if _, err := s.VPCSArgv("x"); err == nil {
		t.Fatal("count>9 should error")
	}
	// A console port is required (vpcs serves telnet on it).
	if _, err := (Spec{Kind: "vpcs", VPCSCount: 1}).VPCSArgv("x"); err == nil {
		t.Fatal("missing console port should error")
	}
}

// TestVPCSArgvUDPTunnel checks the UDP tunnel flags: -s binds VPCSUDPLocal (the
// relay's delivery port), -c targets VPCSUDPRemote (the relay's receiving port),
// with -t 127.0.0.1. Absent ports => no tunnel flags. -p/-i are always present.
func TestVPCSArgvUDPTunnel(t *testing.T) {
	s := Spec{NodeID: 2, Kind: "vpcs", ConsolePort: 9001, VPCSCount: 1,
		VPCSUDPLocal: 10005, VPCSUDPRemote: 10004}
	argv, err := s.VPCSArgv("pc2")
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, a := range argv {
		joined += a + " "
	}
	if !contains(joined, "-s 10005 ") || !contains(joined, "-c 10004 ") || !contains(joined, "-t 127.0.0.1 ") {
		t.Fatalf("vpcs UDP tunnel flags missing/wrong: %s", joined)
	}
	// Console + count flags present alongside the tunnel; no name flag.
	if !contains(joined, "-p 9001 ") || !contains(joined, "-i 1 ") {
		t.Fatalf("vpcs console/count flags missing: %s", joined)
	}
	for _, a := range argv {
		if a == "-N" {
			t.Fatalf("VPCS argv must NOT contain -N: %v", argv)
		}
	}

	// No tunnel wired => no -s/-c/-t (but -p/-i remain).
	s.VPCSUDPLocal, s.VPCSUDPRemote = 0, 0
	argv, _ = s.VPCSArgv("pc2")
	for _, a := range argv {
		if a == "-s" || a == "-c" || a == "-t" {
			t.Fatalf("unconnected VPCS must have no UDP tunnel flags: %v", argv)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

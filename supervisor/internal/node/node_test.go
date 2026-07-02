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
	if argv[len(argv)-1] != "3" {
		t.Fatalf("instance id must be last positional: %v", argv)
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

func TestVPCSArgv(t *testing.T) {
	s := Spec{NodeID: 1, Kind: "vpcs", ConsolePort: 9000, VPCSCount: 1}
	argv, err := s.VPCSArgv("pc1")
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "vpcs" {
		t.Fatalf("argv0=%s", argv[0])
	}
	s.VPCSCount = 20
	if _, err := s.VPCSArgv("x"); err == nil {
		t.Fatal("count>9 should error")
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

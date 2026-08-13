//go:build linux

package node

import (
	"os/exec"
	"testing"
	"time"
)

// Item 5 regression: Stop must not return before the killed child has actually
// been reaped. Previously Stop fired SIGKILL and returned immediately, so
// node.restart's startNodes could spawn the replacement while the old process
// still held its NETIO socket dir and console port.
func TestStopWaitsForChildExit(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn helper process: %v", err)
	}
	m := NewMachine(nil)
	m.To(StateStarting)
	m.To(StateRunning)
	p := &Process{
		Machine:  m,
		cmd:      cmd,
		done:     make(chan struct{}),
		waitDone: make(chan struct{}),
	}
	go p.wait()

	start := time.Now()
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= stopWaitTimeout {
		t.Fatalf("Stop took %v; it timed out instead of observing the exit", elapsed)
	}
	select {
	case <-p.waitDone:
	default:
		t.Fatal("Stop returned before the child was reaped")
	}
	// The reaper owns cmd.Wait, so ProcessState is populated exactly once the
	// wait has completed — this is the happens-before edge restart relies on.
	if cmd.ProcessState == nil {
		t.Fatal("child was not reaped by the time Stop returned")
	}
}

// A Process with no child (console-bridge / test constructions) must not block
// in Stop's wait at all.
func TestStopDoesNotWaitWithoutChild(t *testing.T) {
	p := &Process{done: make(chan struct{})}
	start := time.Now()
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Stop on a childless Process blocked for %v", elapsed)
	}
}

// awaitTermination on the VPCS path must not consult waitDone at all: the
// launcher was reaped at spawn time, so waitDone is already closed and proves
// nothing. With no console port there is no residue to observe and it returns
// immediately; with a port nothing holds, likewise.
func TestAwaitTerminationVPCSIgnoresLauncherReap(t *testing.T) {
	p := &Process{waitDone: make(chan struct{})}
	close(p.waitDone) // as the daemonizing launcher's reaper does, immediately
	if !p.awaitTermination(4242, 0, 200*time.Millisecond) {
		t.Fatal("awaitTermination reported residue for an unset console port")
	}
}

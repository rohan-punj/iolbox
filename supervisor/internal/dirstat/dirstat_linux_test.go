//go:build linux

package dirstat

import (
	"syscall"
	"testing"
	"time"
)

// bindLoopback opens the real thing: a raw AF_PACKET socket bound to loopback,
// exactly as bindTap does for a tap. Skips when the process lacks CAP_NET_RAW
// (the dev box) — on the appliance and the builder the supervisor/test runs as
// root, which is where this test is meant to run.
func bindLoopback(t *testing.T) int {
	t.Helper()
	fd, err := bindTap("lo")
	if err != nil {
		if err == syscall.EPERM || err == syscall.EACCES {
			t.Skipf("no CAP_NET_RAW: %v", err)
		}
		t.Skipf("cannot bind a raw socket to lo: %v", err)
	}
	return fd
}

// TestBindTapInstallsReceiveTimeout pins the mechanism the teardown relies on,
// empirically: recvfrom on a bound socket must come back with EAGAIN instead of
// parking forever. Without SO_RCVTIMEO the read loop blocks indefinitely and
// nothing short of process death gets it out. (syscall has no
// GetsockoptTimeval, and this repo vendors no x/sys, so the option is asserted
// by its observable behavior rather than by reading it back.)
func TestBindTapInstallsReceiveTimeout(t *testing.T) {
	fd := bindLoopback(t)
	defer syscall.Close(fd)

	buf := make([]byte, snapLen)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("never observed an EAGAIN: loopback is too busy to prove the timeout")
		}
		start := time.Now()
		_, _, err := syscall.Recvfrom(fd, buf, 0)
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.EAGAIN {
			// The wake must be attributable to SO_RCVTIMEO, not to a hang: an
			// unset timeout would never return here at all.
			if elapsed := time.Since(start); elapsed > 4*recvTimeout {
				t.Fatalf("recvfrom returned EAGAIN after %s, want about %s", elapsed, recvTimeout)
			}
			return
		}
		if err != nil {
			t.Fatalf("recvfrom: %v", err)
		}
		// A real frame arrived on lo; try again for a quiet window.
	}
}

// TestBlockedRecvfromUnblocksOnStop is the finding-#11 regression proper: a
// readLoop actually parked in recvfrom on a real bound raw socket must return
// when the stop channel closes. Pre-fix (blocking socket, teardown by
// syscall.Close alone) the equivalent goroutine never returned, which is what
// wedged Classifier.Close and with it the whole serialized control plane.
func TestBlockedRecvfromUnblocksOnStop(t *testing.T) {
	fd := bindLoopback(t)
	defer syscall.Close(fd)

	c := newClassifier([]EndpointDev{{Index: 0, Dev: "lo"}})
	stop := make(chan struct{})
	done := make(chan struct{})
	c.wg.Add(1)
	go func() {
		defer close(done)
		defer c.wg.Done()
		c.readLoop(0, fd, stop)
	}()

	// Let the loop reach recvfrom and block there.
	time.Sleep(2 * recvTimeout)
	close(stop)

	select {
	case <-done:
	case <-time.After(5 * recvTimeout):
		t.Fatalf("readLoop still parked in recvfrom %s after stop", 5*recvTimeout)
	}
}

// TestOpenCloseDrainsRealSockets exercises the full Open/Close pair against a
// real bound socket and asserts Close returns well inside its own safety
// timeout — i.e. it drained rather than fell through the bounded-wait escape.
func TestOpenCloseDrainsRealSockets(t *testing.T) {
	if fd, err := bindTap("lo"); err != nil {
		t.Skipf("cannot bind a raw socket to lo: %v", err)
	} else {
		syscall.Close(fd)
	}

	c, err := Open([]EndpointDev{{Index: 0, Dev: "lo"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if c == nil {
		t.Fatal("Open returned a nil Classifier for a bindable device")
	}

	time.Sleep(2 * recvTimeout) // ensure the loop is inside recvfrom
	start := time.Now()
	if !c.closeDrain(closeDrainTimeout) {
		t.Fatalf("Close did not drain the read loops within %s", closeDrainTimeout)
	}
	if elapsed := time.Since(start); elapsed > 2*recvTimeout+time.Second {
		t.Fatalf("Close took %s; expected roughly one receive-timeout tick", elapsed)
	}
	c.Close() // idempotent
}

//go:build linux

package slowtee

import (
	"syscall"
	"testing"
	"time"
)

// TestBindTapInstallsReceiveTimeout pins the SO_RCVTIMEO the forward loops'
// stoppability depends on, empirically (syscall exposes no GetsockoptTimeval):
// recvfrom must return EAGAIN promptly instead of parking forever. Skips
// without CAP_NET_RAW; on the appliance and the builder these run as root.
func TestBindTapInstallsReceiveTimeout(t *testing.T) {
	fd, err := bindTap("lo")
	if err != nil {
		t.Skipf("cannot bind a raw socket to lo: %v", err)
	}
	defer syscall.Close(fd)

	buf := make([]byte, frameLen)
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
			if elapsed := time.Since(start); elapsed > 4*recvTimeout {
				t.Fatalf("recvfrom returned EAGAIN after %s, want about %s", elapsed, recvTimeout)
			}
			return
		}
		if err != nil {
			t.Fatalf("recvfrom: %v", err)
		}
	}
}

// TestOpenCloseDrainsRealSockets: two real bound raw sockets, both forward loops
// parked in recvfrom, Close must drain them rather than block forever.
func TestOpenCloseDrainsRealSockets(t *testing.T) {
	fd, err := bindTap("lo")
	if err != nil {
		t.Skipf("cannot bind a raw socket to lo: %v", err)
	}
	syscall.Close(fd)

	tee, err := open([]string{"lo", "lo"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if tee == nil {
		t.Fatal("open returned a nil Tee for bindable devices")
	}

	time.Sleep(2 * recvTimeout) // ensure both loops are inside recvfrom
	start := time.Now()
	if !tee.closeDrain(closeDrainTimeout) {
		t.Fatalf("Close did not drain the forward loops within %s", closeDrainTimeout)
	}
	if elapsed := time.Since(start); elapsed > 2*recvTimeout+time.Second {
		t.Fatalf("Close took %s; expected roughly one receive-timeout tick", elapsed)
	}
	tee.Close() // idempotent
}

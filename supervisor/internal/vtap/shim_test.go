package vtap

import (
	"net"
	"os"
	"testing"
	"time"
)

// pipeTap adapts an *os.File pipe end to the tapDev interface used by the
// pumps, so tests exercise the exact same pump code as the linux build
// without a real /dev/net/tun device.
type pipeTap struct {
	*os.File
}

// newLoopbackUDP returns two connected loopback UDP sockets: a listens on an
// ephemeral port, b is "connected" in the sense that its WriteToUDP target is
// a's address and vice versa. Used to stand in for the real VPCS<->shim UDP
// leg without needing VPCS itself.
func newLoopbackUDP(t *testing.T) (a, b *net.UDPConn) {
	t.Helper()
	a, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen a: %v", err)
	}
	b, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		_ = a.Close()
		t.Fatalf("listen b: %v", err)
	}
	return a, b
}

// TestPumpUDPToTap confirms a datagram sent to the shim's bind socket (as
// VPCS's `-c` target) is written verbatim to the tap side, with no header
// added or stripped.
func TestPumpUDPToTap(t *testing.T) {
	shimConn, vpcsConn := newLoopbackUDP(t)
	defer shimConn.Close()
	defer vpcsConn.Close()
	sendTo := shimConn.LocalAddr().(*net.UDPAddr)

	tapR, tapW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer tapR.Close()
	defer tapW.Close()
	tap := pipeTap{tapW}

	closed := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpUDPToTap(closed, shimConn, tap)
	}()

	frame := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02}
	if _, err := vpcsConn.WriteToUDP(frame, sendTo); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	buf := make([]byte, 1500)
	_ = tapR.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := tapR.Read(buf)
	if err != nil {
		t.Fatalf("read tap: %v", err)
	}
	if string(buf[:n]) != string(frame) {
		t.Fatalf("tap got %x, want %x (no header mangling expected)", buf[:n], frame)
	}

	close(closed)
	_ = shimConn.SetReadDeadline(time.Now().Add(-time.Second))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pumpUDPToTap did not exit after close")
	}
}

// TestPumpTapToUDP confirms a frame written to the tap side is forwarded
// verbatim as a datagram to sendTo (VPCS's `-s` listen port).
func TestPumpTapToUDP(t *testing.T) {
	shimConn, vpcsConn := newLoopbackUDP(t)
	defer shimConn.Close()
	defer vpcsConn.Close()
	sendTo := vpcsConn.LocalAddr().(*net.UDPAddr)

	tapR, tapW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer tapR.Close()
	tap := pipeTap{tapR}

	closed := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpTapToUDP(closed, tap, shimConn, sendTo)
	}()

	frame := []byte{0xCA, 0xFE, 0x00, 0x11, 0x22, 0x33}
	if _, err := tapW.Write(frame); err != nil {
		t.Fatalf("write tap: %v", err)
	}

	buf := make([]byte, 1500)
	_ = vpcsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := vpcsConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read udp: %v", err)
	}
	if string(buf[:n]) != string(frame) {
		t.Fatalf("udp got %x, want %x (no header mangling expected)", buf[:n], frame)
	}

	// Unblock the pump's tap Read the same way Shim.Close does: close the
	// write end so the pipe read returns EOF, then close(closed) is
	// belt-and-suspenders for the write-side stop check.
	close(closed)
	_ = tapW.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pumpTapToUDP did not exit after tap close")
	}
}

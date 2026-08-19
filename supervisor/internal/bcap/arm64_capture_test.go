//go:build linux

package bcap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const (
	capturePacketAll = 0x0300 // htons(ETH_P_ALL) on little-endian Linux
	tunSetIFF        = 0x400454ca
	tunIFFTap        = 0x0002
	tunIFFNoPI       = 0x1000
)

func hasCaptureCapability(t *testing.T, bit uint) bool {
	t.Helper()
	if os.Geteuid() == 0 {
		return true
	}
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "CapEff:" {
			continue
		}
		eff, err := strconv.ParseUint(fields[1], 16, 64)
		return err == nil && eff&(uint64(1)<<bit) != 0
	}
	return false
}

func runCaptureIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func TestArm64CaptureEndToEnd(t *testing.T) {
	if !hasCaptureCapability(t, 12) || !hasCaptureCapability(t, 13) {
		t.Skip("requires root or CAP_NET_ADMIN and CAP_NET_RAW")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skipf("/dev/net/tun unavailable: %v", err)
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skipf("ip command unavailable: %v", err)
	}
	if _, err := exec.LookPath("tcpdump"); err != nil {
		t.Skipf("tcpdump unavailable: %v", err)
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		t.Skipf("sudo unavailable (bcap uses sudo -n tcpdump): %v", err)
	}
	if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
		t.Skipf("passwordless sudo unavailable for tcpdump: %v", err)
	}
	validator, validatorArgs := captureValidator(t)
	if validator == "" {
		t.Skip("requires tshark or capinfos as a pcapng validator")
	}

	name := fmt.Sprintf("m7cap-%d", os.Getpid()%100000)
	if len(name) >= 16 {
		name = "m7cap0"
	}
	if err := runCaptureIP("tuntap", "add", "dev", name, "mode", "tap"); err != nil {
		t.Skipf("cannot create capture tap with available privileges: %v", err)
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			_ = runCaptureIP("link", "del", "dev", name)
		}
	})
	if err := runCaptureIP("link", "set", "dev", name, "up"); err != nil {
		t.Fatalf("bring capture tap up: %v", err)
	}

	// A tap interface with no process holding its /dev/net/tun fd open
	// reports NO-CARRIER/state DOWN (confirmed empirically on this
	// target: an AF_PACKET injection into such an interface is silently
	// dropped, 0 packets seen by tcpdump) even though `ip link set up`
	// marks it administratively up. In production this fd is held open
	// continuously by the supervisor's vtap code; hold it open here too,
	// bound to this specific interface via TUNSETIFF, for the gate's
	// duration so the tap actually has carrier and can pass traffic.
	tun, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("cannot open /dev/net/tun: %v", err)
	}
	tunClosed := false
	t.Cleanup(func() {
		if !tunClosed {
			_ = tun.Close()
		}
	})
	var ifr [40]byte
	copy(ifr[:16], name)
	binary.LittleEndian.PutUint16(ifr[16:], tunIFFTap|tunIFFNoPI)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tun.Fd(), tunSetIFF, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		t.Fatalf("TUNSETIFF %s: %v", name, errno)
	}

	capture, err := Start(name, "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("start capture on %s: %v", name, err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = capture.Close()
		}
	})

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", capture.Port()))
	if err != nil {
		t.Fatalf("connect capture stream: %v", err)
	}
	connClosed := false
	t.Cleanup(func() {
		if !connClosed {
			_ = conn.Close()
		}
	})

	iface, err := net.InterfaceByName(name)
	if err != nil {
		t.Fatalf("lookup capture tap: %v", err)
	}
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, capturePacketAll)
	if err != nil {
		t.Skipf("AF_PACKET injection unavailable: %v", err)
	}
	fdClosed := false
	t.Cleanup(func() {
		if !fdClosed {
			_ = syscall.Close(fd)
		}
	})
	frame := captureFrame()
	sll := &syscall.SockaddrLinklayer{Protocol: capturePacketAll, Ifindex: iface.Index}
	if err := syscall.Bind(fd, sll); err != nil {
		t.Fatalf("bind capture injector: %v", err)
	}

	// capture.Start launches its capture subprocess asynchronously; there is
	// no readiness signal exposed here, so a single injection immediately
	// after Start can race the subprocess actually attaching to the
	// interface and be missed forever. Re-inject on a short interval until
	// the capture observes at least one frame or the deadline passes, so the
	// gate measures whether capture works at all, not subprocess startup
	// latency.
	deadline := time.Now().Add(5 * time.Second)
	frames := uint64(0)
	for time.Now().Before(deadline) {
		if err := syscall.Sendto(fd, frame, 0, sll); err != nil {
			t.Fatalf("inject known frame: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		frames, _, _ = capture.Stats()
		if frames >= 1 {
			break
		}
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("close capture injector: %v", err)
	}
	fdClosed = true

	if frames == 0 {
		t.Fatal("capture did not observe the injected frame")
	}

	if err := tun.Close(); err != nil {
		t.Fatalf("close tun fd: %v", err)
	}
	tunClosed = true

	if err := capture.Close(); err != nil {
		t.Fatalf("stop capture: %v", err)
	}
	closed = true
	pcapData, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read pcapng stream: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close capture stream: %v", err)
	}
	connClosed = true
	if len(pcapData) == 0 || !bytes.Contains(pcapData, frame) {
		t.Fatalf("pcapng stream is empty or missing injected frame (len=%d)", len(pcapData))
	}

	pcapPath := t.TempDir() + "/phase1-capture.pcapng"
	if err := os.WriteFile(pcapPath, pcapData, 0o600); err != nil {
		t.Fatalf("write pcapng validation file: %v", err)
	}
	args := append([]string{}, validatorArgs...)
	args = append(args, pcapPath)
	if output, err := exec.Command(validator, args...).CombinedOutput(); err != nil {
		t.Fatalf("pcapng validator %s: %v: %s", validator, err, strings.TrimSpace(string(output)))
	}
	if err := os.Remove(pcapPath); err != nil {
		t.Fatalf("remove temporary pcapng: %v", err)
	}
	if _, err := os.Stat(pcapPath); !os.IsNotExist(err) {
		t.Fatalf("temporary pcapng remains: %v", err)
	}

	if err := runCaptureIP("link", "del", "dev", name); err != nil {
		t.Fatalf("delete capture tap: %v", err)
	}
	deleted = true
	// See the matching note in arm64_tap_test.go: net.InterfaceByName
	// reports a deleted interface via an error whose text contains "no
	// such network interface", not one os.IsNotExist recognizes.
	if _, err := net.InterfaceByName(name); err == nil || !strings.Contains(err.Error(), "no such network interface") {
		t.Fatalf("capture tap remains after cleanup: %v", err)
	}
}

func captureValidator(t *testing.T) (string, []string) {
	t.Helper()
	if _, err := exec.LookPath("tshark"); err == nil {
		return "tshark", []string{"-r"}
	}
	if _, err := exec.LookPath("capinfos"); err == nil {
		return "capinfos", nil
	}
	return "", nil
}

func captureFrame() []byte {
	frame := make([]byte, 60)
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x07})
	binary.BigEndian.PutUint16(frame[12:14], 0x88b5)
	copy(frame[14:], []byte("iolbox-phase1-capture"))
	for i := 35; i < len(frame); i++ {
		frame[i] = byte(i ^ 0xa5)
	}
	return frame
}

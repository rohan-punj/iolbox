//go:build linux

package iouyap

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const afPacketAll = 0x0300 // htons(ETH_P_ALL) on little-endian Linux

func hasCapNetRaw(t *testing.T) bool {
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
		return err == nil && eff&(uint64(1)<<13) != 0
	}
	return false
}

func runIPCommand(args ...string) error {
	cmd := exec.Command("ip", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func TestArm64AFPacketFrame(t *testing.T) {
	if !hasCapNetRaw(t) {
		t.Skip("requires root or CAP_NET_RAW")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skipf("ip command unavailable: %v", err)
	}

	name := fmt.Sprintf("m7afp-%d", os.Getpid()%100000)
	if len(name) >= 16 {
		name = "m7afp0"
	}
	if err := runIPCommand("link", "add", "dev", name, "type", "dummy"); err != nil {
		t.Skipf("cannot create test interface with available privileges: %v", err)
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			_ = runIPCommand("link", "del", "dev", name)
		}
	})
	if err := runIPCommand("link", "set", "dev", name, "up"); err != nil {
		t.Fatalf("bring test interface up: %v", err)
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		t.Fatalf("lookup test interface: %v", err)
	}

	// Linux AF_PACKET does not loop a transmitted frame back to the same
	// socket that sent it (confirmed empirically on this target: a
	// second, independent listener such as tcpdump sees a socket's own
	// outgoing frame, but that same sending socket's own recv never
	// does). Use two sockets bound to the test interface: rxFd only
	// receives, txFd only sends, matching how the product's own
	// capture/inject paths are actually separate listeners.
	rxFd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, afPacketAll)
	if err != nil {
		t.Skipf("AF_PACKET raw socket unavailable: %v", err)
	}
	rxClosed := false
	t.Cleanup(func() {
		if !rxClosed {
			_ = syscall.Close(rxFd)
		}
	})
	sll := &syscall.SockaddrLinklayer{Protocol: afPacketAll, Ifindex: iface.Index}
	if err := syscall.Bind(rxFd, sll); err != nil {
		t.Fatalf("bind AF_PACKET receive socket: %v", err)
	}
	tv := syscall.NsecToTimeval(2_000_000_000)
	if err := syscall.SetsockoptTimeval(rxFd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		t.Fatalf("set AF_PACKET receive timeout: %v", err)
	}

	txFd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, afPacketAll)
	if err != nil {
		t.Fatalf("AF_PACKET send socket unavailable: %v", err)
	}
	txClosed := false
	t.Cleanup(func() {
		if !txClosed {
			_ = syscall.Close(txFd)
		}
	})
	if err := syscall.Bind(txFd, sll); err != nil {
		t.Fatalf("bind AF_PACKET send socket: %v", err)
	}

	frame := make([]byte, 60)
	copy(frame[0:6], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02})
	copy(frame[6:12], []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01})
	frame[12], frame[13] = 0x88, 0xb5 // experimental EtherType
	for i := 14; i < len(frame); i++ {
		frame[i] = byte(i ^ 0x5a)
	}
	if err := syscall.Sendto(txFd, frame, 0, sll); err != nil {
		t.Fatalf("send known Ethernet frame: %v", err)
	}

	buf := make([]byte, 2048)
	n, _, err := syscall.Recvfrom(rxFd, buf, 0)
	if err != nil {
		t.Fatalf("receive known Ethernet frame: %v", err)
	}
	if !bytes.Equal(buf[:n], frame) {
		t.Fatalf("AF_PACKET frame = %x, want %x", buf[:n], frame)
	}
	if err := syscall.Close(txFd); err != nil {
		t.Fatalf("close AF_PACKET send socket: %v", err)
	}
	txClosed = true
	if err := syscall.Close(rxFd); err != nil {
		t.Fatalf("close AF_PACKET receive socket: %v", err)
	}
	rxClosed = true
	if err := runIPCommand("link", "del", "dev", name); err != nil {
		t.Fatalf("delete test interface: %v", err)
	}
	deleted = true
	// See the matching note in arm64_tap_test.go: net.InterfaceByName
	// reports a deleted interface via an error whose text contains "no
	// such network interface", not one os.IsNotExist recognizes.
	if _, err := net.InterfaceByName(name); err == nil || !strings.Contains(err.Error(), "no such network interface") {
		t.Fatalf("test interface remains after delete: %v", err)
	}
}

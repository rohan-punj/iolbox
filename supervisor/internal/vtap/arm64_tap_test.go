//go:build linux

package vtap

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

const (
	tunGetIFF   = 0x800454D2
	testIFFTap  = 0x0002
	testIFFNoPI = 0x1000
)

func hasCapNetAdmin(t *testing.T) bool {
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
		return err == nil && eff&(uint64(1)<<12) != 0
	}
	return false
}

func runIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func TestArm64TapLifecycle(t *testing.T) {
	if !hasCapNetAdmin(t) {
		t.Skip("requires root or CAP_NET_ADMIN")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skipf("/dev/net/tun unavailable: %v", err)
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skipf("ip command unavailable: %v", err)
	}

	name := fmt.Sprintf("m7tap-%d", os.Getpid()%100000)
	if len(name) >= maxIfNameSize {
		name = "m7tap0"
	}
	if err := runIP("tuntap", "add", "dev", name, "mode", "tap"); err != nil {
		t.Skipf("cannot create test tap with available privileges: %v", err)
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			_ = runIP("link", "del", "dev", name)
		}
	})

	dev, err := openTap(name)
	if err != nil {
		t.Fatalf("openTap(%q): %v", name, err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = dev.Close()
		}
	})

	var req [40]byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(dev.Fd()), tunGetIFF, uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		t.Fatalf("TUNGETIFF: %v", errno)
	}
	gotName := string(req[:16])
	if nul := strings.IndexByte(gotName, 0); nul >= 0 {
		gotName = gotName[:nul]
	}
	if gotName != name {
		t.Fatalf("ifreq name = %q, want %q", gotName, name)
	}
	flags := binary.LittleEndian.Uint16(req[16:])
	if flags&(testIFFTap|testIFFNoPI) != testIFFTap|testIFFNoPI {
		t.Fatalf("ifreq flags = %#x, want TAP|NO_PI (%#x)", flags, testIFFTap|testIFFNoPI)
	}

	if err := runIP("link", "set", "dev", name, "up"); err != nil {
		t.Fatalf("bring tap up: %v", err)
	}
	if iface, err := net.InterfaceByName(name); err != nil || iface.Flags&net.FlagUp == 0 {
		t.Fatalf("tap after link up = iface=%v err=%v", iface, err)
	}
	if err := runIP("link", "set", "dev", name, "down"); err != nil {
		t.Fatalf("bring tap down: %v", err)
	}
	if iface, err := net.InterfaceByName(name); err != nil || iface.Flags&net.FlagUp != 0 {
		t.Fatalf("tap after link down = iface=%v err=%v", iface, err)
	}

	if err := dev.Close(); err != nil {
		t.Fatalf("close tun fd: %v", err)
	}
	closed = true
	if _, err := dev.Stat(); err == nil {
		t.Fatalf("tun fd still usable after close: %v", err)
	}
	if err := runIP("link", "del", "dev", name); err != nil {
		t.Fatalf("delete test tap: %v", err)
	}
	deleted = true
	// net.InterfaceByName reports a deleted interface as a *net.OpError
	// wrapping "no such network interface" (a route-netlink lookup
	// failure), not a plain os.ErrNotExist-compatible error, so the
	// absence check matches on that text rather than errors.Is.
	if _, err := net.InterfaceByName(name); err == nil || !strings.Contains(err.Error(), "no such network interface") {
		t.Fatalf("test tap remains after delete: %v", err)
	}
}

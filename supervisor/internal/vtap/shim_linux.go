//go:build linux

package vtap

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// TUNSETIFF and the IFF_* flags used to attach /dev/net/tun to an existing
// persistent tap device, linux/amd64 values (this module has no
// golang.org/x/sys dependency — go.mod pins only creack/pty — so these are the
// raw ioctl constants rather than symbolic ones from a syscall package).
// Copied from internal/iouyap/tap_linux.go's openTap recipe.
const (
	tunsetiff = 0x400454CA
	iffTap    = 0x0002
	iffNoPI   = 0x1000
)

// maxIfNameSize is the kernel's IFNAMSIZ: interface names (including the NUL
// terminator implied by the fixed-size ifreq field) must fit in 16 bytes, so
// usable names are at most 15 characters.
const maxIfNameSize = 16

// openTap attaches to an EXISTING persistent tap device named name and
// returns it as an *os.File ready for raw ethernet frame reads/writes (no
// netio header, no packet-info prefix: IFF_NO_PI). The device must already
// exist (created out-of-band, e.g. `ip tuntap add dev <name> mode tap user
// <uid>`, by the fabric manager that owns tap lifecycle) and be owned by the
// calling uid; opening a persistent tap owned by the caller requires no
// additional privilege.
//
// This does NOT create or delete the tap device: Shim.Close leaves it in
// place for the fabric manager to reuse or tear down.
func openTap(name string) (*os.File, error) {
	if len(name) == 0 || len(name) >= maxIfNameSize {
		return nil, fmt.Errorf("vtap: tap device name %q must be 1-%d bytes", name, maxIfNameSize-1)
	}

	// Open the clone device raw and issue TUNSETIFF BEFORE the fd reaches the Go
	// runtime. os.OpenFile registers the fd with the network poller at open time —
	// before TUNSETIFF turns it into a concrete tap — and that early registration
	// is unreliable for tun devices, so a later read flakily fails with
	// "not pollable"/EFAULT and kills the pump (dropping all inbound frames for
	// the VPCS node). Registering post-TUNSETIFF via os.NewFile on a non-blocking
	// configured tap is reliable.
	fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("vtap: open /dev/net/tun: %w", err)
	}

	// ifreq layout: 16-byte name field followed by a uint16 flags field,
	// padded out to the kernel's struct ifreq size (40 bytes on amd64).
	var req [40]byte
	copy(req[:16], name)
	binary.LittleEndian.PutUint16(req[16:], iffTap|iffNoPI)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tunsetiff, uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("vtap: TUNSETIFF %s: %w", name, errno)
	}

	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("vtap: set nonblock %s: %w", name, err)
	}
	return os.NewFile(uintptr(fd), "/dev/net/tun"), nil
}

// Shim bridges one VPCS UDP tunnel to one tap device: a udp<->tap frame pump
// pair with no header manipulation (see package doc). It is process-less —
// the fabric/orchestrator drives Start/Close directly.
type Shim struct {
	tap     *os.File
	udpConn *net.UDPConn
	sendTo  *net.UDPAddr

	closeOnce sync.Once
	closed    chan struct{}
	pumpWG    sync.WaitGroup
}

// Start opens the existing tap device tapName, binds a UDP socket to
// 127.0.0.1:bindPort (the port VPCS's `-c` sends to), resolves sendPort as
// where VPCS listens (VPCS's `-s`), and launches the two pump goroutines.
//
// tapName must already exist (the fabric manager creates it, e.g. via
// `ip tuntap add ... user <uid>`, before calling Start). On any failure Start
// reverses whatever it already opened and returns the error.
func Start(tapName string, bindPort, sendPort int) (*Shim, error) {
	tap, err := openTap(tapName)
	if err != nil {
		return nil, err
	}

	laddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: bindPort}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		_ = tap.Close()
		return nil, fmt.Errorf("vtap: bind udp %s: %w", laddr, err)
	}

	s := &Shim{
		tap:     tap,
		udpConn: conn,
		sendTo:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: sendPort},
		closed:  make(chan struct{}),
	}

	s.pumpWG.Add(2)
	go func() { defer s.pumpWG.Done(); pumpUDPToTap(s.closed, s.udpConn, s.tap) }()
	go func() { defer s.pumpWG.Done(); pumpTapToUDP(s.closed, s.tap, s.udpConn, s.sendTo) }()

	return s, nil
}

// Close is idempotent. It stops the pumps and closes the tap fd + udp socket,
// but does NOT delete the tap device — the fabric manager owns its lifecycle.
//
// The tap->udp pump has no read-deadline knob to poll (tap fds don't support
// SetReadDeadline the way a UDP socket does), so its blocked Read is unblocked
// by closing the tap fd here, which makes the pending Read return an error
// and the pump goroutine exit.
func (s *Shim) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.udpConn != nil {
			_ = s.udpConn.SetReadDeadline(time.Now().Add(-time.Second))
		}
		if s.tap != nil {
			_ = s.tap.Close() // unblocks the tap->udp pump's pending Read
		}
		if s.udpConn != nil {
			_ = s.udpConn.Close()
		}
		s.pumpWG.Wait()
	})
	return nil
}

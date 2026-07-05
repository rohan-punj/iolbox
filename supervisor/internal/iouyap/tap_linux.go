//go:build linux

package iouyap

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// TUNSETIFF and the IFF_* flags used to attach /dev/net/tun to an existing
// persistent tap device, linux/amd64 values (there is no golang.org/x/sys
// dependency in this module, so these are the raw ioctl constants rather
// than symbolic ones from a syscall package).
const (
	tunsetiff = 0x400454CA
	iffTap    = 0x0002
	iffNoPI   = 0x1000
)

// maxIfNameSize is the kernel's IFNAMSIZ: interface names (including the
// NUL terminator implied by the fixed-size ifreq field) must fit in 16
// bytes, so usable names are at most 15 characters.
const maxIfNameSize = 16

// openTap attaches to an EXISTING persistent tap device named name and
// returns it as an *os.File ready for raw ethernet frame reads/writes (no
// netio header, no packet-info prefix: IFF_NO_PI). The device must already
// exist (created out-of-band, e.g. `ip tuntap add dev <name> mode tap user
// <uid>`, by the fabric manager that owns tap lifecycle) and be owned by
// the calling uid; opening a persistent tap owned by the caller requires no
// additional privilege.
//
// This does NOT create or delete the tap device: TapBridge.Close leaves it
// in place for the fabric manager to reuse or tear down.
func openTap(name string) (*os.File, error) {
	if len(name) == 0 || len(name) >= maxIfNameSize {
		return nil, fmt.Errorf("iouyap: tap device name %q must be 1-%d bytes", name, maxIfNameSize-1)
	}

	// Open the clone device with a raw syscall and issue TUNSETIFF BEFORE handing
	// the fd to the Go runtime. os.OpenFile registers the fd with the network
	// poller at open time — while it is still the bare /dev/net/tun clone device,
	// before TUNSETIFF turns it into a concrete tap — and that early registration
	// is unreliable for tun devices: a later read then flakily fails with
	// "not pollable" or EFAULT, which permanently kills the read pump and silently
	// drops every inbound frame for the interface (a lab node then never receives
	// e.g. its DHCP OFFER). Registering with the poller only after TUNSETIFF, via
	// os.NewFile on a fully-configured non-blocking tap, is reliable.
	fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("iouyap: open /dev/net/tun: %w", err)
	}

	// ifreq layout: 16-byte name field followed by a uint16 flags field,
	// padded out to the kernel's struct ifreq size (40 bytes on amd64).
	var req [40]byte
	copy(req[:16], name)
	binary.LittleEndian.PutUint16(req[16:], iffTap|iffNoPI)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tunsetiff, uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("iouyap: TUNSETIFF %s: %w", name, errno)
	}

	// Non-blocking so os.NewFile registers the configured tap with the poller:
	// reads park until a frame arrives and Close() unblocks them by evicting the
	// fd from the poller.
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("iouyap: set nonblock %s: %w", name, err)
	}
	return os.NewFile(uintptr(fd), "/dev/net/tun"), nil
}

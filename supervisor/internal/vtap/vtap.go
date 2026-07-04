// Package vtap bridges a VPCS UDP tunnel to a Linux tap device so a VPCS node
// can sit on the same bridge fabric as every other node type.
//
// VPCS (GNS3 vpcs 0.8.3) speaks a UDP tunnel natively: `-s <localUdp>` is the
// port VPCS BINDS to receive frames, `-c <remoteUdp>` is the port VPCS SENDS
// to, and `-t 127.0.0.1` keeps it on loopback. The datagram payload is a RAW
// ETHERNET FRAME with no header at all — confirmed by internal/relay, whose
// pump forwards each received datagram verbatim ("the datagram already IS the
// clean ethernet frame (the mesh is headerless; iouyap strips IOL's netio
// header at the unix-socket edge)", internal/relay/relay_linux.go) and by
// internal/iouyap/header.go, which notes the netio header "exists ONLY on the
// netio (unix socket) side ... so the UDP mesh ... carries raw ethernet frames
// with no header at all." So this shim does zero header manipulation: it just
// pumps bytes between a UDP socket and a tap fd.
//
// The tap device itself is created and destroyed by the fabric manager (e.g.
// `ip tuntap add dev <name> mode tap user <uid>`), matching the iouyap/extnet
// nat-tap pattern; this package only opens an existing tap and pumps frames.
package vtap

import (
	"io"
	"net"
	"time"
)

// frameMax is the largest ethernet frame (with generous headroom for VLAN
// tags/jumbo frames) the pumps will move in one read.
const frameMax = 65536

// pollInterval bounds how long a blocked read waits before re-checking for
// shutdown. Mirrors the extnet/iouyap pump poll pattern.
const pollInterval = 500 * time.Millisecond

// udpConn is the subset of *net.UDPConn the pumps need, so tests can supply a
// real loopback pair without a tap device.
type udpConn interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	SetReadDeadline(t time.Time) error
	Close() error
}

// tapDev is the subset of *os.File the pumps need on the tap side, so tests
// can supply an os.Pipe (or any io.ReadWriteCloser) instead of a real
// /dev/net/tun fd. A tap fd has no read-deadline knob (see Close), so this
// intentionally does NOT require SetReadDeadline.
type tapDev interface {
	io.ReadWriteCloser
}

// pumpUDPToTap reads datagrams (raw ethernet frames) off conn and writes each
// one straight to tap, with no header manipulation. It polls with a
// read-deadline so it notices closed promptly instead of blocking forever on
// a socket with no more traffic.
func pumpUDPToTap(closed <-chan struct{}, conn udpConn, tap tapDev) {
	buf := make([]byte, frameMax)
	for {
		select {
		case <-closed:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(pollInterval))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		if n == 0 {
			continue
		}
		if _, err := tap.Write(buf[:n]); err != nil {
			select {
			case <-closed:
				return
			default:
			}
		}
	}
}

// pumpTapToUDP reads ethernet frames off tap and sends each one, unmodified,
// to sendTo. The tap fd has no read-deadline knob, so this loop cannot poll a
// stop channel the way pumpUDPToTap does: unblocking a pending Read on
// shutdown is Close's job, by closing the tap fd out from under the read
// (see Shim.Close on linux).
func pumpTapToUDP(closed <-chan struct{}, tap tapDev, conn udpConn, sendTo *net.UDPAddr) {
	buf := make([]byte, frameMax)
	for {
		n, err := tap.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		if _, err := conn.WriteToUDP(buf[:n], sendTo); err != nil {
			select {
			case <-closed:
				return
			default:
			}
		}
	}
}

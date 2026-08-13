//go:build linux

package slowtee

import (
	"net"
	"sync"
	"syscall"
	"time"
)

// recvTimeout is the SO_RCVTIMEO installed on every bound raw socket, and is
// what makes the forward loops stoppable: close(fd) does not reliably wake a
// thread already parked in recvfrom, and AF_PACKET has no shutdown handler
// (EOPNOTSUPP). Mirrors dirstat.recvTimeout.
const recvTimeout = 250 * time.Millisecond

// ethPAll is ETH_P_ALL in network byte order for the socket protocol field,
// so the AF_PACKET socket sees every ethertype in both directions (mirrors
// dirstat's bindTap).
const ethPAll = 0x0300 // htons(ETH_P_ALL) on little-endian amd64

// packetOutgoing is sll_pkttype PACKET_OUTGOING: a frame the host TRANSMITTED
// out this tap. Dropping these is mandatory — without it, a frame this Tee
// injects on a tap would be re-read on that same tap's socket and
// re-forwarded back to the peer, causing an infinite loop/storm.
const packetOutgoing = 4

// frameLen is the read buffer size: a full frame (not header-only, unlike
// dirstat), since the whole LACPDU must be forwarded byte-for-byte.
const frameLen = 2048

// open binds a raw socket to each of exactly two tap devices and starts one
// read goroutine per socket, forwarding only LACP slow-protocols frames to
// the peer tap. See the Tee doc for scope/degrade rules.
func open(devs []string) (*Tee, error) {
	if len(devs) != 2 {
		return nil, nil // not a p2p link (or nothing to tee) -- non-fatal, no tee
	}

	fdA, err := bindTap(devs[0])
	if err != nil {
		return nil, nil // degrade: no tee for this link, non-fatal
	}
	fdB, err := bindTap(devs[1])
	if err != nil {
		_ = syscall.Close(fdA)
		return nil, nil // degrade: no tee for this link, non-fatal
	}

	ifaceA, err := net.InterfaceByName(devs[0])
	if err != nil {
		_ = syscall.Close(fdA)
		_ = syscall.Close(fdB)
		return nil, nil
	}
	ifaceB, err := net.InterfaceByName(devs[1])
	if err != nil {
		_ = syscall.Close(fdA)
		_ = syscall.Close(fdB)
		return nil, nil
	}

	t := &Tee{wg: &sync.WaitGroup{}}
	stop := make(chan struct{})
	t.wg.Add(2)
	go func() {
		defer t.wg.Done()
		forwardLoop(fdA, fdB, ifaceB.Index, stop)
	}()
	go func() {
		defer t.wg.Done()
		forwardLoop(fdB, fdA, ifaceA.Index, stop)
	}()

	// stopRead signals; the sockets are closed only after Close has seen both
	// loops return, so neither loop can ever touch a recycled descriptor.
	var stopOnce, closeOnce sync.Once
	t.stopRead = func() { stopOnce.Do(func() { close(stop) }) }
	t.closeFDs = func() {
		closeOnce.Do(func() {
			_ = syscall.Close(fdA)
			_ = syscall.Close(fdB)
		})
	}
	return t, nil
}

// bindTap opens a raw AF_PACKET socket and binds it to the named tap device
// by interface index, so it only sees that tap's frames. Mirrors
// dirstat.bindTap exactly.
func bindTap(dev string) (int, error) {
	iface, err := net.InterfaceByName(dev)
	if err != nil {
		return -1, err
	}
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(ethPAll))
	if err != nil {
		return -1, err
	}
	sll := syscall.SockaddrLinklayer{
		Protocol: ethPAll,
		Ifindex:  iface.Index,
	}
	if err := syscall.Bind(fd, &sll); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	tv := syscall.NsecToTimeval(recvTimeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		// An unstoppable forward loop is worse than no LACP passthrough on this
		// link, so treat this as a bind failure (open() then degrades to no tee).
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

// forwardLoop reads full frames off fd and, for each one that was RECEIVED
// (not our own injection) and is a slow-protocols (LACP) frame, injects it
// onto peerFd's netdev (peerIfindex) so the node behind the peer tap sees it.
// Exits when stop is closed (within recvTimeout, or immediately on a busy
// socket); the caller closes the fds only after both loops have returned.
func forwardLoop(fd, peerFd, peerIfindex int, stop <-chan struct{}) {
	buf := make([]byte, frameLen)
	for {
		select {
		case <-stop:
			return // a continuously busy socket never reaches the EAGAIN path
		default:
		}
		n, from, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			if err == syscall.EINTR || err == syscall.EAGAIN {
				continue // EAGAIN is the SO_RCVTIMEO tick; re-check stop above
			}
			return // fatal socket error
		}
		if n <= 0 {
			continue
		}
		sll, ok := from.(*syscall.SockaddrLinklayer)
		if !ok || sll.Pkttype == packetOutgoing {
			continue // our own injection on this tap; drop to avoid a forward loop
		}
		if !isSlowProtocols(buf[:n]) {
			continue // everything else already crosses via the kernel bridge
		}
		dest := &syscall.SockaddrLinklayer{Ifindex: peerIfindex}
		_ = syscall.Sendto(peerFd, buf[:n], 0, dest)
	}
}

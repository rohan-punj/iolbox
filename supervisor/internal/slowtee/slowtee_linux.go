//go:build linux

package slowtee

import (
	"net"
	"sync"
	"syscall"
)

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
	t.wg.Add(2)
	go func() {
		defer t.wg.Done()
		forwardLoop(fdA, fdB, ifaceB.Index)
	}()
	go func() {
		defer t.wg.Done()
		forwardLoop(fdB, fdA, ifaceA.Index)
	}()

	var once sync.Once
	t.closer = func() {
		once.Do(func() {
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
	return fd, nil
}

// forwardLoop reads full frames off fd and, for each one that was RECEIVED
// (not our own injection) and is a slow-protocols (LACP) frame, injects it
// onto peerFd's netdev (peerIfindex) so the node behind the peer tap sees it.
// Exits when fd is closed on teardown.
func forwardLoop(fd, peerFd, peerIfindex int) {
	buf := make([]byte, frameLen)
	for {
		n, from, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return // fd closed on teardown, or a fatal socket error
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

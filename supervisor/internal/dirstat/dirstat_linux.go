//go:build linux

package dirstat

import (
	"fmt"
	"net"
	"sync"
	"syscall"

	"github.com/rohanpunj/iolab/supervisor/internal/relay"
)

// snapLen caps how many leading bytes of each frame the raw socket delivers. The
// deepest fixed-offset peek relay.ClassifyDetailed makes is the BGP message-type
// byte behind an 802.1Q tag + 20-byte IP header + up-to-60-byte TCP header + a
// 16-byte marker; 128 bytes covers every subtype peek with headroom while
// keeping the per-frame copy tiny (header-only, as the doc requires). A frame
// truncated before a subtype byte simply yields subtype "" — the same graceful
// degradation as a segmented stream.
const snapLen = 128

// ethPAll is ETH_P_ALL in network byte order for the socket protocol field, so
// the AF_PACKET socket sees every ethertype in both directions.
const ethPAll = 0x0300 // htons(ETH_P_ALL) on little-endian amd64

// packetOutgoing is sll_pkttype PACKET_OUTGOING: a frame the host TRANSMITTED
// out this tap (bridge->node). Everything else (HOST/BROADCAST/MULTICAST/
// OTHERHOST) is a frame the host RECEIVED on the tap — i.e. sourced by the node
// behind it, which is the direction we attribute to that endpoint.
const packetOutgoing = 4

// Open binds a raw AF_PACKET socket to each of the (up to two) endpoint tap
// devices and starts a header-only read goroutine per socket that classifies
// each RECEIVED frame and attributes it to that endpoint index. devs are the tap
// device names in doc endpoint order (devs[0] -> endpoint 0). A device that
// can't be bound (missing, or the process lacks CAP_NET_RAW) is skipped with a
// note in err's context but does not fail the others — the link still gets
// whatever direction it can. Returns a nil *Classifier (and nil error) when no
// socket could be opened at all, so the caller degrades to aggregate-only stats.
//
// The appliance runs the supervisor as root, so both sockets normally bind; the
// non-root dev box simply gets a nil Classifier and the aggregate fps/bps glow
// is unaffected.
func Open(devs []string) (*Classifier, error) {
	if len(devs) == 0 {
		return nil, nil
	}
	c := &Classifier{counts: make(Counters), wg: &sync.WaitGroup{}}
	var fds []int
	var firstErr error
	for ep, dev := range devs {
		if ep > 1 {
			break // a link has exactly two endpoints; ignore any extras defensively
		}
		if dev == "" {
			continue
		}
		fd, err := bindTap(dev)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("dirstat: bind %s: %w", dev, err)
			}
			continue
		}
		fds = append(fds, fd)
		c.wg.Add(1)
		go func(ep, fd int) {
			defer c.wg.Done()
			c.readLoop(ep, fd)
		}(ep, fd)
	}
	if len(fds) == 0 {
		return nil, firstErr
	}
	// closer shuts every read loop down: closing the socket fd makes the blocked
	// recvfrom return an error and the goroutine exit. Idempotent via a once.
	var once sync.Once
	c.closer = func() {
		once.Do(func() {
			for _, fd := range fds {
				_ = syscall.Close(fd)
			}
		})
	}
	return c, firstErr
}

// bindTap opens a raw AF_PACKET socket and binds it to the named tap device by
// interface index, so it only sees that tap's frames. The socket is left in
// blocking mode; the read loop's recvfrom returns on Close (fd closed).
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

// readLoop reads frames off one bound raw socket, classifies each frame the host
// RECEIVED on the tap (sourced by the endpoint behind it), and increments the
// endpoint's counter. It reads at most snapLen bytes per frame (header only) and
// exits when the socket is closed. Frames the host TRANSMITTED out the tap
// (PACKET_OUTGOING) are the peer's mirror and are dropped to avoid double count.
func (c *Classifier) readLoop(ep, fd int) {
	buf := make([]byte, snapLen)
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
		if sll, ok := from.(*syscall.SockaddrLinklayer); ok && sll.Pkttype == packetOutgoing {
			continue // host->node mirror; the peer tap counts the node->host side
		}
		label, subtype, _ := relay.ClassifyDetailed(buf[:n])
		c.count(ep, label, subtype)
	}
}

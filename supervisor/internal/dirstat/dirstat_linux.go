//go:build linux

package dirstat

import (
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/relay"
)

// recvTimeout is the SO_RCVTIMEO installed on every bound raw socket. It is the
// mechanism that makes teardown work at all: a blocking recvfrom that never
// times out cannot be interrupted by closing the descriptor from another
// goroutine (see bindTap), so the read loop instead wakes with EAGAIN at this
// cadence and checks the stop signal. It costs one wake-up per socket per
// interval on an idle link, which is nothing next to the classification work a
// busy link already does.
const recvTimeout = 250 * time.Millisecond

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
const ethPAll = 0x0300 // htons(ETH_P_ALL) on little-endian 64-bit Linux

// packetOutgoing is sll_pkttype PACKET_OUTGOING: a frame the host TRANSMITTED
// out this tap (bridge->node). Everything else (HOST/BROADCAST/MULTICAST/
// OTHERHOST) is a frame the host RECEIVED on the tap — i.e. sourced by the node
// behind it, which is the direction we attribute to that endpoint.
const packetOutgoing = 4

// Open binds a raw AF_PACKET socket to each of the (up to two) endpoint tap
// devices and starts a header-only read goroutine per socket that classifies
// each RECEIVED frame and attributes it to that endpoint index. devs carries
// the document endpoint index explicitly because the slice is sparse. A device that
// can't be bound (missing, or the process lacks CAP_NET_RAW) is skipped with a
// note in err's context but does not fail the others — the link still gets
// whatever direction it can. Returns a nil *Classifier (and nil error) when no
// socket could be opened at all, so the caller degrades to aggregate-only stats.
//
// The appliance runs the supervisor as root, so both sockets normally bind; the
// non-root dev box simply gets a nil Classifier and the aggregate fps/bps glow
// is unaffected.
func Open(devs []EndpointDev) (*Classifier, error) {
	if len(devs) == 0 {
		return nil, nil
	}
	c := newClassifier(devs)
	var fds []int
	var firstErr error
	stop := make(chan struct{})
	for _, d := range devs {
		if d.Index < 0 || d.Index > 1 {
			continue // a link has exactly two endpoints; ignore extras defensively
		}
		if d.Dev == "" {
			continue
		}
		fd, err := bindTap(d.Dev)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("dirstat: bind %s: %w", d.Dev, err)
			}
			continue
		}
		fds = append(fds, fd)
		c.wg.Add(1)
		go func(ep, fd int) {
			defer c.wg.Done()
			c.readLoop(ep, fd, stop)
		}(d.Index, fd)
	}
	if len(fds) == 0 {
		close(stop)
		return nil, firstErr
	}
	// stopRead only signals; the sockets are closed by closeFDs, which
	// Classifier.Close runs after the read loops have provably returned. Closing
	// an fd out from under a goroutine parked in recvfrom does NOT reliably wake
	// it on Linux (see bindTap) and races descriptor reuse, so the signal — not
	// the close — is what ends the loops. Both hooks are idempotent.
	var stopOnce, closeOnce sync.Once
	c.stopRead = func() { stopOnce.Do(func() { close(stop) }) }
	c.closeFDs = func() {
		closeOnce.Do(func() {
			for _, fd := range fds {
				_ = syscall.Close(fd)
			}
		})
	}
	return c, firstErr
}

// bindTap opens a raw AF_PACKET socket and binds it to the named tap device by
// interface index, so it only sees that tap's frames.
//
// The socket stays in blocking mode but carries an SO_RCVTIMEO, which is what
// makes the read loop stoppable. close(fd) from another goroutine does NOT
// reliably return a thread already parked inside recvfrom on Linux, and
// shutdown() is not an option either: AF_PACKET has no shutdown handler
// (packet_ops wires sock_no_shutdown), so it fails with EOPNOTSUPP and wakes
// nothing. A bounded receive timeout plus a stop channel is therefore the
// mechanism; see readLoop.
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
		// Without the timeout the read loop would be unstoppable, which is far
		// worse than this link having no directional data, so fail the bind.
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

// readLoop reads frames off one bound raw socket, classifies each frame the host
// RECEIVED on the tap (sourced by the endpoint behind it), and increments the
// endpoint's counter. It reads at most snapLen bytes per frame (header only) and
// exits when stop is closed (within recvTimeout, or immediately on a busy link).
// Frames the host TRANSMITTED out the tap (PACKET_OUTGOING) are the peer's
// mirror and are dropped to avoid double count.
//
// The caller closes the fd only after this goroutine has returned, so the
// descriptor is always valid for the whole life of this loop.
func (c *Classifier) readLoop(ep, fd int, stop <-chan struct{}) {
	buf := make([]byte, snapLen)
	for {
		select {
		case <-stop:
			return // a continuously busy socket never sees the EAGAIN path
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
		if sll, ok := from.(*syscall.SockaddrLinklayer); ok && sll.Pkttype == packetOutgoing {
			continue // host->node mirror; the peer tap counts the node->host side
		}
		label, subtype, _ := relay.ClassifyDetailed(buf[:n])
		c.count(ep, label, subtype)
		if n >= 12 {
			var source [6]byte
			copy(source[:], buf[6:12])
			c.observeSource(ep, source, monotonicNow())
		}
	}
}

// Package slowtee is a tiny AF_PACKET forwarder that carries ONLY the
// LACP/slow-protocols multicast (01:80:C2:00:00:02, EtherType 0x8809) between
// the two member taps of a p2p fabric link.
//
// The fabric's kernel bridge (iolbr<linkid>) forwards data, CDP, STP, and PAgP
// fine, but the kernel refuses to enable group_fwd_mask bit 2 (LACP) at all —
// it returns EIO on that bit — so a bridge alone can never carry
// 01:80:C2:00:00:02 between the two switches on a link, and
// `channel-group … mode active/passive` port-channels never negotiate.
//
// Tee plugs that one gap: it binds a raw socket to each of the link's two
// member taps and, on each side, forwards only frames addressed to the LACP
// multicast to the OTHER tap. Everything else (including PAgP, which already
// works) is left entirely to the kernel bridge — Tee never touches it.
//
// SCOPE: p2p fabric links only (exactly two taps). N-port segments (hubs)
// are out of scope for v1 — LACP across a shared segment would need
// flood-to-all-members semantics, not a single peer-to-peer forward.
package slowtee

import (
	"bytes"
	"sync"
)

// slowProtoDst is the destination MAC of IEEE 802.3 slow-protocols frames
// (LACP among them), EtherType 0x8809.
var slowProtoDst = []byte{0x01, 0x80, 0xc2, 0x00, 0x00, 0x02}

// isSlowProtocols reports whether frame is an IEEE 802.3 slow-protocols
// (LACP) frame: its destination MAC is 01:80:c2:00:00:02. Frames shorter than
// an Ethernet header (14 bytes) are never matched. Defined here (not
// slowtee_linux.go) so slowtee_test.go can exercise this pure logic on every
// platform, mirroring how dirstat keeps its pure logic out of the linux-only
// file.
func isSlowProtocols(frame []byte) bool {
	return len(frame) >= 14 && bytes.Equal(frame[0:6], slowProtoDst)
}

// Tee holds the two bound raw sockets and the goroutines that shuttle
// slow-protocols frames between them. The socket/goroutine machinery lives in
// the _linux.go build; the _other.go stub never populates closer/wg, so its
// Close is a no-op and Open always returns (nil, nil).
type Tee struct {
	mu sync.Mutex

	// closer/wg are populated only by the linux implementation.
	closer func()
	wg     *sync.WaitGroup
}

// Open binds a raw AF_PACKET socket to each of exactly two tap device names
// and starts one read goroutine per socket: each forwards only the LACP
// slow-protocols multicast (01:80:C2:00:00:02) it RECEIVES on its tap to the
// OTHER tap, so the two switches behind the taps can exchange LACPDUs across a
// link whose bridge can't carry that multicast.
//
// Open is deliberately narrow: fewer or more than two devs (nothing to tee, or
// an N-port segment which is out of scope — see the package doc) returns
// (nil, nil), and a bind failure on EITHER tap tears down whatever was opened
// and also returns (nil, nil) — a missing tee just costs that link LACP
// passthrough; every other protocol still works.
func Open(devs []string) (*Tee, error) {
	return open(devs)
}

// Close stops the read goroutines and closes both sockets. Idempotent and
// nil-safe (a stub / non-fabric Tee is nil).
func (t *Tee) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	closer := t.closer
	wg := t.wg
	t.mu.Unlock()
	if closer != nil {
		closer()
	}
	if wg != nil {
		wg.Wait()
	}
}

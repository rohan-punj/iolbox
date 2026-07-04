// Package dirstat is the always-on, header-only, per-endpoint-tap directional
// traffic classifier for fabric links. It answers the question a
// tcpdump-on-bridge capture can't: which endpoint SOURCED each frame.
//
// For each fabric link the server opens one Classifier over the link's two
// endpoint tap devices. The Classifier binds a raw AF_PACKET socket to each tap
// (Linux only) and, in a per-socket goroutine, reads just the leading bytes of
// every frame — enough for relay.ClassifyDetailed — and increments a counter
// keyed by (endpoint index, protocol label, subtype). A frame RECEIVED on a tap
// (sll_pkttype != PACKET_OUTGOING) was sent by the node behind that tap, so it
// is attributed as sourced from that endpoint; a frame the host sent OUT the tap
// (PACKET_OUTGOING) is the mirror of the peer's ingress and is not double
// counted.
//
// The counters are cumulative uint64s; the stats loop diffs two Snapshot()s the
// same way it diffs the netdev fps/bps counters, so this never drives emission
// on its own and can't regress the always-on glow. Close() tears the sockets
// and goroutines down; the server owns one Classifier per fabric link and closes
// them on lab.stop / link.remove / teardownFabric.
package dirstat

import "sync"

// key identifies one accumulator bucket: which endpoint sourced the frame (0 or
// 1), its primary protocol label, and its packet-type subtype ("" when the
// label has none / it couldn't be decoded).
type key struct {
	ep      int
	label   string
	subtype string
}

// Counters is a cumulative snapshot of a Classifier's per-(endpoint,label,
// subtype) frame counts, safe for the caller to keep and diff against a later
// snapshot. It is a plain map so the stats loop can iterate it without touching
// dirstat internals.
type Counters map[key]uint64

// Snapshot copies the live counters under the lock. The returned map is owned by
// the caller. Nil-safe: a nil *Classifier (non-fabric / stub) yields nil.
func (c *Classifier) Snapshot() Counters {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(Counters, len(c.counts))
	for k, v := range c.counts {
		out[k] = v
	}
	return out
}

// count is the shared accumulator increment, called from the read goroutines
// (linux) and kept here so the field layout and locking live in one place.
func (c *Classifier) count(ep int, label, subtype string) {
	c.mu.Lock()
	c.counts[key{ep: ep, label: label, subtype: subtype}]++
	c.mu.Unlock()
}

// Classifier holds the counters shared by the (platform-specific) read
// goroutines. The socket/goroutine machinery lives in the _linux.go build; the
// _other.go stub leaves those fields zero and Open returns a nil *Classifier.
type Classifier struct {
	mu     sync.Mutex
	counts Counters

	// closer/wg are populated only by the linux implementation; the stub never
	// sets them so its Close is a no-op.
	closer func()
	wg     *sync.WaitGroup
}

// Direction sums a Counters diff over an interval into a per-(label,subtype)
// pair of directional rates [fpsFromEp0, fpsFromEp1]. It is defined here (not in
// the stats loop) so the diff math is next to the counter definition. prev is
// the previous snapshot (nil on the first tick), cur the current one; secs is
// the interval in seconds. A bucket whose counter went backwards (socket
// re-opened after a link re-add) is skipped for this tick. round rounds each
// rate to match the loop's one-decimal fps.
func Direction(prev, cur Counters, secs float64, round func(float64) float64) (
	byLabel map[string][2]float64,
	byLabelSubtype map[string]map[string][2]float64,
) {
	if len(cur) == 0 {
		return nil, nil
	}
	byLabel = make(map[string][2]float64)
	byLabelSubtype = make(map[string]map[string][2]float64)
	for k, c := range cur {
		p := prev[k] // 0 if unseen last tick
		if c < p {
			continue // counter reset; re-baseline silently
		}
		d := c - p
		if d == 0 {
			continue
		}
		rate := round(float64(d) / secs)
		if rate == 0 {
			continue
		}
		lab := byLabel[k.label]
		lab[k.ep] += rate
		byLabel[k.label] = lab

		if k.subtype != "" {
			sub := byLabelSubtype[k.label]
			if sub == nil {
				sub = make(map[string][2]float64)
				byLabelSubtype[k.label] = sub
			}
			e := sub[k.subtype]
			e[k.ep] += rate
			sub[k.subtype] = e
		}
	}
	if len(byLabel) == 0 {
		byLabel = nil
	}
	if len(byLabelSubtype) == 0 {
		byLabelSubtype = nil
	}
	return byLabel, byLabelSubtype
}

// Close stops the read goroutines and closes the sockets. Idempotent and
// nil-safe (a stub / non-fabric Classifier is nil).
func (c *Classifier) Close() {
	if c == nil {
		return
	}
	if c.closer != nil {
		c.closer()
	}
	if c.wg != nil {
		c.wg.Wait()
	}
}

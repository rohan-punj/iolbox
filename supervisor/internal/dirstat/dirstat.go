// Package dirstat is the always-on, header-only, per-endpoint-tap directional
// traffic classifier for fabric links. It answers the question a
// tcpdump-on-bridge capture can't: which endpoint SOURCED each frame.
//
// For each fabric link the server opens one Classifier over the link's two
// endpoint tap devices. The Classifier binds a raw AF_PACKET socket to each tap
// (Linux only) and, in a per-socket goroutine, reads just the leading bytes of
// every frame — enough for relay.ClassifyDetailed — and increments a counter
// keyed by (endpoint index, protocol label, subtype). A frame RECEIVED on a tap
// (sll_pkttype != PACKET_OUTGOING) is counted for that tap's direction; it is
// not, by itself, proof that the endpoint originated the source MAC because a
// bridge or switch may have forwarded it. The separate Attribution channel
// learns one source MAC, flips to ambiguous on a second, and never licenses a
// node name from an unproven frame. PACKET_OUTGOING is the peer's mirror and is
// not double counted.
//
// The counters are cumulative uint64s; the stats loop diffs two Snapshot()s the
// same way it diffs the netdev fps/bps counters, so this never drives emission
// on its own and can't regress the always-on glow. Close() tears the sockets
// and goroutines down; the server owns one Classifier per fabric link and closes
// them on lab.stop / link.remove / teardownFabric.
package dirstat

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// EndpointDev binds a tap device to the LAB DOCUMENT endpoint index it
// belongs to. The device list is sparse: a stopped or not-yet-attached
// endpoint contributes no device, so callers must not reconstruct the index
// from slice position.
type EndpointDev struct {
	Index int
	Dev   string
}

type attribState string

const (
	attribNone      attribState = "none"
	attribSingle    attribState = "single"
	attribAmbiguous attribState = "ambiguous"
)

const (
	macTTL      = 5 * time.Minute
	maxConflict = 32
)

var monotonicOrigin = time.Now()

func monotonicNow() int64 { return time.Since(monotonicOrigin).Nanoseconds() }

// attribCandidate is one endpoint's source-MAC attribution state. An endpoint
// is attributable ONLY in state single: a second distinct source MAC is
// evidence that the endpoint is forwarding for another device, so the first
// MAC is discarded rather than retained as a guess.
type attribCandidate struct {
	mac       [6]byte
	state     attribState
	firstSeen int64 // monotonic nanos
	lastSeen  int64 // monotonic nanos
}

type conflictMAC struct {
	mac      [6]byte
	lastSeen int64
}

// EndpointAttrib is a per-endpoint source-MAC attribution hint for one fabric
// link endpoint. EndpointIndex is explicit because the slice is sparse.
// State is the only thing that licenses naming a node: single has one MAC,
// ambiguous means the endpoint forwards for other devices, and none means no
// usable observation is currently available.
type EndpointAttrib struct {
	EndpointIndex int    `json:"endpointIndex"`
	State         string `json:"state"`
	MAC           string `json:"mac,omitempty"`
}

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
	c.ensureEndpointLocked(ep)
	c.counts[key{ep: ep, label: label, subtype: subtype}]++
	c.mu.Unlock()
}

// Classifier holds the counters shared by the (platform-specific) read
// goroutines. The socket/goroutine machinery lives in the _linux.go build; the
// _other.go stub leaves those fields zero and Open returns a nil *Classifier.
type Classifier struct {
	mu     sync.Mutex
	counts Counters
	// endpoints records the document indexes that had a usable tap when this
	// classifier was opened. It is separate from candidates so an endpoint
	// with no observations can still be reported as state "none".
	endpoints  map[int]struct{}
	candidates map[int]attribCandidate
	conflicts  []conflictMAC

	// closer/wg are populated only by the linux implementation; the stub never
	// sets them so its Close is a no-op.
	closer func()
	wg     *sync.WaitGroup
}

func newClassifier(devs []EndpointDev) *Classifier {
	c := &Classifier{
		counts:     make(Counters),
		endpoints:  make(map[int]struct{}),
		candidates: make(map[int]attribCandidate),
		wg:         &sync.WaitGroup{},
	}
	for _, d := range devs {
		if d.Index < 0 || d.Index > 1 || d.Dev == "" {
			continue
		}
		c.endpoints[d.Index] = struct{}{}
		c.candidates[d.Index] = attribCandidate{state: attribNone}
	}
	return c
}

func (c *Classifier) ensureMapsLocked() {
	if c.counts == nil {
		c.counts = make(Counters)
	}
	if c.endpoints == nil {
		c.endpoints = make(map[int]struct{})
	}
	if c.candidates == nil {
		c.candidates = make(map[int]attribCandidate)
	}
}

func (c *Classifier) ensureEndpointLocked(ep int) {
	c.ensureMapsLocked()
	if ep < 0 || ep > 1 {
		return
	}
	c.endpoints[ep] = struct{}{}
	if _, ok := c.candidates[ep]; !ok {
		c.candidates[ep] = attribCandidate{state: attribNone}
	}
}

func validSourceMAC(mac [6]byte) bool {
	if mac[0]&1 != 0 { // group/multicast source is not an endpoint identity
		return false
	}
	for _, b := range mac {
		if b != 0 {
			return true
		}
	}
	return false
}

func (c *Classifier) conflictIndexLocked(mac [6]byte) int {
	for i := range c.conflicts {
		if c.conflicts[i].mac == mac {
			return i
		}
	}
	return -1
}

func (c *Classifier) addConflictLocked(mac [6]byte, now int64) {
	if i := c.conflictIndexLocked(mac); i >= 0 {
		c.conflicts[i].lastSeen = now
		return
	}
	c.conflicts = append(c.conflicts, conflictMAC{mac: mac, lastSeen: now})
	if len(c.conflicts) > maxConflict {
		oldest := 0
		for i := 1; i < len(c.conflicts); i++ {
			if c.conflicts[i].lastSeen < c.conflicts[oldest].lastSeen {
				oldest = i
			}
		}
		copy(c.conflicts[oldest:], c.conflicts[oldest+1:])
		c.conflicts = c.conflicts[:len(c.conflicts)-1]
	}
}

func (c *Classifier) expireLocked(now int64) {
	for ep, candidate := range c.candidates {
		if candidate.state != attribNone && now-candidate.lastSeen > macTTL.Nanoseconds() {
			c.candidates[ep] = attribCandidate{state: attribNone}
		}
	}
	kept := c.conflicts[:0]
	for _, conflict := range c.conflicts {
		if now-conflict.lastSeen <= macTTL.Nanoseconds() {
			kept = append(kept, conflict)
		}
	}
	c.conflicts = kept
}

func (c *Classifier) clearCandidateLocked(ep int) {
	c.ensureEndpointLocked(ep)
	c.candidates[ep] = attribCandidate{state: attribNone}
}

// observeSource records one source MAC from an endpoint tap. Keeping this
// operation in the platform-independent file makes the learning lifecycle
// testable without requiring AF_PACKET privileges; Linux readLoop supplies
// the same bytes and a monotonic timestamp.
func (c *Classifier) observeSource(ep int, mac [6]byte, now int64) {
	if c == nil || !validSourceMAC(mac) || ep < 0 || ep > 1 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureEndpointLocked(ep)

	// A MAC currently attributed to the other endpoint is a conflict, not a
	// reason to choose one side. Drop the old owner and suppress the source on
	// this side while the bounded conflict entry is alive.
	other := 1 - ep
	otherCandidate, otherOK := c.candidates[other]
	if otherOK && otherCandidate.state == attribSingle && otherCandidate.mac == mac {
		c.addConflictLocked(mac, now)
		c.clearCandidateLocked(other)
		current := c.candidates[ep]
		switch current.state {
		case attribSingle:
			if current.mac == mac {
				c.clearCandidateLocked(ep)
			} else {
				current.state = attribAmbiguous
				current.mac = [6]byte{}
				current.lastSeen = now
				c.candidates[ep] = current
			}
		case attribAmbiguous:
			current.lastSeen = now
			c.candidates[ep] = current
		}
		return
	}

	// A conflicted MAC is never relearned while the conflict is alive. If it
	// was already this endpoint's candidate, remove it from attribution.
	if c.conflictIndexLocked(mac) >= 0 {
		current := c.candidates[ep]
		switch current.state {
		case attribSingle:
			if current.mac == mac {
				c.clearCandidateLocked(ep)
			}
		case attribAmbiguous:
			current.lastSeen = now
			c.candidates[ep] = current
		}
		return
	}

	current := c.candidates[ep]
	switch current.state {
	case attribNone:
		c.candidates[ep] = attribCandidate{
			mac: mac, state: attribSingle, firstSeen: now, lastSeen: now,
		}
	case attribSingle:
		if current.mac == mac {
			current.lastSeen = now
			c.candidates[ep] = current
		} else {
			// The first source may itself have been forwarded. Never retain it
			// once a second distinct source proves the endpoint is ambiguous.
			current.mac = [6]byte{}
			current.state = attribAmbiguous
			current.lastSeen = now
			c.candidates[ep] = current
		}
	case attribAmbiguous:
		current.lastSeen = now
		c.candidates[ep] = current
	}
}

// Attribution copies the current endpoint hints under the lock. Expiry is
// lazy: no timer goroutine is needed for a quiet link.
func (c *Classifier) Attribution() []EndpointAttrib {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureMapsLocked()
	c.expireLocked(monotonicNow())
	indexes := make([]int, 0, len(c.endpoints))
	for ep := range c.endpoints {
		indexes = append(indexes, ep)
	}
	sort.Ints(indexes)
	out := make([]EndpointAttrib, 0, len(indexes))
	for _, ep := range indexes {
		candidate := c.candidates[ep]
		entry := EndpointAttrib{EndpointIndex: ep, State: string(candidate.state)}
		if candidate.state == attribSingle && c.conflictIndexLocked(candidate.mac) < 0 {
			entry.MAC = formatMAC(candidate.mac)
		} else if candidate.state == attribSingle {
			entry.State = string(attribNone)
		}
		if entry.State == "" {
			entry.State = string(attribNone)
		}
		out = append(out, entry)
	}
	return out
}

func formatMAC(mac [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
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

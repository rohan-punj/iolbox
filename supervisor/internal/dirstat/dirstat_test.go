package dirstat

import (
	"testing"
)

func testMAC(last byte) [6]byte { return [6]byte{0x02, 0, 0, 0, 0, last} }

func attribFor(t *testing.T, c *Classifier, ep int) EndpointAttrib {
	t.Helper()
	for _, a := range c.Attribution() {
		if a.EndpointIndex == ep {
			return a
		}
	}
	t.Fatalf("no attribution for endpoint %d", ep)
	return EndpointAttrib{}
}

func TestAttributionSingularMACAndAmbiguity(t *testing.T) {
	c := newClassifier([]EndpointDev{{Index: 0, Dev: "tap0"}})
	now := monotonicNow()
	first := testMAC(1)
	second := testMAC(2)
	c.observeSource(0, first, now)
	if got := attribFor(t, c, 0); got.State != "single" || got.MAC != formatMAC(first) {
		t.Fatalf("one source MAC = %+v, want single/%s", got, formatMAC(first))
	}
	c.observeSource(0, first, now+1)
	c.mu.Lock()
	lastSeen := c.candidates[0].lastSeen
	c.mu.Unlock()
	if lastSeen != now+1 {
		t.Fatalf("same MAC did not refresh lastSeen: got %d want %d", lastSeen, now+1)
	}
	c.observeSource(0, second, now+2)
	if got := attribFor(t, c, 0); got.State != "ambiguous" || got.MAC != "" {
		t.Fatalf("two source MACs = %+v, want ambiguous with no MAC", got)
	}
}

func TestAttributionIgnoresInvalidSourceMACs(t *testing.T) {
	c := newClassifier([]EndpointDev{{Index: 0, Dev: "tap0"}})
	now := monotonicNow()
	c.observeSource(0, [6]byte{}, now)
	c.observeSource(0, [6]byte{1, 0, 0, 0, 0, 1}, now+1)
	if got := attribFor(t, c, 0); got.State != "none" {
		t.Fatalf("invalid source MACs changed state: %+v", got)
	}
}

func TestAttributionAgesAndRelearns(t *testing.T) {
	c := newClassifier([]EndpointDev{{Index: 0, Dev: "tap0"}})
	now := monotonicNow()
	first := testMAC(3)
	second := testMAC(4)
	c.observeSource(0, first, now)
	candidate := c.candidates[0]
	candidate.lastSeen = monotonicNow() - macTTL.Nanoseconds() - 1
	c.candidates[0] = candidate
	if got := attribFor(t, c, 0); got.State != "none" {
		t.Fatalf("aged candidate = %+v, want none", got)
	}
	c.observeSource(0, second, monotonicNow())
	if got := attribFor(t, c, 0); got.State != "single" || got.MAC != formatMAC(second) {
		t.Fatalf("relearned candidate = %+v, want single/%s", got, formatMAC(second))
	}

	c.observeSource(0, first, monotonicNow())
	c.observeSource(0, second, monotonicNow()+1)
	if got := attribFor(t, c, 0); got.State != "ambiguous" {
		t.Fatalf("ambiguous candidate before expiry = %+v", got)
	}
	candidate = c.candidates[0]
	candidate.lastSeen = monotonicNow() - macTTL.Nanoseconds() - 1
	c.candidates[0] = candidate
	if got := attribFor(t, c, 0); got.State != "none" {
		t.Fatalf("aged ambiguity = %+v, want none", got)
	}
	c.observeSource(0, second, monotonicNow())
	if got := attribFor(t, c, 0); got.State != "single" || got.MAC != formatMAC(second) {
		t.Fatalf("relearned ambiguity = %+v", got)
	}
}

func TestAttributionCrossEndpointConflictIsBoundedAndAges(t *testing.T) {
	c := newClassifier([]EndpointDev{{Index: 0, Dev: "tap0"}, {Index: 1, Dev: "tap1"}})
	now := monotonicNow()
	shared := testMAC(10)
	c.observeSource(0, shared, now)
	c.observeSource(1, shared, now+1)
	if got := attribFor(t, c, 0); got.State != "none" {
		t.Fatalf("old owner after conflict = %+v, want none", got)
	}
	if got := attribFor(t, c, 1); got.State != "none" {
		t.Fatalf("new side after conflict = %+v, want none", got)
	}

	for i := 0; i < maxConflict+1; i++ {
		mac := testMAC(byte(20 + i))
		c.observeSource(0, mac, now+int64(i+2))
		c.observeSource(1, mac, now+int64(i+3))
	}
	if len(c.conflicts) != maxConflict {
		t.Fatalf("conflict set length = %d, want %d", len(c.conflicts), maxConflict)
	}
	if c.conflictIndexLocked(shared) >= 0 {
		t.Fatalf("oldest conflict was not dropped")
	}
	for i := range c.conflicts {
		c.conflicts[i].lastSeen = monotonicNow() - macTTL.Nanoseconds() - 1
	}
	_ = c.Attribution()
	if len(c.conflicts) != 0 {
		t.Fatalf("aged conflicts remain: %d", len(c.conflicts))
	}
	c.observeSource(0, shared, monotonicNow())
	if got := attribFor(t, c, 0); got.State != "single" || got.MAC != formatMAC(shared) {
		t.Fatalf("post-conflict relearn = %+v", got)
	}
}

func TestEndpointIndexSurvivesSparseDeviceList(t *testing.T) {
	// This is the regression for the old compacted []string path: endpoint 0
	// contributes no device and endpoint 1 does. The indexed path must keep
	// both the counter key and wire attribution at document index 1.
	c := newClassifier([]EndpointDev{{Index: 1, Dev: "tap1"}})
	mac := testMAC(42)
	c.count(1, "ARP", "request")
	c.observeSource(1, mac, monotonicNow())
	if c.Snapshot()[key{ep: 1, label: "ARP", subtype: "request"}] != 1 {
		t.Fatalf("counter did not retain endpoint index 1: %#v", c.Snapshot())
	}
	attrs := c.Attribution()
	if len(attrs) != 1 || attrs[0].EndpointIndex != 1 || attrs[0].MAC != formatMAC(mac) {
		t.Fatalf("sparse endpoint attribution = %+v, want only endpoint 1", attrs)
	}
}

func TestAttributionNilClassifier(t *testing.T) {
	var c *Classifier
	if got := c.Attribution(); got != nil {
		t.Fatalf("nil Attribution = %#v, want nil", got)
	}
}

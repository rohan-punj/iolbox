package server

import (
	"testing"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

// twoIOLLab builds a document whose first IOL node has id 0, so its e0/0 static
// tap is named "iol1_0" — the deterministic, lab-unscoped name that every such
// lab produces. That collision is the whole of finding #9.
func twoIOLLab(id string) *lab.Lab {
	return &lab.Lab{
		ID:    id,
		Nodes: []lab.Node{{ID: 0, Kind: lab.KindIOL}, {ID: 1, Kind: lab.KindIOL}},
		Links: []lab.Link{{
			ID:        0,
			Endpoints: []lab.Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "e0/0"}},
		}},
	}
}

// TestUnrelatedLabsAllocateTheSameTapName pins the premise: two labs built
// independently, with no shared state, name the same kernel device.
func TestUnrelatedLabsAllocateTheSameTapName(t *testing.T) {
	a := computeStaticTaps(twoIOLLab("a"), 0)
	b := computeStaticTaps(twoIOLLab("b"), 0)
	ta, ok := tapForEndpoint(a, lab.Endpoint{Node: 0, Interface: "e0/0"})
	if !ok {
		t.Fatal("lab a has no static tap for node 0 e0/0")
	}
	tb, ok := tapForEndpoint(b, lab.Endpoint{Node: 0, Interface: "e0/0"})
	if !ok {
		t.Fatal("lab b has no static tap for node 0 e0/0")
	}
	if ta.tapName != tb.tapName || ta.tapName != "iol1_0" {
		t.Fatalf("tap names = %q / %q, want both %q", ta.tapName, tb.tapName, "iol1_0")
	}
	if ta.pseudo != tb.pseudo {
		t.Fatalf("pseudo instances = %d / %d, want both identical (deterministic base)", ta.pseudo, tb.pseudo)
	}
}

// TestClaimTapDisplacesForeignOwner covers the registry itself: claiming,
// identity-checked release, and cross-lab eviction closing the foreign pump and
// unhooking it from its own lab's bookkeeping.
func TestClaimTapDisplacesForeignOwner(t *testing.T) {
	older := newLoadedLab(twoIOLLab("older"), t.TempDir())
	newer := newLoadedLab(twoIOLLab("newer"), t.TempDir())

	closer := &testBridgeCloser{}
	old := &labBridge{netioPath: "/tmp/netio0/500", tapName: "iol1_0", closer: closer}
	older.tapBridges[old.netioPath] = old
	if _, displaced := claimTap(old.tapName, older, old); displaced {
		t.Fatal("first claim reported a displacement")
	}
	if _, displaced := claimTap(old.tapName, older, old); displaced {
		t.Fatal("re-claim by the same bridge reported a displacement")
	}
	if !tapClaimedByOtherLab("iol1_0", newer) {
		t.Fatal("tap claimed by older lab does not read as foreign to newer lab")
	}
	if tapClaimedByOtherLab("iol1_0", older) {
		t.Fatal("a lab's own claim read as foreign to itself")
	}
	if tapClaimedByOtherLab("iol2_0", newer) {
		t.Fatal("an unclaimed tap read as foreign; unowned devices must stay usable")
	}

	// The later lab starting its fabric must close the earlier lab's pump before
	// TUNSETIFF, and must drop it from the earlier lab's map too.
	evictForeignTapClaim("iol1_0", newer)
	if !closer.closed {
		t.Fatal("foreign pump was not closed before tap reuse")
	}
	older.mu.Lock()
	_, still := older.tapBridges[old.netioPath]
	older.mu.Unlock()
	if still {
		t.Fatal("evicted foreign pump left behind in its own lab's tapBridges")
	}
	if tapClaimedByOtherLab("iol1_0", newer) {
		t.Fatal("claim survived eviction")
	}

	// A late release from the displaced bridge must not un-register a successor.
	next := &labBridge{netioPath: "/tmp/netio0/501", tapName: "iol1_0", closer: &testBridgeCloser{}}
	claimTap(next.tapName, newer, next)
	releaseTap("iol1_0", old)
	if !tapClaimedByOtherLab("iol1_0", older) {
		t.Fatal("a stale bridge's release dropped the current owner's claim")
	}
	releaseTap("iol1_0", next)
	if tapClaimedByOtherLab("iol1_0", older) {
		t.Fatal("owner release did not drop the claim")
	}
}

// TestScheduledFaultTimerSkipsTapOwnedByAnotherLab is the finding-#9 regression
// proper, and it reproduces the mechanism deterministically — no -race, no
// full-package timing dependency.
//
// An earlier lab is still its own server's CURRENT lab (so s.isCurrentLab
// passes) and has a pending fault timer. A later, entirely unrelated lab has by
// then taken ownership of the same kernel tap name, exactly as computeStaticTaps
// guarantees it can. The timer must decline to touch the kernel.
func TestScheduledFaultTimerSkipsTapOwnedByAnotherLab(t *testing.T) {
	s := newTestServer()
	older := newLoadedLab(twoIOLLab("older"), t.TempDir())
	s.refreshFabric(older)
	s.mu.Lock()
	s.lab = older
	s.mu.Unlock()
	if !s.isCurrentLab(older) {
		t.Fatal("test setup: older lab is not the server's current lab")
	}

	// A completely different lab now owns iol1_0.
	newer := newLoadedLab(twoIOLLab("newer"), t.TempDir())
	s.refreshFabric(newer)
	owner := &labBridge{netioPath: "/tmp/netio0/500", tapName: "iol1_0", closer: &testBridgeCloser{}}
	newer.tapBridges[owner.netioPath] = owner
	claimTap(owner.tapName, newer, owner)
	defer releaseTap(owner.tapName, owner)

	skipped := make(chan string, 4)
	setFaultTimerSkipHook(func(_ int, tap string) { skipped <- tap })
	defer setFaultTimerSkipHook(nil)

	for _, tc := range []struct {
		name     string
		schedule func(*lab.LinkFault)
	}{
		{"activation", func(f *lab.LinkFault) { s.scheduleFaultActivation(older, 0, f, time.Millisecond, 0) }},
		{"expiry", func(f *lab.LinkFault) { s.scheduleFaultExpiry(older, 0, f, time.Millisecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := &lab.LinkFault{Down: true}
			older.mu.Lock()
			older.linkFaults[0] = activeFault{Fault: fault, Active: tc.name == "expiry"}
			delete(older.fabricLinks, 0)
			older.mu.Unlock()

			tc.schedule(fault)
			select {
			case tap := <-skipped:
				if tap != "iol1_0" {
					t.Fatalf("skipped tap = %q, want iol1_0", tap)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("scheduled fault callback did not decline to touch the foreign-owned tap")
			}

			older.mu.Lock()
			active := older.linkFaults[0].Active
			attached := older.fabricLinks[0]
			older.mu.Unlock()
			if active {
				t.Fatal("callback left the fault active after skipping the kernel work")
			}
			if attached {
				t.Fatal("callback realised fabric state for a link whose tap belongs to another lab")
			}
		})
	}
}

// TestScheduledFaultTimerCompletes is the regression for the non-reentrant
// self-deadlock found while writing the finding-#9 test: both timer callbacks
// called loadedLab.findLink (which takes ll.mu) from inside their own ll.mu
// section, so every scheduled fault hung its goroutine forever while holding
// ll.mu AND s.labMu — permanently wedging the server's whole control plane.
//
// The fault here is an impairment on a link whose endpoint tap devices do not
// exist, so reconcileLinkFault's existence filter yields no targets and the
// callback performs no privileged work on any platform.
func TestScheduledFaultTimerCompletes(t *testing.T) {
	s := newTestServer()
	ll := newLoadedLab(twoIOLLab("timer"), t.TempDir())
	s.refreshFabric(ll)
	s.mu.Lock()
	s.lab = ll
	s.mu.Unlock()

	fault := &lab.LinkFault{LossPct: 5}
	ll.mu.Lock()
	ll.linkFaults[0] = activeFault{Fault: fault}
	ll.mu.Unlock()
	s.scheduleFaultActivation(ll, 0, fault, time.Millisecond, 0)

	deadline := time.Now().Add(10 * time.Second)
	for {
		ll.mu.Lock()
		got := ll.linkFaults[0]
		ll.mu.Unlock()
		if got.Active && got.Timer == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled fault activation never completed (callback deadlocked on ll.mu)")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// The lock must be free afterwards, and the serialization lock too: a
	// deadlocked callback strands both.
	done := make(chan struct{})
	go func() {
		s.labMu.Lock()
		ll.mu.Lock()
		ll.mu.Unlock()
		s.labMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ll.mu / s.labMu still held after the fault callback returned")
	}
}

// TestScheduledFaultTimerActsOnItsOwnTaps is the over-broadness control: the
// guard must only fire for FOREIGN ownership. A lab that owns its taps, or that
// has never started its fabric at all (no claims anywhere), must be unaffected.
func TestScheduledFaultTimerActsOnItsOwnTaps(t *testing.T) {
	ll := newLoadedLab(twoIOLLab("own"), t.TempDir())
	s := newTestServer()
	s.refreshFabric(ll)
	l := &ll.doc.Links[0]

	if tap, foreign := linkTapClaimedElsewhere(ll, l); foreign {
		t.Fatalf("unclaimed taps read as foreign (%q); a never-started lab must behave as before", tap)
	}

	own := &labBridge{netioPath: "/tmp/netio0/500", tapName: "iol1_0", closer: &testBridgeCloser{}}
	ll.tapBridges[own.netioPath] = own
	claimTap(own.tapName, ll, own)
	defer releaseTap(own.tapName, own)
	if tap, foreign := linkTapClaimedElsewhere(ll, l); foreign {
		t.Fatalf("a lab's own tap %q read as foreign", tap)
	}

	// close() must give the name back so the next lab can take it cleanly.
	_ = own.close()
	if tapClaimedByOtherLab("iol1_0", newLoadedLab(twoIOLLab("next"), t.TempDir())) {
		t.Fatal("labBridge.close did not release the process-global tap claim")
	}
}

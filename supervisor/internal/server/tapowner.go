package server

import (
	"log"
	"sync"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

// ---------------------------------------------------------------------------
// Finding #9: kernel tap device names are a PROCESS-GLOBAL namespace.
//
// A static tap's name is fabric.TapName(netmap.InstanceID(nodeID), flatIndex),
// i.e. "iol<nodeID+1>_<port>" — derived from the LAB DOCUMENT alone. Every lab
// whose first IOL node has id 0 therefore names its first interface's tap
// "iol1_0", and the pseudo-instance counter behind the matching
// /tmp/netio<uid>/<pseudo> socket likewise restarts at netmap.PseudoInstanceBase
// for every fresh lab (fabric.go computeStaticTaps). Neither identity is scoped
// by lab id or by Go object identity.
//
// Finding #1's fix made startFabric evict a stale pump BY TAP NAME — but only
// within one *loadedLab's own tapBridges map. Two UNRELATED labs (two *Server
// instances in a test binary; two sequential labs whose teardown has not
// completed; a fault timer from an earlier lab that is still its own server's
// "current" lab) share the kernel namespace but share no map, so the eviction
// never sees the other side's pump and iouyap's TUNSETIFF fails with
// "device or resource busy".
//
// s.isCurrentLab is NOT sufficient protection: it only asks whether ll is still
// the lab of the server that scheduled the callback. A timer belonging to an
// older, still-"current"-to-its-own-server lab passes that check and then
// operates on tap/bridge names another lab now owns.
//
// This registry is the missing scope: one process-wide map from kernel tap
// device name to the {lab, pump} that currently owns it. It is deliberately
// small — a claim, a release, a foreign-owner eviction, and an ownership query
// — and is the only cross-lab state in the package.
// ---------------------------------------------------------------------------

// tapClaim is the current owner of one kernel tap device name.
type tapClaim struct {
	lab    *loadedLab
	bridge *labBridge
}

var tapOwners = struct {
	mu sync.Mutex
	m  map[string]tapClaim
}{m: make(map[string]tapClaim)}

// claimTap records ll/bridge as the owner of the kernel tap device name. It
// returns the claim it displaced (and true) when the name was previously held
// by a DIFFERENT bridge, so callers can act on the displacement; re-claiming
// with the same bridge reports no displacement.
func claimTap(name string, ll *loadedLab, bridge *labBridge) (tapClaim, bool) {
	if name == "" {
		return tapClaim{}, false
	}
	tapOwners.mu.Lock()
	prev, had := tapOwners.m[name]
	tapOwners.m[name] = tapClaim{lab: ll, bridge: bridge}
	tapOwners.mu.Unlock()
	if !had || prev.bridge == bridge {
		return tapClaim{}, false
	}
	return prev, true
}

// releaseTap drops name's claim iff bridge still holds it. Identity-checked so
// a late close of an already-displaced pump cannot un-register its successor.
// Called from labBridge.close, which every pump-removal path funnels through.
func releaseTap(name string, bridge *labBridge) {
	if name == "" {
		return
	}
	tapOwners.mu.Lock()
	if cur, ok := tapOwners.m[name]; ok && cur.bridge == bridge {
		delete(tapOwners.m, name)
	}
	tapOwners.mu.Unlock()
}

// tapClaimedByOtherLab reports whether the named kernel tap is currently owned
// by a lab other than ll. An UNOWNED tap reports false: a lab that has never
// started its fabric holds no claims, and must keep behaving exactly as before.
func tapClaimedByOtherLab(name string, ll *loadedLab) bool {
	if name == "" {
		return false
	}
	tapOwners.mu.Lock()
	defer tapOwners.mu.Unlock()
	cur, ok := tapOwners.m[name]
	return ok && cur.lab != ll
}

// evictForeignTapClaim closes and unregisters any pump from a DIFFERENT lab
// that still owns the named kernel tap, so the caller can open a fresh pump on
// it without hitting TUNSETIFF EBUSY. This is finding #1's by-identity eviction
// widened from per-lab to per-process; a pump owned by ll itself is left alone
// (startFabric's own skip/ghost logic already reasons about those).
//
// Lock discipline: the registry lock is released before the other lab's mu is
// taken and before Close runs, so no lock is ever held across another lab's
// lock or across a blocking close.
func evictForeignTapClaim(name string, ll *loadedLab) {
	if name == "" {
		return
	}
	tapOwners.mu.Lock()
	cur, ok := tapOwners.m[name]
	if !ok || cur.lab == ll {
		tapOwners.mu.Unlock()
		return
	}
	delete(tapOwners.m, name)
	tapOwners.mu.Unlock()
	if cur.bridge == nil {
		return
	}
	if cur.lab != nil {
		cur.lab.mu.Lock()
		if held, ok := cur.lab.tapBridges[cur.bridge.netioPath]; ok && held == cur.bridge {
			delete(cur.lab.tapBridges, cur.bridge.netioPath)
		}
		cur.lab.mu.Unlock()
	}
	log.Printf("fabric: tap %s was still owned by another lab's pump (%s); closing it before reuse",
		name, cur.bridge.netioPath)
	_ = cur.bridge.close()
}

// linkTapClaimedElsewhere reports the first of a link's IOL endpoint taps that
// is currently owned by a lab other than ll. Scheduled fault callbacks use it
// to refuse kernel work on devices that no longer belong to them: the tap and
// bridge names they would touch are global, so s.isCurrentLab alone cannot tell
// them whether the devices behind those names are still theirs.
func linkTapClaimedElsewhere(ll *loadedLab, l *lab.Link) (string, bool) {
	taps := ll.staticTapsSnapshot()
	for _, ep := range l.Endpoints {
		t, ok := tapForEndpoint(taps, ep)
		if !ok {
			continue
		}
		if tapClaimedByOtherLab(t.tapName, ll) {
			return t.tapName, true
		}
	}
	return "", false
}

var (
	skipHookMu         sync.Mutex
	faultTimerSkipHook func(linkID int, tap string)
)

// setFaultTimerSkipHook installs an observation point for the "scheduled fault
// callback declined to touch a foreign-owned tap" decision. Test-only; the
// production value is nil.
func setFaultTimerSkipHook(fn func(linkID int, tap string)) {
	skipHookMu.Lock()
	faultTimerSkipHook = fn
	skipHookMu.Unlock()
}

func notifyFaultTimerSkip(linkID int, tap string) {
	skipHookMu.Lock()
	fn := faultTimerSkipHook
	skipHookMu.Unlock()
	if fn != nil {
		fn(linkID, tap)
	}
}

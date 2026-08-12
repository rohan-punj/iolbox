package server

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/fabric"
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// endpointDev keeps the identity from the lab document attached to the
// compacted set of currently discoverable host devices. Never use the slice
// position as endpoint identity: stopped VPCS/tool endpoints are omitted.
type endpointDev struct {
	EndpointIndex int
	Dev           string
}

func cloneLinkFault(in *lab.LinkFault) *lab.LinkFault {
	if in == nil {
		return nil
	}
	out := *in
	if in.TargetEndpoint != nil {
		v := *in.TargetEndpoint
		out.TargetEndpoint = &v
	}
	return &out
}

func faultHasImpairment(f *lab.LinkFault) bool {
	return f != nil && (f.DelayMs > 0 || f.JitterMs > 0 || f.LossPct > 0 ||
		f.RateKbit > 0 || f.DuplicatePct > 0 || f.ReorderPct > 0)
}

func hasAtMostTwoDecimals(v float64) bool {
	return math.Abs(v-math.Round(v*100)/100) < 1e-9
}

// validateLinkFault is the single API-side validation for the netem model.
// Keeping it separate from tc means malformed values never reach a privileged
// command and can be tested on any OS.
func validateLinkFault(f *lab.LinkFault, endpointCount int) error {
	if f == nil {
		return nil
	}
	if endpointCount < 2 {
		return fmt.Errorf("link must have at least two endpoints")
	}
	if f.TargetEndpoint != nil && (*f.TargetEndpoint < 0 || *f.TargetEndpoint >= endpointCount) {
		return fmt.Errorf("targetEndpoint must be an index from 0 to %d (link has %d endpoints)", endpointCount-1, endpointCount)
	}
	values := []struct {
		name string
		v    float64
		min  float64
		max  float64
	}{
		{"delayMs", f.DelayMs, 0, 10000},
		{"jitterMs", f.JitterMs, 0, 10000},
		{"lossPct", f.LossPct, 0, 100},
		{"duplicatePct", f.DuplicatePct, 0, 100},
		{"reorderPct", f.ReorderPct, 0, 100},
	}
	for _, item := range values {
		if item.v < item.min || item.v > item.max || !hasAtMostTwoDecimals(item.v) {
			return fmt.Errorf("%s must be between %v and %v with at most two decimals", item.name, item.min, item.max)
		}
	}
	if f.RateKbit != 0 && (f.RateKbit < 1 || f.RateKbit > 10_000_000) {
		return fmt.Errorf("rateKbit must be an integer from 1 to 10000000")
	}
	if f.JitterMs > 0 && f.DelayMs <= 0 {
		return fmt.Errorf("jitterMs requires delayMs greater than zero")
	}
	if f.ReorderPct > 0 && f.DelayMs <= 0 {
		return fmt.Errorf("reorderPct requires delayMs greater than zero")
	}
	if f.Down && faultHasImpairment(f) {
		return fmt.Errorf("down is mutually exclusive with impairment fields")
	}
	if !f.Down && !faultHasImpairment(f) {
		return fmt.Errorf("fault must set down or at least one impairment field")
	}
	return nil
}

func faultNetem(f *lab.LinkFault) fabric.Netem {
	return fabric.Netem{
		DelayMs:      f.DelayMs,
		JitterMs:     f.JitterMs,
		LossPct:      f.LossPct,
		RateKbit:     f.RateKbit,
		DuplicatePct: f.DuplicatePct,
		ReorderPct:   f.ReorderPct,
	}
}

func faultTargetsEndpoint(f *lab.LinkFault, endpointIndex int) bool {
	return f == nil || f.TargetEndpoint == nil || *f.TargetEndpoint == endpointIndex
}

func cancelFaultTimerLocked(ll *loadedLab, linkID int) {
	f, ok := ll.linkFaults[linkID]
	if !ok || f.Timer == nil {
		return
	}
	f.Timer.Stop()
	f.Timer = nil
	ll.linkFaults[linkID] = f
}

func (s *Server) cancelFaultTimer(ll *loadedLab, linkID int) {
	ll.mu.Lock()
	cancelFaultTimerLocked(ll, linkID)
	ll.mu.Unlock()
}

func (s *Server) cancelFaultTimersForNode(ll *loadedLab, nodeID int) {
	ll.mu.Lock()
	for _, l := range ll.doc.Links {
		for _, ep := range l.Endpoints {
			if ep.Node == nodeID {
				cancelFaultTimerLocked(ll, l.ID)
				break
			}
		}
	}
	ll.mu.Unlock()
}

func (s *Server) emitLinkFault(ll *loadedLab, linkID int, reason string) {
	f, ok := ll.faultForLink(linkID)
	if !ok {
		return
	}
	s.emit(protocol.EventLinkFault, protocol.LinkFaultData{
		Link:   linkID,
		Fault:  cloneLinkFault(f.Fault),
		Active: f.Active,
		Reason: reason,
	})
}

func (ll *loadedLab) activateInitialFaults() []int {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	var activated []int
	for id, f := range ll.linkFaults {
		if f.Fault == nil || !f.Fault.Initial || f.Active {
			continue
		}
		f.Active = true
		ll.linkFaults[id] = f
		activated = append(activated, id)
	}
	return activated
}

func (s *Server) linkFaultSupported(ll *loadedLab, l *lab.Link) (bool, string) {
	if !isFabricLink(l, fabricNodes(ll.doc)) {
		return false, "link is not an Ethernet-realizable fabric link"
	}
	taps := ll.staticTaps
	if len(taps) == 0 {
		// Fault definitions may be edited after lab.load but before the first
		// node.start, while the normal static-tap refresh has not run yet.
		taps = computeStaticTaps(ll.doc, currentUID())
	}
	for _, ep := range l.Endpoints {
		n := ll.findNode(ep.Node)
		if n == nil {
			return false, fmt.Sprintf("link endpoint references unknown node %d", ep.Node)
		}
		if n.Kind == lab.KindIOL {
			if _, ok := tapForEndpoint(taps, ep); !ok {
				return false, fmt.Sprintf("node %d interface %s has no Ethernet static tap", ep.Node, ep.Interface)
			}
		}
	}
	return true, ""
}

func (s *Server) scheduleFaultExpiry(ll *loadedLab, linkID int, fault *lab.LinkFault, after time.Duration) {
	if after <= 0 {
		return
	}
	t := time.AfterFunc(after, func() {
		ll.mu.Lock()
		current, ok := ll.linkFaults[linkID]
		if !ok || current.Fault != fault {
			ll.mu.Unlock()
			return
		}
		current.Timer = nil
		current.Active = false
		ll.linkFaults[linkID] = current
		link := ll.findLink(linkID)
		ll.mu.Unlock()
		if link == nil {
			return
		}
		if err := s.clearFaultEffect(ll, link); err != nil {
			log.Printf("fabric: link %d: scheduled fault restore: %v", linkID, err)
			s.emitLinkFault(ll, linkID, err.Error())
			return
		}
		s.emitLinkFault(ll, linkID, "restored")
	})
	ll.mu.Lock()
	current, ok := ll.linkFaults[linkID]
	if ok && current.Fault == fault {
		current.Timer = t
		ll.linkFaults[linkID] = current
	} else {
		t.Stop()
	}
	ll.mu.Unlock()
}

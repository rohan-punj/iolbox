package server

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

func (s *Server) applyActiveFault(ll *loadedLab, l *lab.Link) error {
	f, ok := ll.faultForLink(l.ID)
	if !ok || f.Fault == nil || !f.Active {
		return nil
	}
	if f.Fault.Down {
		return s.reconcileFabricLinkDown(ll, l, f)
	}
	return s.reconcileLinkFault(ll, l)
}

func (s *Server) clearFaultEffect(ll *loadedLab, l *lab.Link) error {
	f, ok := ll.faultForLink(l.ID)
	if ok && f.Fault != nil && f.Fault.Down {
		// Clearing admin-down is admin-up: use the ordinary fabric attach path so
		// the bridge is ensured and every currently-present endpoint is restored.
		return s.attachFabricLink(ll, l)
	}
	return s.clearLinkNetem(ll, l)
}

func (s *Server) handleLinkSetFault(raw json.RawMessage) (any, error) {
	var args protocol.LinkFaultArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	l := ll.findLink(args.Link)
	if l == nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "unknown link %d", args.Link)
	}
	// The support gate deliberately runs before every other validation and
	// before any state mutation, so serial endpoints never get a partial fault.
	if ok, reason := s.linkFaultSupported(ll, l); !ok {
		return nil, protocol.NewError(protocol.CodeUnsupported, reason)
	}
	if args.AfterSec < 0 || args.ForSec < 0 {
		return nil, protocol.NewError(protocol.CodeBadRequest, "afterSec and forSec must be non-negative")
	}
	if args.Fault == nil && args.ForSec > 0 {
		return nil, protocol.NewError(protocol.CodeBadRequest, "forSec requires a fault")
	}
	if err := validateLinkFault(args.Fault, len(l.Endpoints)); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "%v", err)
	}

	old, hadOld := ll.faultForLink(l.ID)
	if hadOld {
		s.cancelFaultTimer(ll, l.ID)
	}
	oldDocFault := cloneLinkFault(l.Fault)
	newFault := cloneLinkFault(args.Fault)

	// Remove the prior live effect before scheduling/replacing it. This ensures
	// a targeted edit also clears qdiscs from devices that leave the target set.
	if hadOld && old.Active {
		if err := s.clearFaultEffect(ll, l); err != nil {
			return nil, err
		}
	}

	ll.setLinkFault(l.ID, newFault)
	if newFault == nil {
		ll.mu.Lock()
		delete(ll.linkFaults, l.ID)
		ll.mu.Unlock()
		out := protocol.LinkFaultData{Link: l.ID, Active: false, Reason: "cleared"}
		s.emit(protocol.EventLinkFault, out)
		return out, nil
	}

	// Install the new definition with a fresh pointer. Timer callbacks compare
	// this pointer, preventing an old scheduled callback from mutating a newer
	// fault for the same link.
	current := activeFault{Fault: newFault, Active: args.AfterSec <= 0}
	ll.mu.Lock()
	ll.linkFaults[l.ID] = current
	ll.mu.Unlock()

	if current.Active {
		if err := s.applyActiveFault(ll, l); err != nil {
			// Restore the prior document/runtime definition if tc or bridge
			// realization rejected the new request.
			ll.setLinkFault(l.ID, oldDocFault)
			ll.mu.Lock()
			if hadOld {
				ll.linkFaults[l.ID] = old
			} else {
				delete(ll.linkFaults, l.ID)
			}
			ll.mu.Unlock()
			if hadOld && old.Active {
				_ = s.applyActiveFault(ll, l)
			}
			return nil, err
		}
	} else {
		s.scheduleFaultActivation(ll, l.ID, newFault, time.Duration(args.AfterSec*float64(time.Second)), args.ForSec)
	}

	if args.ForSec > 0 && current.Active {
		s.scheduleFaultExpiry(ll, l.ID, newFault, time.Duration(args.ForSec*float64(time.Second)))
	}
	reason := "applied"
	if !current.Active {
		reason = fmt.Sprintf("scheduled in %.3g seconds", args.AfterSec)
	}
	out := protocol.LinkFaultData{Link: l.ID, Fault: cloneLinkFault(newFault), Active: current.Active, Reason: reason}
	s.emit(protocol.EventLinkFault, out)
	return out, nil
}

func (s *Server) scheduleFaultActivation(ll *loadedLab, linkID int, fault *lab.LinkFault, after time.Duration, forSec float64) {
	t := time.AfterFunc(after, func() {
		s.labMu.Lock()
		defer s.labMu.Unlock()
		if !s.isCurrentLab(ll) {
			return
		}
		ll.mu.Lock()
		current, ok := ll.linkFaults[linkID]
		if !ok || current.Fault != fault {
			ll.mu.Unlock()
			return
		}
		current.Active = true
		current.Timer = nil
		ll.linkFaults[linkID] = current
		ll.mu.Unlock()
		// findLink takes ll.mu itself; calling it inside the section above was a
		// non-reentrant self-deadlock that stranded ll.mu and s.labMu forever.
		link := ll.findLink(linkID)
		if link == nil {
			return
		}
		// Same process-global device-namespace guard as scheduleFaultExpiry
		// (finding #9): applyActiveFault reaches EnsureBridge/Attach/Detach/tc on
		// names another lab may now own. Roll our own activation back rather than
		// impairing a device that is not ours.
		if tap, foreign := linkTapClaimedElsewhere(ll, link); foreign {
			ll.mu.Lock()
			if current, ok := ll.linkFaults[linkID]; ok && current.Fault == fault {
				current.Active = false
				ll.linkFaults[linkID] = current
			}
			ll.mu.Unlock()
			log.Printf("fabric: link %d: scheduled fault activation skipped: tap %s is owned by another lab", linkID, tap)
			notifyFaultTimerSkip(linkID, tap)
			s.emitLinkFault(ll, linkID, "skipped: link devices are owned by another lab")
			return
		}
		if err := s.applyActiveFault(ll, link); err != nil {
			ll.mu.Lock()
			current, ok := ll.linkFaults[linkID]
			if ok && current.Fault == fault {
				current.Active = false
				ll.linkFaults[linkID] = current
			}
			ll.mu.Unlock()
			s.emitLinkFault(ll, linkID, err.Error())
			return
		}
		s.emitLinkFault(ll, linkID, "applied")
		if forSec > 0 {
			s.scheduleFaultExpiry(ll, linkID, fault, time.Duration(forSec*float64(time.Second)))
		}
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

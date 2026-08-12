//go:build !linux

package server

import (
	"strconv"

	"github.com/rohanpunj/iolbox/supervisor/internal/dirstat"
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// The static-tap fabric is Linux-only (taps + bridges). On other platforms the
// control plane still computes ll.staticTaps (so the NETMAP and tests match), but
// no kernel objects are created; these are no-ops.

func (s *Server) startFabric(ll *loadedLab, ids []int) error          { ll.activateInitialFaults(); return nil }
func (s *Server) attachFabricLink(ll *loadedLab, l *lab.Link) error   { return nil }
func (s *Server) attachFabricForNode(ll *loadedLab, nodeID int) error { return nil }
func (s *Server) detachFabricLink(ll *loadedLab, l *lab.Link)         {}
func (s *Server) teardownFabric(ll *loadedLab) {
	ll.mu.Lock()
	defer ll.mu.Unlock()
	for id, f := range ll.linkFaults {
		f.Active = false
		f.Timer = nil
		ll.linkFaults[id] = f
	}
}

func tapDeviceExists(name string) bool { return false }

func (s *Server) fabricLinkTapDevs(ll *loadedLab, l *lab.Link) []string {
	indexed := s.fabricLinkEndpointDevs(ll, l)
	devs := make([]string, 0, len(indexed))
	for _, e := range indexed {
		devs = append(devs, e.Dev)
	}
	return devs
}

func (s *Server) fabricLinkEndpointDevs(ll *loadedLab, l *lab.Link) []endpointDev {
	var out []endpointDev
	for endpointIndex, ep := range l.Endpoints {
		n := ll.findNode(ep.Node)
		switch {
		case n != nil && n.Kind == lab.KindVPCS:
			if nr := ll.get(ep.Node); nr != nil && nr.vtapName != "" {
				out = append(out, endpointDev{EndpointIndex: endpointIndex, Dev: nr.vtapName})
			}
		case n != nil && n.Kind == lab.KindNAT:
			out = append(out, endpointDev{EndpointIndex: endpointIndex, Dev: "iolnat" + strconv.Itoa(ep.Node)})
		case n != nil && (n.Kind == lab.KindTool || n.Kind == lab.KindPC):
			if nr := ll.get(ep.Node); nr != nil && nr.tool != nil {
				out = append(out, endpointDev{EndpointIndex: endpointIndex, Dev: tool.HostVethName(ep.Node)})
			}
		default:
			if t, ok := tapForEndpoint(ll.staticTaps, ep); ok {
				out = append(out, endpointDev{EndpointIndex: endpointIndex, Dev: t.tapName})
			}
		}
	}
	return out
}

func (s *Server) fabricLinkFaultTargets(ll *loadedLab, l *lab.Link) []endpointDev {
	return nil
}

func (s *Server) reconcileLinkFault(ll *loadedLab, l *lab.Link) error { return nil }
func (s *Server) reconcileFabricLinkDown(ll *loadedLab, l *lab.Link, f activeFault) error {
	ll.mu.Lock()
	ll.fabricLinks[l.ID] = true
	ll.mu.Unlock()
	return nil
}
func (s *Server) clearLinkNetem(ll *loadedLab, l *lab.Link) error { return nil }

func (s *Server) setupVPCSFabric(ll *loadedLab, nr *nodeRuntime, n *lab.Node) error { return nil }
func (s *Server) teardownVPCS(nr *nodeRuntime)                                      {}

func (s *Server) startBridgeCapture(ll *loadedLab, linkID, port int) (int, error) { return port, nil }
func (s *Server) stopBridgeCapture(ll *loadedLab, linkID int)                     {}

// fabStat mirrors the linux type so statsLoop compiles on any OS.
type fabStat struct {
	frames uint64
	bytes  uint64
	protos map[string]uint64
	dir    dirstat.Counters
	attrib []dirstat.EndpointAttrib
}

func (s *Server) fabricStats(ll *loadedLab) map[int]fabStat { return nil }

//go:build !linux

package server

import (
	"github.com/rohanpunj/iolbox/supervisor/internal/dirstat"
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

// The static-tap fabric is Linux-only (taps + bridges). On other platforms the
// control plane still computes ll.staticTaps (so the NETMAP and tests match), but
// no kernel objects are created; these are no-ops.

func (s *Server) startFabric(ll *loadedLab) error                     { return nil }
func (s *Server) attachFabricLink(ll *loadedLab, l *lab.Link) error   { return nil }
func (s *Server) attachFabricForNode(ll *loadedLab, nodeID int) error { return nil }
func (s *Server) detachFabricLink(ll *loadedLab, l *lab.Link)         {}
func (s *Server) teardownFabric(ll *loadedLab)                        {}

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
}

func (s *Server) fabricStats(ll *loadedLab) map[int]fabStat { return nil }

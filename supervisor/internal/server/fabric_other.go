//go:build !linux

package server

import "github.com/rohanpunj/iolab/supervisor/internal/lab"

// The static-tap fabric is Linux-only (taps + bridges). On other platforms the
// control plane still computes ll.staticTaps (so the NETMAP and tests match), but
// no kernel objects are created; these are no-ops.

func (s *Server) startFabric(ll *loadedLab) error                   { return nil }
func (s *Server) attachFabricLink(ll *loadedLab, l *lab.Link) error { return nil }
func (s *Server) detachFabricLink(ll *loadedLab, l *lab.Link)       {}
func (s *Server) teardownFabric(ll *loadedLab)                      {}

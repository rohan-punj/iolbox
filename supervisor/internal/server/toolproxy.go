package server

import (
	"encoding/json"

	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// ToolProxyTarget returns the running tool GUI's AF_UNIX socket and the
// validated manifest route allowlist for nodeID. The endpoint owns the actual
// socket filename; the server deliberately obtains it through SocketPath
// rather than reconstructing an implementation detail.
func (s *Server) ToolProxyTarget(nodeID int) (socket string, routes []tool.ProxyRoute, ok bool) {
	s.mu.Lock()
	ll := s.lab
	s.mu.Unlock()
	if ll == nil {
		return "", nil, false
	}
	nr := ll.get(nodeID)
	if nr == nil || nr.tool == nil || nr.tool.State() != "running" {
		return "", nil, false
	}
	n := ll.findNode(nodeID)
	if n == nil {
		return "", nil, false
	}
	var packID string
	if err := json.Unmarshal(n.Config["pack"], &packID); err != nil {
		return "", nil, false
	}
	pack, found := s.toolPack(packID)
	if !found {
		return "", nil, false
	}
	socket = nr.tool.SocketPath()
	if socket == "" {
		return "", nil, false
	}
	routes = append([]tool.ProxyRoute(nil), pack.Manifest.GUI.ProxyRoutes...)
	return socket, routes, true
}

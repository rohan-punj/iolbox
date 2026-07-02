//go:build !linux

package server

// currentUID returns a fixed, deterministic uid off Linux so bridge-plan unit
// tests produce stable netio socket paths (/tmp/netio1000/<instance>) without
// depending on the host OS. The real value (os.Getuid) is used only on Linux,
// where the netio dir must match IOL's own /tmp/netio<uid> (bridgeplan_linux.go).
func currentUID() int { return 1000 }

// startBridges is a no-op off Linux: there is no IOL to connect to a netio
// socket, so no iouyap bridge is started. The pure plan (bridgeplan.go) is still
// built and unit-tested. On Linux the real implementation starts one iouyap
// bridge per bridged IOL endpoint.
func (s *Server) startBridges(ll *loadedLab) error { return nil }

// stopBridges is a no-op off Linux (no live bridges exist there).
func (s *Server) stopBridges(ll *loadedLab) {}

//go:build !linux

package server

// extractNVRAM is a stub off Linux (no IOL NVRAM files exist there). It returns
// an empty config so control-plane tests can exercise config.extract wiring.
func (s *Server) extractNVRAM(ll *loadedLab, id int) (string, error) {
	return "", nil
}

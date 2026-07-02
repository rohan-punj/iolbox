//go:build !linux

package server

// prepareLabDir is a stub off Linux: there is no IOL to spawn, so no shared lab
// dir, NETMAP, iourc, or NVRAM files are written. Control-plane tests exercise
// lab.load/start wiring without touching the filesystem. On Linux the real
// implementation (nvram_linux.go) writes the whole-lab artifacts before spawn.
func (s *Server) prepareLabDir(ll *loadedLab) error {
	return nil
}

// extractNVRAM is a stub off Linux (no IOL NVRAM files exist there). It returns
// an empty config so control-plane tests can exercise config.extract wiring.
func (s *Server) extractNVRAM(ll *loadedLab, id int) (string, error) {
	return "", nil
}

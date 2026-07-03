//go:build !linux

package server

// hostReader is a non-Linux stub: the runtime is always Linux, so the dev-box
// build reports zeros rather than reading /proc.
type hostReader struct{}

func newHostReader(string) *hostReader { return &hostReader{} }

func (h *hostReader) read(int) (cpuPct float64, memUsed, memTotal, diskUsed, diskTotal uint64) {
	return 0, 0, 0, 0, 0
}

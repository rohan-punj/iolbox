//go:build linux

package server

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// cpuSample is one reading of aggregate CPU busy/total jiffies from
// /proc/stat's first "cpu" line. A percentage needs two samples (the busy
// delta over the total delta), which readHostStats keeps between ticks.
type cpuSample struct {
	busy, total uint64
}

// readCPUSample parses the aggregate "cpu" line of /proc/stat into busy/total
// jiffies. Fields: user nice system idle iowait irq softirq steal ... — busy
// is everything except idle+iowait.
func readCPUSample() (cpuSample, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var total, idle uint64
		for i, f := range fields {
			v, _ := strconv.ParseUint(f, 10, 64)
			total += v
			if i == 3 || i == 4 { // idle, iowait
				idle += v
			}
		}
		return cpuSample{busy: total - idle, total: total}, true
	}
	return cpuSample{}, false
}

// readMem returns used/total memory in bytes from /proc/meminfo, using
// MemTotal - MemAvailable for "used" (the figure that reflects real pressure,
// not counting reclaimable cache).
func readMem() (used, total uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var memTotal, memAvail uint64
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		kb, _ := strconv.ParseUint(f[1], 10, 64) // values are in kB
		switch f[0] {
		case "MemTotal:":
			memTotal = kb * 1024
		case "MemAvailable:":
			memAvail = kb * 1024
		}
	}
	if memAvail > memTotal {
		memAvail = memTotal
	}
	return memTotal - memAvail, memTotal
}

// readDisk returns used/total bytes of the filesystem holding path (the
// run-dir, where node working files and images live).
func readDisk(path string) (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bs := uint64(st.Bsize)
	total = st.Blocks * bs
	free := st.Bavail * bs
	if free > total {
		free = total
	}
	return total - free, total
}

// hostReader samples the runtime's CPU/RAM/disk. It holds the previous CPU
// jiffy sample so consecutive reads yield a busy percentage.
type hostReader struct {
	prev    cpuSample
	havePr  bool
	diskDir string
}

func newHostReader(diskDir string) *hostReader { return &hostReader{diskDir: diskDir} }

// read returns the current host stats and whether the reading is usable. The
// first call has no prior CPU sample, so CPUPct is 0 until the second call.
func (h *hostReader) read(cores int) (cpuPct float64, memUsed, memTotal, diskUsed, diskTotal uint64) {
	if s, ok := readCPUSample(); ok {
		if h.havePr && s.total > h.prev.total {
			dTotal := s.total - h.prev.total
			dBusy := s.busy - h.prev.busy
			if s.busy < h.prev.busy {
				dBusy = 0
			}
			cpuPct = float64(dBusy) / float64(dTotal) * 100
		}
		h.prev = s
		h.havePr = true
	}
	memUsed, memTotal = readMem()
	diskUsed, diskTotal = readDisk(h.diskDir)
	return cpuPct, memUsed, memTotal, diskUsed, diskTotal
}

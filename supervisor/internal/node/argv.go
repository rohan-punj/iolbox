package node

import (
	"fmt"
	"strconv"
)

// Spec is the platform-independent description of how to launch a node. It is
// built from a lab node + allocated resources, then handed to the linux spawner.
type Spec struct {
	// NodeID is the lab node id (also the IOL instance id passed as the last argv).
	NodeID int
	Kind   string // "iol" | "vpcs"

	// IOL fields.
	ImagePath   string // absolute path to the IOL ELF binary
	WorkDir     string // working directory (for IOL: the SHARED lab dir with NETMAP+iourc+nvram)
	Ethernet    int    // ethernet adapter groups
	Serial      int    // serial adapter groups
	RAM         int    // megabytes
	ConsolePort int    // telnet console TCP port (the supervisor's pty->telnet bridge binds this)
	NVRAMKiB    int    // IOL NVRAM size in KiB (-n); 0 uses DefaultNVRAMKiB

	// VPCS fields.
	VPCSCount int // number of PCs (<= 9)
}

// DefaultNVRAMKiB is the NVRAM size used when Spec.NVRAMKiB is 0. IOL rounds
// this and it must comfortably exceed the injected startup-config; 64 KiB fits
// typical lab configs. See NVRAMKiBFor for config-sized growth.
const DefaultNVRAMKiB = 64

// IOLArgv builds the argv for launching an IOL instance.
//
// IOL argv shape (community convention):
//
//	<image> [-e <eth groups>] [-s <serial groups>] -n <nvram KiB> <instance-id>
//
// The instance id is the last positional argument and matches the NETMAP node
// id. We deliberately DO NOT pass "-l": the keepalive flag causes a 100% idle
// CPU spin on this kernel (see PLAN.md).
//
// The IOL binary reads NETMAP, the iourc license, and its NVRAM file from its
// current working directory (its cwd = the SHARED lab dir; the spawner sets it),
// so those files must already be there. There is NO console argv flag and NO
// console env var: P0 confirmed real IOL uses stdin/stdout on its controlling
// pty for the console and opens no TCP port of its own. The supervisor allocates
// a pty, runs IOL attached to it, and bridges that pty to ConsolePort (see
// spawn_linux.go). The -n size is honoured for the injected NVRAM config.
func (s Spec) IOLArgv() []string {
	argv := []string{s.ImagePath}
	if s.Ethernet > 0 {
		argv = append(argv, "-e", strconv.Itoa(s.Ethernet))
	}
	if s.Serial > 0 {
		argv = append(argv, "-s", strconv.Itoa(s.Serial))
	}
	nvKiB := s.NVRAMKiB
	if nvKiB <= 0 {
		nvKiB = DefaultNVRAMKiB
	}
	argv = append(argv, "-n", strconv.Itoa(nvKiB))
	// NOTE: no "-l" — intentionally omitted (idle CPU spin).
	argv = append(argv, strconv.Itoa(s.NodeID))
	return argv
}

// NVRAMKiBFor returns an NVRAM size (KiB) large enough to hold a startup-config
// of configLen bytes plus codec headers/padding, never below DefaultNVRAMKiB.
// IOL's -n must be >= the NVRAM file we inject or it will truncate/reject it.
func NVRAMKiBFor(configLen int) int {
	// Headers (~52B) + generous slack; round the config up to whole KiB and add
	// the default as headroom for private-config/growth.
	needKiB := (configLen+1023)/1024 + DefaultNVRAMKiB
	if needKiB < DefaultNVRAMKiB {
		return DefaultNVRAMKiB
	}
	return needKiB
}

// Environ returns the environment for an IOL process. IOL reads its license from
// the iourc file named by IOURC (we point it at the shared lab dir's iourc; IOL
// also falls back to ./iourc in cwd, which is the same file). There is
// deliberately NO console env var — the console is a pty bridged by the
// supervisor, not an IOL-opened TCP port (P0-confirmed).
func (s Spec) Environ() []string {
	return []string{
		"IOURC=" + s.WorkDir + "/iourc",
	}
}

// VPCSArgv builds the argv for a VPCS process hosting up to 9 PCs with its
// built-in telnet console.
//
//	vpcs -N <name> -p <consolePort> [-m <startMac>] [-s <startUdp> -c <startUdp>]
//
// VPCS connects each PC over UDP tunnels configured via its runtime commands or
// -s/-c options. We expose the console port with -p; per-PC UDP endpoints are
// wired by the relay layer using the ports recorded in the Spec by the caller.
func (s Spec) VPCSArgv(name string) ([]string, error) {
	if s.VPCSCount < 1 || s.VPCSCount > 9 {
		return nil, fmt.Errorf("vpcs count must be 1..9, got %d", s.VPCSCount)
	}
	return []string{
		"vpcs",
		"-N", name,
		"-p", strconv.Itoa(s.ConsolePort),
	}, nil
}

package node

import (
	"fmt"
	"strconv"

	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
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
	// VPCSUDPLocal is the UDP port VPCS binds to RECEIVE frames the relay
	// forwards to it (VPCS's -s). 0 means no UDP tunnel (an unconnected PC).
	VPCSUDPLocal int
	// VPCSUDPRemote is the UDP port VPCS SENDS frames to — the relay's receiving
	// port (VPCS's -c). 0 means no UDP tunnel.
	VPCSUDPRemote int
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
// The instance id is the last positional argument. It is netmap.InstanceID(
// NodeID), not the raw lab node id (IOL rejects 0; valid range 1..1024), and it
// matches the NETMAP node id and the nvram_<id> filename. We deliberately DO NOT
// pass "-l": the keepalive flag causes a 100% idle CPU spin on this kernel
// (see PLAN.md).
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
	// -m <MB>: without it IOL runs at its built-in default (256MB), which is
	// too small for modern 17.x x86_64 images to finish booting — the process
	// stays alive while IOS wedges, so the failure is silent. Spec.RAM comes
	// from the lab doc's node.ram.
	if s.RAM > 0 {
		argv = append(argv, "-m", strconv.Itoa(s.RAM))
	}
	nvKiB := s.NVRAMKiB
	if nvKiB <= 0 {
		nvKiB = DefaultNVRAMKiB
	}
	argv = append(argv, "-n", strconv.Itoa(nvKiB))
	// NOTE: no "-l" — intentionally omitted (idle CPU spin).
	// The positional is the IOL *instance* id (netmap.InstanceID), NOT the raw
	// lab node id: IOL rejects instance id 0 (valid range 1..1024), and this id
	// must match the NETMAP node id and the nvram_<id> filename.
	argv = append(argv, strconv.Itoa(netmap.InstanceID(s.NodeID)))
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

// VPCSArgv builds the argv for a bundled vpcs 0.8.3 process.
//
//	vpcs -p <ConsolePort> -i <count> [-s <localUdp> -c <remoteUdp> -t 127.0.0.1]
//
// name is unused: vpcs 0.8.3 has NO name flag — passing "-N" makes it print
// `vpcs: invalid option -- 'N'` and exit. It is kept in the signature for a
// stable call site.
//
// Console (VM-confirmed): vpcs is its OWN telnet console server. "-p
// <ConsolePort>" makes vpcs open and listen on that TCP port and serve a
// `VPCS>` prompt. So — unlike IOL — vpcs is NOT run under the supervisor's pty
// and the supervisor does NOT bind ConsolePort; vpcs owns it. vpcs also
// daemonizes (forks; the launcher exits immediately). "-i <count>" sets the
// number of PCs the process hosts (default 1).
//
// UDP tunnel: vpcs speaks the UDP tunnel protocol natively (it never speaks IOL
// netio), so a VPCS<->IOL link connects vpcs's UDP tunnel straight to the
// supervisor's relay (the IOL side reaches the same relay through an iouyap
// netio<->UDP bridge). The port pairing (from server.bridgePlan.vpcsUDPFor):
//
//   - -s <localUdp>  : the port vpcs BINDS to receive frames the relay forwards
//     to it — i.e. the relay endpoint's RemotePort for this VPCS.
//   - -c <remoteUdp> : the port vpcs SENDS frames to — the relay endpoint's
//     receiving LocalPort.
//   - -t 127.0.0.1   : the tunnel peer host (the relay runs on loopback).
//
// When no UDP tunnel is wired (VPCSUDPLocal/Remote == 0) the -s/-c/-t flags are
// omitted and the PC is unconnected.
func (s Spec) VPCSArgv(name string) ([]string, error) {
	_ = name // vpcs 0.8.3 has no name flag; see doc above.
	if s.VPCSCount < 1 || s.VPCSCount > 9 {
		return nil, fmt.Errorf("vpcs count must be 1..9, got %d", s.VPCSCount)
	}
	if s.ConsolePort <= 0 || s.ConsolePort > 65535 {
		return nil, fmt.Errorf("vpcs requires a console port, got %d", s.ConsolePort)
	}
	argv := []string{
		"vpcs",
		"-p", strconv.Itoa(s.ConsolePort),
		"-i", strconv.Itoa(s.VPCSCount),
	}
	if s.VPCSUDPLocal > 0 && s.VPCSUDPRemote > 0 {
		argv = append(argv,
			"-s", strconv.Itoa(s.VPCSUDPLocal),
			"-c", strconv.Itoa(s.VPCSUDPRemote),
			"-t", "127.0.0.1",
		)
	}
	return argv, nil
}

package node

import (
	"fmt"
	"strconv"

	"github.com/rohanpunj/iolbox/supervisor/internal/netmap"
)

// Spec is the platform-independent description of how to launch a node. It is
// built from a lab node + allocated resources, then handed to the linux spawner.
type Spec struct {
	// NodeID is the lab node id (also the IOL instance id passed as the last argv).
	NodeID int
	Kind   string // "iol" | "vpcs"
	// Name is the node's display name from the lab doc. Used by the console hub
	// to emit a terminal-title escape so native telnet clients label their
	// tab/window with the node name instead of host:port. Cosmetic; may be "".
	Name string

	// IOL fields.
	ImagePath   string // absolute path to the IOL ELF binary
	WorkDir     string // working directory (for IOL: the SHARED lab dir with NETMAP+iourc+nvram)
	Ethernet    int    // ethernet adapter groups
	Serial      int    // serial adapter groups
	RAM         int    // megabytes
	ConsolePort int    // telnet console TCP port (the supervisor's pty->telnet bridge binds this)
	// ConsoleBind is the host the pty->telnet bridge listener binds. Empty
	// defaults to loopback; 0.0.0.0 lets a native telnet client on the GUI
	// host reach <vm-ip>:<ConsolePort> directly. (VPCS ignores this: vpcs
	// binds its own console on all interfaces.)
	ConsoleBind string
	NVRAMKiB    int // IOL NVRAM size in KiB (-n); 0 uses DefaultNVRAMKiB

	// VPCS fields.
	VPCSCount int // number of PCs (<= 9)
	// VPCSUDPLocal is the UDP port VPCS binds to RECEIVE frames the relay
	// forwards to it (VPCS's -s). 0 means no UDP tunnel (an unconnected PC).
	VPCSUDPLocal int
	// VPCSUDPRemote is the UDP port VPCS SENDS frames to — the relay's receiving
	// port (VPCS's -c). 0 means no UDP tunnel.
	VPCSUDPRemote int
}

// MinIOLRAMMB is the floor the supervisor applies to every IOL node's -m value.
//
// IOL's own built-in default is 256 MB, which is not enough for a modern 17.x
// x86_64 image to finish booting. Confirmed on real hardware 2026-08-14 with
// IOL 17.18.02 at ram=256:
//
//	%SYS-2-MALLOCFAIL: Memory allocation of 220004 bytes failed
//	Pool: Processor  Free: 21216  Cause: Not enough free memory
//
// The critical part is that this is SILENT to the supervisor: the IOL process
// stays alive with IOS wedged mid-init, so lab.start returns ok and every node
// reports "running". Nothing in the control protocol or the supervisor logs
// distinguishes it from a healthy boot — the only evidence is on the console.
// That is why the floor is enforced here rather than left to lab authors: a
// too-small -m does not fail loudly enough to be a lab-authoring bug you would
// ever notice.
const MinIOLRAMMB = 1024

// IOLRAMFor returns the effective -m megabytes for an IOL node, given the lab
// document's node.ram (0 = unset) and the detected image class ("l2" / "l3" /
// "unknown"; anything else is treated as unknown).
//
// Both unset and too-small values are raised to the class floor. Clamping an
// explicit value is deliberate: an under-provisioned node does not merely run
// slower, it wedges during init while still reporting "running", so honouring
// the author's number would trade a working node for an inert one. The cost of
// raising it is bounded by what IOS actually allocates.
//
// The floors are per-class because the L2 and L3 image families do not share a
// footprint; they are equal today (both 17.x families need well over IOL's
// 256 MB built-in) and this switch is the single place to diverge them.
func IOLRAMFor(ram int, class string) int {
	floor := MinIOLRAMMB
	switch class {
	case "l2":
		floor = MinIOLRAMMB
	case "l3":
		floor = MinIOLRAMMB
	}
	if ram < floor {
		return floor
	}
	return ram
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
	// stays alive while IOS wedges, so the failure is silent (see MinIOLRAMMB).
	// Spec.RAM comes from the lab doc's node.ram put through IOLRAMFor by the
	// server's buildSpec, so for an IOL node it is never below the class floor
	// and the flag is always emitted. The RAM > 0 guard remains so a Spec built
	// by hand (tests, non-IOL kinds) still behaves.
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
// netio), so its frames go to a per-VPCS udp<->tap shim (internal/vtap) whose
// tap joins the link's Linux bridge. The port pairing (VPCSUDPLocal/Remote =
// the shim's [vpcsBind, shimBind] from server.setupVPCSFabric):
//
//   - -s <localUdp>  : the port vpcs BINDS to receive frames the shim forwards
//     to it (VPCSUDPLocal).
//   - -c <remoteUdp> : the port vpcs SENDS frames to — the shim's bind port
//     (VPCSUDPRemote).
//   - -t 127.0.0.1   : the tunnel peer host (the shim runs on loopback).
//
// When no UDP tunnel is wired (VPCSUDPLocal/Remote == 0) the -s/-c/-t flags are
// omitted and the PC is unconnected.
//
// MAC uniqueness ("-m"): every lab node is its OWN vpcs process, and vpcs
// derives each PC's MAC as 00:50:79:66:68:XX where XX = (intra-process PC
// index + "-m" value) & 0xff (vpcs.c pth_reader, confirmed against the
// v0.8.3 source). The intra-process index is always 0 for a single-PC node,
// and without "-m" it defaults to 0 too — so every VPCS node in a lab was
// generating the IDENTICAL MAC (00:50:79:66:68:00), which broke L2
// forwarding for any lab with more than one VPCS node on the same segment
// (switches flapping the MAC between ports, duplicate-address warnings).
// Passing "-m" = NodeID makes the last octet vary per node (vpcs's own
// arg2int() does not actually clamp to the documented 0..240 range — only
// the final "& 0xff" in pth_reader bounds it — so any NodeID is safe here).
// VPCSMAC returns the MAC vpcs 0.8.3 will assign to PC index pcIndex of the
// node with the given id, GIVEN the "-m" value VPCSArgv passes (see the -m
// block above). It is a consequence of that flag, not an independent fact: if
// VPCSArgv ever stops passing "-m nodeID", this function is wrong and must
// change with it. That coupling is the entire reason this lives here and not
// in the GUI.
func VPCSMAC(nodeID, pcIndex int) string {
	return fmt.Sprintf("00:50:79:66:68:%02x", byte((nodeID+pcIndex)&0xff))
}

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
		"-m", strconv.Itoa(s.NodeID),
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

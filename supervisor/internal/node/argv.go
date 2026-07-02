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
	WorkDir     string // per-node working directory (holds NETMAP, iourc, nvram)
	Ethernet    int    // ethernet adapter groups
	Serial      int    // serial adapter groups
	RAM         int    // megabytes
	ConsolePort int    // telnet console TCP port

	// VPCS fields.
	VPCSCount int // number of PCs (<= 9)
}

// IOLArgv builds the argv for launching an IOL instance.
//
// IOL argv shape (community convention):
//
//	<image> [-e <eth groups>] [-s <serial groups>] [-n <nvram KiB>] <instance-id>
//
// The instance id is the last positional argument and matches the NETMAP node
// id. We deliberately DO NOT pass "-l": the keepalive flag causes a 100% idle
// CPU spin on this kernel (see PLAN.md).
//
// The IOL binary reads NETMAP and the iourc license from its current working
// directory, so WorkDir must be the process cwd (set by the spawner) and hold
// both files. The telnet console is exposed by IOL itself when the
// IOURC/telnet environment is set (see Environ) — IOL listens on the telnet
// port passed via the environment rather than an argv flag.
//
// ASSUMPTION (verify in P0): the exact IOL argv flags and whether the console
// telnet port is selected via env (IOURC + a listen port) or by IOL's built-in
// default of 127.0.0.1:(2000+id). We model the env approach and expose the port
// via Environ; P0 confirms against a real image.
func (s Spec) IOLArgv() []string {
	argv := []string{s.ImagePath}
	if s.Ethernet > 0 {
		argv = append(argv, "-e", strconv.Itoa(s.Ethernet))
	}
	if s.Serial > 0 {
		argv = append(argv, "-s", strconv.Itoa(s.Serial))
	}
	// NVRAM size in KiB; 64 KiB is a safe default that fits typical configs.
	argv = append(argv, "-n", "64")
	// NOTE: no "-l" — intentionally omitted (idle CPU spin).
	argv = append(argv, strconv.Itoa(s.NodeID))
	return argv
}

// Environ returns the environment for an IOL process. IOL reads its license
// from the iourc file named by IOURC and keys it by NETIO_NAME/hostname. We
// also advertise the telnet console port so the wrapper/telnet bridge binds it.
//
// ASSUMPTION (verify in P0): the precise env var names IOL honours for the
// iourc path and console port. These are centralised here so P0 fixes them once.
func (s Spec) Environ() []string {
	return []string{
		"IOURC=" + s.WorkDir + "/iourc",
		"IOL_CONSOLE_PORT=" + strconv.Itoa(s.ConsolePort),
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

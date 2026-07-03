package main

import (
	"os/exec"
	"strings"
)

// backend identifies which runtime backend the launcher will drive.
type backend string

const (
	backendQEMU backend = "qemu"
	backendWSL  backend = "wsl"
	backendAuto backend = "auto"
)

// detection is the result of probing this machine for backend availability.
type detection struct {
	// hypervisorPresent is the DISCRIMINATOR for whether WSL2 can actually run.
	// True == a hypervisor (Hyper-V / Windows Hypervisor Platform / a
	// type-1 VMM) is active under Windows. On a VMware Workstation box this is
	// FALSE (VMware uses its own VMM without the Windows hypervisor), and WSL2
	// cannot start a distro even though wsl.exe and `wsl --version` work.
	hypervisorPresent bool
	hypervisorKnown   bool // false if we couldn't determine it

	wslExePresent  bool // wsl.exe on PATH
	wslVersionOK   bool // `wsl --version` succeeded (WSL2 engine installed)
	wslVersionText string

	// reason explains the auto decision in plain English (printed to the user).
	reason string
}

// wslUsable reports whether the wsl2 backend can actually bring a distro up on
// THIS machine. wsl.exe + a working `wsl --version` are necessary but NOT
// sufficient: on a VMware box both succeed yet no distro can start. The real
// gate is an active hypervisor.
func (d detection) wslUsable() bool {
	return d.wslExePresent && d.wslVersionOK && d.hypervisorKnown && d.hypervisorPresent
}

// detectBackends probes the machine. It never mutates system state (never
// enables a Windows feature) — read-only detection only.
func detectBackends() detection {
	var d detection

	// wsl.exe presence + `wsl --version` (the WSL2 engine check).
	if path, err := exec.LookPath("wsl.exe"); err == nil && path != "" {
		d.wslExePresent = true
		// `wsl --version` prints the WSL/kernel versions and exits 0 when the
		// modern (WSL2) engine is installed. On stock/old boxes it errors.
		out, err := exec.Command("wsl.exe", "--version").CombinedOutput()
		// wsl.exe emits UTF-16LE on some Windows builds; strip NULs so the
		// text is greppable and the success check is on exit status anyway.
		txt := strings.ReplaceAll(string(out), "\x00", "")
		d.wslVersionText = strings.TrimSpace(txt)
		if err == nil {
			d.wslVersionOK = true
		}
	}

	// Hypervisor presence — the WSL2-usability discriminator. We ask Windows
	// directly via Win32_ComputerSystem.HypervisorPresent (equivalent to CPUID
	// leaf 1 ECX bit 31). This is a read-only CIM query; it enables nothing.
	//
	// Why shelling PowerShell instead of an in-process CPUID: a pure-Go CPUID
	// needs a small .s asm stub (or a x/sys dependency). To keep the launcher
	// stdlib-only (matching the supervisor's convention) we use the documented
	// PowerShell probe. HypervisorPresent is authoritative and matches what a
	// CPUID leaf-1 ECX[31] read would report, without adding a build asset.
	if present, ok := queryHypervisorPresent(); ok {
		d.hypervisorPresent = present
		d.hypervisorKnown = true
	}

	return d
}

// queryHypervisorPresent runs the documented read-only PowerShell CIM query.
// Returns (present, ok); ok==false when the query itself failed to produce a
// parseable answer.
func queryHypervisorPresent() (bool, bool) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-Command", "(Get-CimInstance Win32_ComputerSystem).HypervisorPresent")
	out, err := cmd.Output()
	if err != nil {
		return false, false
	}
	return parseHypervisorPresent(string(out))
}

// parseHypervisorPresent interprets the PowerShell boolean output ("True" /
// "False", possibly with UTF-16 NULs / whitespace). Split out for unit testing.
func parseHypervisorPresent(raw string) (bool, bool) {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, "\x00", "")))
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// chooseBackend applies the docs/providers.md ranking for the given --backend
// selection. Returns the chosen backend and fills det.reason with a
// plain-English explanation. For explicit qemu/wsl it does not override the
// user's choice, but still records why.
func chooseBackend(sel backend, det *detection) backend {
	switch sel {
	case backendQEMU:
		det.reason = "backend forced to qemu (--backend qemu)."
		return backendQEMU
	case backendWSL:
		if det.wslUsable() {
			det.reason = "backend forced to wsl; WSL2 is usable on this machine."
		} else {
			det.reason = "backend forced to wsl (--backend wsl), but WSL2 is NOT usable here: " +
				wslUnusableReason(*det)
		}
		return backendWSL
	default: // auto
		if det.wslUsable() {
			det.reason = "auto: WSL2 is usable (an active hypervisor was detected) — preferring wsl2 (fastest cold start, smallest footprint)."
			return backendWSL
		}
		det.reason = "auto: WSL2 is NOT usable here (" + wslUnusableReason(*det) +
			") — falling back to the bundled QEMU-TCG backend, which conflicts with nothing."
		return backendQEMU
	}
}

// wslUnusableReason returns the specific reason WSL2 can't run, in the order
// the probes gate on.
func wslUnusableReason(d detection) string {
	if !d.wslExePresent {
		return "wsl.exe was not found on PATH — WSL is not installed"
	}
	if !d.wslVersionOK {
		return "`wsl --version` failed — the modern WSL2 engine is not installed"
	}
	if !d.hypervisorKnown {
		return "could not determine hypervisor status (Win32_ComputerSystem query failed)"
	}
	if !d.hypervisorPresent {
		return "no active Windows hypervisor (Win32_ComputerSystem.HypervisorPresent = False), " +
			"so no WSL2 distro can start. Enabling Hyper-V / the Windows Hypervisor " +
			"Platform WOULD make WSL2 work, but it degrades VMware Workstation and kills " +
			"nested virtualization — the launcher will NOT enable it for you"
	}
	return "unknown"
}

//go:build linux

package extnet

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// DefaultRouteIface returns the interface name holding the VM's IPv4 default
// route (the one nat MASQUERADEs out of), read from /proc/net/route so no
// external command is needed. It errors when there is no default route.
func DefaultRouteIface() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", fmt.Errorf("extnet: open /proc/net/route: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		// Iface Destination Gateway Flags ... — destination 00000000 is default.
		if len(fields) < 4 {
			continue
		}
		if fields[1] == "00000000" {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("extnet: no IPv4 default route found")
}

// PickMgmtIface resolves the management interface a mgmt node's macvtap attaches
// to. If pref is non-empty it is used verbatim (the -mgmt-iface flag). Otherwise
// it auto-picks the first UP, non-loopback ethernet interface that is NOT the
// default-route interface; if the only usable interface IS the default-route
// one, that is used (a single-NIC VM). It errors when no candidate exists.
func PickMgmtIface(pref string) (string, error) {
	if pref != "" {
		return pref, nil
	}
	def, _ := DefaultRouteIface() // best-effort; empty is fine (no default route)

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("extnet: list interfaces: %w", err)
	}
	var candidates []string
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if ifi.Flags&net.FlagUp == 0 {
			continue
		}
		if !isEthernet(ifi.Name) {
			continue
		}
		candidates = append(candidates, ifi.Name)
	}
	// Prefer a non-default-route candidate (so the mgmt macvtap doesn't hang off
	// the same NIC the VM routes through, if a second NIC exists).
	for _, name := range candidates {
		if name != def {
			return name, nil
		}
	}
	// Only the default-route NIC is usable (single-NIC VM): use it.
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if def != "" {
		return def, nil
	}
	return "", fmt.Errorf("extnet: no candidate management interface (no UP non-loopback ethernet iface)")
}

// isEthernet reports whether a device looks like an ethernet NIC (not a bridge,
// tap, or virtual overlay we manage ourselves), from /sys/class/net/<name>/type
// == 1 (ARPHRD_ETHER). Missing/unreadable type is treated as non-ethernet.
func isEthernet(name string) bool {
	// Never adopt one of our own managed devices.
	if strings.HasPrefix(name, "iolnat") || strings.HasPrefix(name, "iolmgmt") {
		return false
	}
	raw, err := os.ReadFile("/sys/class/net/" + name + "/type")
	if err != nil {
		return false
	}
	t, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return false
	}
	return t == 1
}

// Detect reports the runtime's support for nat/mgmt nodes. It is used at server
// startup to gate the hello features. sudoOK should be the result of the
// server's `sudo -n true` probe (injected so the pure gate logic stays testable
// off Linux). nat needs /dev/net/tun + sudo; mgmt needs a candidate mgmt iface +
// sudo. See Capabilities / GateFeatures.
func Detect(sudoOK bool, mgmtPref string) Capabilities {
	tun := fileExists("/dev/net/tun")
	_, mgmtErr := PickMgmtIface(mgmtPref)
	return Capabilities{
		NAT:  sudoOK && tun,
		Mgmt: sudoOK && tun && mgmtErr == nil,
	}
}

// SudoOK probes whether `sudo -n true` succeeds (passwordless sudo available),
// the precondition for every privileged extnet command.
func SudoOK() bool {
	return exec.Command("sudo", "-n", "true").Run() == nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

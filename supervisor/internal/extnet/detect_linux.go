//go:build linux

package extnet

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
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

// Detect reports the runtime's support for nat nodes. It is used at server
// startup to gate the hello features. sudoOK should be the result of the
// server's `sudo -n true` probe (injected so the pure gate logic stays testable
// off Linux). nat needs /dev/net/tun + sudo. See Capabilities / GateFeatures.
func Detect(sudoOK bool) Capabilities {
	tun := fileExists("/dev/net/tun")
	return Capabilities{
		NAT: sudoOK && tun,
	}
}

// SudoOK probes whether `sudo -n true` succeeds (passwordless sudo available),
// the precondition for every privileged extnet command.
func SudoOK() bool {
	cmd := exec.Command("sudo", "-n", "true")
	// Start and register atomically so the subreaper cannot reap this direct
	// child—`sudo -n true` returns almost instantly—before Wait owns it.
	if err := tool.Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid }); err != nil {
		return false
	}
	err := cmd.Wait()
	tool.Registry.Remove(cmd.Process.Pid)
	return err == nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

//go:build linux

package fabric

import (
	"os/exec"

	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// HasSudo probes whether `sudo -n true` succeeds (passwordless sudo
// available), the precondition for every privileged fabric command. Mirrors
// extnet.SudoOK's exact approach.
func HasSudo() bool {
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

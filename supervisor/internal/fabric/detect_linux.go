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
	if err := cmd.Start(); err != nil {
		return false
	}
	tool.Registry.Add(cmd.Process.Pid)
	err := cmd.Wait()
	tool.Registry.Remove(cmd.Process.Pid)
	return err == nil
}

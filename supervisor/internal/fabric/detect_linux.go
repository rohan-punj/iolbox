//go:build linux

package fabric

import "os/exec"

// HasSudo probes whether `sudo -n true` succeeds (passwordless sudo
// available), the precondition for every privileged fabric command. Mirrors
// extnet.SudoOK's exact approach.
func HasSudo() bool {
	return exec.Command("sudo", "-n", "true").Run() == nil
}

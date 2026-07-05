//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachFromConsoleCtrlC sets CREATE_NEW_PROCESS_GROUP so Windows disables
// CTRL_C_EVENT delivery to the child (qemu), letting our own signal.NotifyContext
// handler run the graceful QMP system_powerdown path instead of qemu dying
// abruptly alongside the launcher on Ctrl-C. Windows-only field — see the call
// site in qemu.go for why this is split out (keeps GOOS=linux buildable).
func detachFromConsoleCtrlC(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

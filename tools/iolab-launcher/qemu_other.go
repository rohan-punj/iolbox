//go:build !windows

package main

import "os/exec"

// detachFromConsoleCtrlC is a no-op on non-Windows: this launcher is a
// Windows-only product (GOOS=windows is the real target), but the codebase is
// kept `go build`-able under GOOS=linux for CI/dev-machine convenience — see
// qemu.go's call site and qemu_windows.go for the real (Windows) behavior.
func detachFromConsoleCtrlC(cmd *exec.Cmd) {}

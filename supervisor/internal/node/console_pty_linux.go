//go:build linux

package node

import (
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
)

// startConsolePTY mirrors pty.Start's controlling-terminal setup, but prepares
// the terminal before exec. Updating termios after Start can race a child's own
// terminal setup, overwriting its echo/canonical settings or losing our ISIG
// change. The caller keeps this start inside the process registry lock.
func startConsolePTY(cmd *exec.Cmd) (*os.File, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, err
	}
	defer slave.Close()
	if err := prepareConsolePTY(master); err != nil {
		_ = master.Close()
		return nil, err
	}
	if cmd.Stdin == nil {
		cmd.Stdin = slave
	}
	if cmd.Stdout == nil {
		cmd.Stdout = slave
	}
	if cmd.Stderr == nil {
		cmd.Stderr = slave
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		return nil, err
	}
	return master, nil
}

func prepareConsolePTY(master *os.File) error {
	// IOL receives console control keys as input bytes. A default PTY instead
	// translates VINTR/VSUSP into SIGINT/SIGTSTP for its foreground process
	// group, killing or suspending the appliance rather than its CLI command.
	// Change only ISIG: the device still owns canonical mode, echo, speeds,
	// character mappings, and every other terminal attribute.
	var attrs syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&attrs))); errno != 0 {
		return os.NewSyscallError("console pty TCGETS", errno)
	}
	attrs.Lflag &^= syscall.ISIG
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&attrs))); errno != 0 {
		return os.NewSyscallError("console pty TCSETS", errno)
	}
	if err := syscall.SetNonblock(int(master.Fd()), true); err != nil {
		return err
	}
	// Also confirm this descriptor is supported by Go's poller.
	return master.SetWriteDeadline(time.Time{})
}

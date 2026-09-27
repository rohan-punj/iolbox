//go:build linux

package node

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/creack/pty"
)

func TestConsolePTYChildStartsWithSignalsDisabledAndOwnsLaterModes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `
test "$1" = "expected argument" || exit 42
stty -a
printf '\nCHILD-MODES\n'
stty -echo -icanon
stty -a
`, "console-test", "expected argument")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	attrs := &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	cmd.SysProcAttr = attrs
	master, err := startConsolePTY(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatalf("child failed: %v; stderr=%s", err, &stderr)
	}
	if cmd.SysProcAttr != attrs || attrs.Pdeathsig != syscall.SIGTERM || !attrs.Setsid || !attrs.Setctty {
		t.Fatalf("process attributes not preserved: %+v", cmd.SysProcAttr)
	}
	if cmd.Stdout != &stdout || cmd.Stderr != &stderr {
		t.Fatal("custom output streams replaced")
	}
	modes := strings.Split(stdout.String(), "CHILD-MODES")
	if len(modes) != 2 || !strings.Contains(modes[0], "-isig") || !strings.Contains(modes[1], "-isig") {
		t.Fatalf("child must observe signals disabled before its first instruction: %s", &stdout)
	}
	if !strings.Contains(modes[1], "-icanon") || !strings.Contains(modes[1], "-echo ") {
		t.Fatalf("child terminal mode changes overwritten: %s", &stdout)
	}
}

func TestConsolePTYFailedExecDoesNotReturnMaster(t *testing.T) {
	cmd := exec.Command("/nonexistent/iolbox-console-test")
	master, err := startConsolePTY(cmd)
	if err == nil || master != nil || cmd.Process != nil {
		t.Fatalf("failed exec returned live resources: master=%v process=%v err=%v", master, cmd.Process, err)
	}
}

func consolePTYAttrs(t *testing.T, tty *os.File) syscall.Termios {
	t.Helper()
	var attrs syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&attrs))); errno != 0 {
		t.Fatal(errno)
	}
	return attrs
}

func setConsolePTYAttrs(t *testing.T, tty *os.File, attrs syscall.Termios) {
	t.Helper()
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&attrs))); errno != 0 {
		t.Fatal(errno)
	}
}

func TestConsolePTYDisablesSignalsAndPreservesOtherAttributes(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	before := consolePTYAttrs(t, slave)
	before.Lflag |= syscall.ISIG
	setConsolePTYAttrs(t, slave, before)
	if err = prepareConsolePTY(master); err != nil {
		t.Fatal(err)
	}
	after := consolePTYAttrs(t, slave)
	want := before
	want.Lflag &^= syscall.ISIG
	if after != want {
		t.Fatalf("terminal attributes changed beyond ISIG: got %+v, want %+v", after, want)
	}
}

func TestConsolePTYControlKeysReachSlaveAsBytes(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	attrs := consolePTYAttrs(t, slave)
	// Canonical mode needs a newline for delivery. No child process is started:
	// the test proves the driver delivers VINTR/VSUSP, without risking signals
	// to any real foreground process group.
	attrs.Lflag |= syscall.ISIG | syscall.ICANON
	attrs.Lflag &^= syscall.ECHO
	attrs.Cc[syscall.VINTR] = 3
	attrs.Cc[syscall.VSUSP] = 26
	setConsolePTYAttrs(t, slave, attrs)
	if err = prepareConsolePTY(master); err != nil {
		t.Fatal(err)
	}
	if err = syscall.SetNonblock(int(slave.Fd()), true); err != nil {
		t.Fatal(err)
	}
	if err = slave.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	want := []byte{'a', 3, 26, 'b', '\n'}
	if _, err = master.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err = io.ReadFull(slave, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("control bytes got %v, want %v", got, want)
	}
}

func TestConsolePTYBlockedWriteHonorsDeadline(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err = prepareConsolePTY(master); err != nil {
		t.Fatal(err)
	}
	h := &consoleHub{pty: master, done: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	at := time.Now()
	err = h.writeTurnAfterClaimForTest(ctx, make([]byte, 1<<20))
	if !errors.Is(err, os.ErrDeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if time.Since(at) > time.Second {
		t.Fatal("write exceeded deadline")
	}
}

func (h *consoleHub) writeTurnAfterClaimForTest(ctx context.Context, p []byte) error {
	release, id, err := h.claimTurn(ctx, "blocked")
	if err != nil {
		return err
	}
	defer release()
	return h.writeTurn(ctx, id, p)
}

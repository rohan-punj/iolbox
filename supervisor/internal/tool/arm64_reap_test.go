//go:build linux

package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestArm64ReapLifecycle exercises the Linux waitid ABI with a real orphaned
// grandchild. The direct child is signalled and waited by os/exec; its
// grandchild is adopted by this process after the shell exits, then observed
// with waitid(WNOWAIT) and collected with wait4. The final ECHILD check makes
// the no-zombie/process-tree-empty assertion explicit.
func TestArm64ReapLifecycle(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("shell helper unavailable: %v", err)
	}
	if err := SetSubreaper(); err != nil {
		t.Skipf("child subreaper unavailable: %v", err)
	}

	dir := t.TempDir()
	pidFile := dir + "/grandchild.pid"
	cmd := exec.Command("sh", "-c", fmt.Sprintf(
		"trap 'exit 17' TERM; (sleep 0.25; exit 23) & echo $! > %q; while :; do sleep 0.01; done",
		pidFile,
	))
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn lifecycle helper: %v", err)
	}
	var grandchildPID int
	cleaned := false
	t.Cleanup(func() {
		if !cleaned {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		if grandchildPID != 0 {
			_ = syscall.Kill(grandchildPID, syscall.SIGKILL)
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(grandchildPID, &status, 0, nil)
		}
	})

	deadline := time.Now().Add(2 * time.Second)
	for grandchildPID == 0 && time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			grandchildPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if grandchildPID <= 0 {
		t.Fatal("helper did not publish its grandchild PID")
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal child: %v", err)
	}
	waitErr := cmd.Wait()
	cleaned = true
	if waitErr == nil {
		t.Fatal("signalled child exited successfully, want exit status 17")
	}
	childStatus, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !childStatus.Exited() || childStatus.ExitStatus() != 17 {
		t.Fatalf("child status = %#v, want exited(17)", cmd.ProcessState.Sys())
	}

	var grandchildStatus syscall.WaitStatus
	found := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pid, err := reapPeekable()
		if err != nil {
			t.Fatalf("waitid probe: %v", err)
		}
		if pid != grandchildPID {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		n, err := syscall.Wait4(grandchildPID, &grandchildStatus, syscall.WNOHANG, nil)
		if err != nil {
			t.Fatalf("wait4 grandchild: %v", err)
		}
		if n == grandchildPID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("orphaned grandchild PID %d was not observed and reaped", grandchildPID)
	}
	if !grandchildStatus.Exited() || grandchildStatus.ExitStatus() != 23 {
		t.Fatalf("grandchild status = %#v, want exited(23)", grandchildStatus)
	}

	if _, err := os.Stat("/proc/" + strconv.Itoa(grandchildPID)); !os.IsNotExist(err) {
		t.Fatalf("grandchild /proc entry remains after wait4: %v", err)
	}
	var status syscall.WaitStatus
	if pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil); pid != -1 || err != syscall.ECHILD {
		t.Fatalf("remaining child process after lifecycle: pid=%d err=%v", pid, err)
	}
}

//go:build linux

package tool

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReapLinuxChildFixture(t *testing.T) {
	switch os.Getenv("REAP_FIXTURE_MODE") {
	case "orphan":
		time.Sleep(750 * time.Millisecond)
		os.Exit(0)
	case "parent":
		pidFile := os.Getenv("REAP_FIXTURE_PID_FILE")
		if pidFile == "" {
			t.Fatal("fixture pid file is empty")
		}
		orphan := exec.Command(os.Args[0], "-test.run=^TestReapLinuxChildFixture$")
		orphan.Env = []string{"REAP_FIXTURE_MODE=orphan"}
		if err := orphan.Start(); err != nil {
			t.Fatalf("start orphan fixture: %v", err)
		}
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", orphan.Process.Pid)), 0o600); err != nil {
			_ = orphan.Process.Kill()
			_, _ = orphan.Process.Wait()
			t.Fatal(err)
		}
		time.Sleep(5 * time.Second)
	}
}

func TestReapLinuxOwnershipSplit(t *testing.T) {
	if err := exec.Command(os.Args[0], "-test.run=^TestReapLinuxChildFixture$").Run(); err != nil {
		t.Skipf("test process cannot fork: %v", err)
	}
	if err := SetSubreaper(); err != nil {
		t.Skipf("child subreaper is unavailable: %v", err)
	}

	registry := NewPIDRegistry()
	stop := StartReaper(registry)
	defer stop()

	pidFile := filepath.Join(t.TempDir(), "orphan.pid")
	registered := exec.Command(os.Args[0], "-test.run=^TestReapLinuxChildFixture$")
	registered.Env = []string{"REAP_FIXTURE_MODE=parent", "REAP_FIXTURE_PID_FILE=" + pidFile}
	if err := registered.Start(); err != nil {
		t.Skipf("test process cannot fork registered child: %v", err)
	}
	registeredPID := registered.Process.Pid
	registry.Add(registeredPID)
	waited := false
	defer func() {
		if waited {
			return
		}
		_ = registered.Process.Kill()
		_, _ = registered.Process.Wait()
		registry.Remove(registeredPID)
	}()

	var orphanPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if _, scanErr := fmt.Sscanf(string(data), "%d", &orphanPID); scanErr == nil && orphanPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if orphanPID == 0 {
		t.Fatal("orphan fixture did not publish its pid")
	}

	if err := registered.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitErr := registered.Wait()
	waited = true
	registry.Remove(registeredPID)
	if waitErr == nil {
		t.Fatal("registered child exited cleanly after SIGKILL")
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("registered child wait error = %v, want ExitError", waitErr)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, err := os.Stat(fmt.Sprintf("/proc/%d", orphanPID))
		if errors.Is(err, os.ErrNotExist) {
			stop()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unregistered orphan pid %d still has a /proc entry", orphanPID)
}

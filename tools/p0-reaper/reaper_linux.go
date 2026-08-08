//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	prSetChildSubreaper = 36
	wnowait             = 0x01000000
)

type pidRegistry struct {
	sync.Mutex
	pids map[int]struct{}
}

func runReaper(target, resultPath, setprivPath, launcherPath string) error {
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("PR_SET_CHILD_SUBREAPER: %w", errno)
	}

	registry := &pidRegistry{pids: make(map[int]struct{})}
	reapedOrphans := make(chan int, 8)
	stopReaper := make(chan struct{})
	go reapLoop(registry, reapedOrphans, stopReaper)

	cmd, err := commandFor(target, setprivPath, launcherPath)
	if err != nil {
		close(stopReaper)
		return err
	}
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		close(stopReaper)
		return fmt.Errorf("start registered child: %w", err)
	}
	registry.add(cmd.Process.Pid)

	waitResult := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		registry.remove(cmd.Process.Pid)
		waitResult <- err
	}()

	grandchildPID, err := waitForGrandchildPID(10 * time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		close(stopReaper)
		return err
	}
	if err := cmd.Process.Kill(); err != nil {
		close(stopReaper)
		return fmt.Errorf("SIGKILL registered GUI: %w", err)
	}

	select {
	case err := <-waitResult:
		if err == nil {
			close(stopReaper)
			return errors.New("registered GUI unexpectedly exited cleanly")
		}
	case <-time.After(5 * time.Second):
		close(stopReaper)
		return errors.New("exec.Cmd.Wait did not deliver the GUI exit")
	}

	if err := waitForOrphanReap(grandchildPID, reapedOrphans, 5*time.Second); err != nil {
		close(stopReaper)
		return err
	}
	close(stopReaper)

	lines := []string{
		"SUBREAPER PASS",
		"DIRECT_CHILD_WAIT PASS",
		"ORPHAN_REAP PASS",
		fmt.Sprintf("DIRECT_CHILD_PID %d", cmd.Process.Pid),
		fmt.Sprintf("ORPHAN_PID %d", grandchildPID),
	}
	if err := os.WriteFile(resultPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}

// commandFor builds the registered child's argv for exactly ONE privilege
// transition mechanism -- the same one the rest of the P0 fixture already
// selected for this box (see LAUNCH_MODE in docs/tests/p0-spike.sh). T0.6 must
// not invent a third path: on a target whose setpriv cannot express the pinned
// transition, the setpriv-wrapped GUI lands in a half-transitioned state where
// tool-stubgui never gets far enough to publish its grandchild PID, and the
// reaper probe then fails for a reason that has nothing to do with SIGCHLD
// ownership.
//
// There is deliberately no bare-root mode. Running the GUI as unrestricted root
// would exercise a transition production never uses, and every reaper assertion
// would still pass -- an untested branch that can only ever hide a defect.
func commandFor(target, setprivPath, launcherPath string) (*exec.Cmd, error) {
	switch {
	case setprivPath != "" && launcherPath != "":
		return nil, errors.New("--setpriv and --launcher are mutually exclusive")
	case setprivPath != "":
		return exec.Command(setprivPath,
			"--reuid", "ioltool", "--regid", "ioltool", "--clear-groups", "--no-new-privs",
			"--bounding-set", "-all,+cap_net_raw",
			"--inh-caps", "-all,+cap_net_raw",
			"--ambient-caps", "-all,+cap_net_raw",
			"--", target,
		), nil
	case launcherPath != "":
		return exec.Command(launcherPath, "--user", "ioltool", "--", target), nil
	default:
		return nil, errors.New("one of --setpriv or --launcher is required: the reaper probe must run its GUI through a real privilege transition")
	}
}

func (r *pidRegistry) add(pid int) {
	r.Lock()
	r.pids[pid] = struct{}{}
	r.Unlock()
}

func (r *pidRegistry) remove(pid int) {
	r.Lock()
	delete(r.pids, pid)
	r.Unlock()
}

func (r *pidRegistry) contains(pid int) bool {
	r.Lock()
	_, ok := r.pids[pid]
	r.Unlock()
	return ok
}

func reapLoop(registry *pidRegistry, reaped chan<- int, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG|wnowait, nil)
		if err == nil && pid > 0 && !registry.contains(pid) {
			var orphanStatus syscall.WaitStatus
			if orphanPID, reapErr := syscall.Wait4(pid, &orphanStatus, syscall.WNOHANG, nil); reapErr == nil && orphanPID == pid {
				reaped <- pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForGrandchildPID(timeout time.Duration) (int, error) {
	path := os.Getenv("IOLBOX_STUB_GRANDCHILD_PID_FILE")
	if path == "" {
		return 0, errors.New("IOLBOX_STUB_GRANDCHILD_PID_FILE is required")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0, fmt.Errorf("grandchild pid did not appear at %s", filepath.Clean(path))
}

func waitForOrphanReap(pid int, reaped <-chan int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case got := <-reaped:
			if got == pid {
				if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); !os.IsNotExist(err) {
					return fmt.Errorf("orphan %d still exists after reap", pid)
				}
				return nil
			}
		default:
			if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
				return errors.New("orphan disappeared without reaper observation")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return fmt.Errorf("orphan %d was not reaped", pid)
}

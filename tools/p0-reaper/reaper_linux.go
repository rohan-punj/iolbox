//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	prSetChildSubreaper = 36

	// waitid(2) idtype and option values. These are kernel ABI constants
	// spelled out by hand: package syscall exposes no Waitid wrapper on
	// linux/amd64 and this module is stdlib-only (see go.mod -- no
	// golang.org/x/sys), the same way p0-launcher spells out its prctl and
	// capset constants.
	pAll    = 0          // idtype_t P_ALL: any child, id ignored
	wNoHang = 0x00000001 // WNOHANG
	wExited = 0x00000004 // WEXITED
	wNoWait = 0x01000000 // WNOWAIT: report, but leave the child reapable
)

// siginfo mirrors the kernel's siginfo_t as waitid(2) fills it in for a
// SIGCHLD-shaped notification. Only the leading fields are named; the trailing
// padding exists so the struct is exactly the 128 bytes the kernel writes.
//
// Layout note (x86-64): siginfo_t opens with si_signo, si_errno, si_code --
// three int32s -- and then the union of signal-specific fields. The union
// contains 8-byte members (si_addr, and the clock_t si_utime/si_stime of the
// SIGCHLD arm), so it is 8-byte aligned and starts at offset 16, not 12; the
// kernel spells this out as __ARCH_SI_PREAMBLE_SIZE == 4 * sizeof(int) in
// arch/x86/include/uapi/asm/siginfo.h. si_pid is the first member of the
// SIGCHLD arm, so it is an int32 at offset 16.
type siginfo struct {
	signo  int32
	errno  int32
	code   int32
	_      int32  // union alignment padding (__ARCH_SI_PREAMBLE_SIZE)
	pid    int32  // _sigchld._pid
	uid    uint32 // _sigchld._uid
	status int32  // _sigchld._status
	_      int32
	_      [12]uint64 // si_utime, si_stime, and the rest of the 128-byte tail
}

// Compile-time assertion that siginfo is exactly siginfo_t sized. Both
// constants are untyped uintptr expressions, so either a short or a long
// struct makes one of them negative and fails the build.
const (
	_ = unsafe.Sizeof(siginfo{}) - 128
	_ = 128 - unsafe.Sizeof(siginfo{})
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

	// The registered GUI is now confirmed dead (cmd.Wait delivered its exit), so
	// the kernel has already reparented its children -- exit_notify() reparents
	// onto the nearest PR_SET_CHILD_SUBREAPER ancestor BEFORE the dying parent is
	// made reapable, so by the time Wait4 hands us the GUI's status the orphan's
	// PPid is already this process. Nothing else in the fixture ever signals the
	// grandchild, and its --grandchild mode blocks forever, so without an
	// explicit SIGKILL here it never terminates and never becomes reapable. That
	// kill IS the scenario T0.6 models: a tool's script child that has to be
	// cleaned up after its GUI parent died.
	if err := killReparentedOrphan(grandchildPID, 2*time.Second); err != nil {
		close(stopReaper)
		return err
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

// peekReapable reports the pid of a child that has exited and is waiting to be
// collected WITHOUT collecting it -- the zombie is deliberately left in place
// so the ownership split can decide who is allowed to reap it. It returns 0
// when nothing is currently reapable, which includes the legitimate "this
// process has no children at all" case: that is the state at loop startup, and
// again in the window between the registered child being collected by
// cmd.Wait() and the orphan being reparented onto this subreaper.
//
// The peek has to be waitid(2). WNOWAIT is a waitid-only option: kernel_wait4()
// screens its options argument against
// ~(WNOHANG|WUNTRACED|WCONTINUED|__WNOTHREAD|__WCLONE|__WALL) and returns
// EINVAL for any other bit, and WNOWAIT (0x01000000) is not in that set. A
// "peek" built on wait4 therefore never peeks -- every call fails with EINVAL
// before it can report anything.
func peekReapable() (int, error) {
	var info siginfo
	_, _, errno := syscall.Syscall6(syscall.SYS_WAITID,
		pAll, 0, uintptr(unsafe.Pointer(&info)), wExited|wNoHang|wNoWait, 0, 0)
	switch errno {
	case 0:
		// A WNOHANG waitid with nothing ready still succeeds; the kernel
		// reports the empty result by writing si_signo == 0 and si_pid == 0.
		return int(info.pid), nil
	case syscall.ECHILD, syscall.EINTR:
		// No children to wait on, or the poll was interrupted. Neither is an
		// error here -- the caller just polls again.
		return 0, nil
	default:
		return 0, errno
	}
}

func reapLoop(registry *pidRegistry, reaped chan<- int, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		// The peek is non-destructive, so a pid that belongs to the registered
		// child stays reapable for cmd.Wait() to collect -- that is the whole
		// ownership split. Only an unregistered pid is ours to reap, and that
		// second wait4 (no WNOWAIT) is the destructive one.
		pid, err := peekReapable()
		if err == nil && pid > 0 && !registry.contains(pid) {
			var orphanStatus syscall.WaitStatus
			orphanPID, reapErr := syscall.Wait4(pid, &orphanStatus, syscall.WNOHANG, nil)
			if reapErr == nil && orphanPID == pid {
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

// killReparentedOrphan confirms pid has been reparented onto this process --
// which is both the T0.6 assertion (the subreaper, not init, inherited it) and
// a cheap identity check that the pid read out of the pid file has not been
// recycled onto some unrelated process -- and then SIGKILLs it so it becomes
// reapable. The reparenting is not raced: it has already happened by the time
// the registered child's exit status is delivered, so the first probe should
// match; the poll only covers /proc bookkeeping lag.
func killReparentedOrphan(pid int, timeout time.Duration) error {
	self := os.Getpid()
	deadline := time.Now().Add(timeout)
	for {
		ppid, err := parentPID(pid)
		if err != nil {
			return fmt.Errorf("inspect orphan %d: %w", pid, err)
		}
		if ppid == self {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				return fmt.Errorf("SIGKILL orphan %d: %w", pid, err)
			}
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("orphan %d was not reparented onto the subreaper (PPid %d, want %d)", pid, ppid, self)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func parentPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(line, "PPid:")
		if !ok {
			continue
		}
		return strconv.Atoi(strings.TrimSpace(rest))
	}
	return 0, fmt.Errorf("no PPid line in /proc/%d/status", pid)
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
				// The /proc entry vanishing is not by itself proof that something
				// other than reapLoop collected it: reapLoop removes the entry with
				// its wait4 and only then publishes the pid, so a probe landing in
				// that window would otherwise fail a run that actually passed. Give
				// the notification a bounded chance to arrive before concluding the
				// orphan was reaped behind our back.
				select {
				case got := <-reaped:
					if got == pid {
						return nil
					}
				case <-time.After(500 * time.Millisecond):
				}
				return errors.New("orphan disappeared without reaper observation")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return fmt.Errorf("orphan %d was not reaped", pid)
}

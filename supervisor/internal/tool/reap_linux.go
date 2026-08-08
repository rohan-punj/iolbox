//go:build linux

package tool

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	reapPRSetChildSubreaper = 36

	// waitid(2) idtype and option values. These are kernel ABI constants
	// spelled out by hand because package syscall exposes no Waitid wrapper on
	// linux/amd64 and this module intentionally has no x/sys dependency.
	reapPAll    = 0          // idtype_t P_ALL: any child, id ignored
	reapWNoHang = 0x00000001 // WNOHANG
	reapWExited = 0x00000004 // WEXITED
	reapWNoWait = 0x01000000 // WNOWAIT: report, but leave the child reapable

	reapCageWaitTimeout = 5 * time.Second
)

// reapSiginfo mirrors the kernel's siginfo_t as waitid(2) fills it in for a
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
type reapSiginfo struct {
	reapSigno     int32
	reapErrno     int32
	reapCode      int32
	reapUnionPad  int32
	reapPID       int32
	reapUID       uint32
	reapStatus    int32
	reapStatusPad int32
	reapTail      [12]uint64
}

// Compile-time assertions that the hand-laid-out ABI record is exactly
// siginfo_t sized. A short or long record makes one expression negative and
// fails the Linux build instead of silently corrupting waitid output.
const (
	_ = unsafe.Sizeof(reapSiginfo{}) - 128
	_ = 128 - unsafe.Sizeof(reapSiginfo{})
)

// SetSubreaper marks the supervisor as the adoption point for orphaned
// grandchildren. It is process-wide, which is why the registry covers every
// direct exec.Cmd child in the supervisor rather than only tool endpoints.
func SetSubreaper() error {
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, reapPRSetChildSubreaper, 1, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("tool: set child subreaper: %w", errno)
	}
	return nil
}

// StartReaper starts the supervisor-scope orphan collector. Its returned stop
// function can be called repeatedly and does not return until the poller has
// exited, so shutdown cannot race process termination.
func StartReaper(reg *PIDRegistry) (stop func()) {
	if reg == nil {
		reg = Registry
	}
	reapStop := make(chan struct{})
	reapDone := make(chan struct{})
	var reapStopOnce sync.Once
	go func() {
		defer close(reapDone)
		reapLoop(reg, reapStop)
	}()
	return func() {
		reapStopOnce.Do(func() { close(reapStop) })
		<-reapDone
	}
}

// ReapStale removes only the prior generation's objects recorded for this
// durable installation, then cleans unrecorded tool cages inside its delegated
// cgroup subtree. It intentionally never searches host-global netns or veth
// names, because those names are not an install boundary.
func ReapStale(cfg ReapConfig) error {
	state, err := LoadObjectState(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("tool: load stale object state: %w", err)
	}
	if !reapStateEligible(state, cfg.InstanceID) {
		return nil
	}

	cagePaths, cageErr := ListCages(cfg.Root)
	if cageErr != nil && reapMissing(cageErr) {
		cageErr = nil
	}
	plan := reapPlan(state, cfg.InstanceID, cagePaths)
	cleanupErr := reapExecute(cfg, state, plan)
	if cageErr != nil {
		cageErr = fmt.Errorf("tool: list stale cages: %w", cageErr)
	}
	return errors.Join(cageErr, cleanupErr)
}

// reapPeekable uses the waitid ABI so the kernel reports a ready child while
// leaving ownership of its exit status to either cmd.Wait or this loop. A
// zero PID is the successful empty-poll result, not an error.
func reapPeekable() (int, error) {
	var info reapSiginfo
	_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, reapPAll, 0, uintptr(unsafe.Pointer(&info)), reapWExited|reapWNoHang|reapWNoWait, 0, 0)
	switch errno {
	case 0:
		return int(info.reapPID), nil
	case syscall.ECHILD, syscall.EINTR:
		return 0, nil
	default:
		return 0, errno
	}
}

// reapLoop polls at supervisor scope because orphaned grandchildren reparent
// there. It leaves registered direct children to their own cmd.Wait and uses a
// separate PID-specific collection only for an unregistered orphan.
func reapLoop(reg *PIDRegistry, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		pid, err := reapPeekable()
		if err == nil && pid > 0 && !reg.Contains(pid) {
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// reapExecute applies the portable decision plan while continuing after each
// individual failure. A state record is pruned only after every listed object
// operation for that node succeeded or was already absent.
func reapExecute(cfg ReapConfig, state ObjectState, plan []reapPlanEntry) error {
	records := make(map[int]ObjectRecord, len(state.Objects))
	complete := make(map[int]bool, len(state.Objects))
	ids := make([]int, 0, len(state.Objects))
	for _, record := range state.Objects {
		if _, exists := records[record.NodeID]; !exists {
			ids = append(ids, record.NodeID)
		}
		records[record.NodeID] = record
		complete[record.NodeID] = true
	}
	sort.Ints(ids)

	var failures []error
	for _, entry := range plan {
		var err error
		switch entry.reapKind {
		case reapPlanKillCage:
			err = KillCage(entry.reapPath)
		case reapPlanWaitCage:
			err = WaitCageEmpty(entry.reapPath, reapCageWaitTimeout)
		case reapPlanRemoveCage:
			err = RemoveCage(entry.reapPath)
		case reapPlanNetns:
			err = DeleteNetns(entry.reapNodeID)
		case reapPlanVeth:
			err = DeleteVeth(entry.reapNodeID)
		case reapPlanSocket:
			err = os.RemoveAll(entry.reapPath)
		}
		if err == nil || reapMissing(err) {
			continue
		}
		if _, recorded := records[entry.reapNodeID]; recorded {
			complete[entry.reapNodeID] = false
		}
		failures = append(failures, fmt.Errorf("tool: stale cleanup node %d (%s): %w", entry.reapNodeID, entry.reapKind, err))
	}

	for _, nodeID := range ids {
		if !complete[nodeID] {
			continue
		}
		if err := PruneObject(cfg.StateDir, cfg.InstanceID, nodeID); err != nil && !reapMissing(err) {
			failures = append(failures, fmt.Errorf("tool: prune stale node %d: %w", nodeID, err))
		}
	}
	return errors.Join(failures...)
}

// reapMissing turns disappearance between the ordered cleanup operations into
// success, which is the normal result when another teardown already won.
func reapMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

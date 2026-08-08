//go:build linux

package main

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	capSetGid           = 6
	capSetUid           = 7
	capSetPcap          = 8
	capNetRaw           = 13
	capProbeLast        = 63
	prSetNoNewPrivs     = 38
	prCapBsetDrop       = 24
	prSetSecurebits     = 28
	prCapAmbient        = 47
	prCapAmbientRaise   = 2
	securebitNoroot     = 1 << 0
	securebitNorootLock = 1 << 1
	securebitKeepCaps   = 1 << 4
	securebitKeepLock   = 1 << 5
	capsetVersion3      = 0x20080522
)

type capHeader struct {
	Version uint32
	PID     int32
}

// capData mirrors struct __user_cap_data_struct. _LINUX_CAPABILITY_VERSION_3
// describes a 64-bit capability set, so capget(2)/capset(2) read and write TWO
// of these structs (index 0 = caps 0..31, index 1 = caps 32..63). Handing the
// kernel a single struct makes it copy 24 bytes out of a 12-byte object: the
// high words are whatever happened to follow it in memory, and capset then
// returns EPERM because those junk bits are not a subset of the caller's
// current permitted set. That was the "p0-launcher: capset: operation not
// permitted" seen on the target appliance.
type capData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

type capSets [2]capData

func capabilityName(cap int) string {
	switch cap {
	case capSetGid:
		return "CAP_SETGID"
	case capSetUid:
		return "CAP_SETUID"
	case capSetPcap:
		return "CAP_SETPCAP"
	case capNetRaw:
		return "CAP_NET_RAW"
	default:
		return fmt.Sprintf("cap %d", cap)
	}
}

func capGet() (capSets, error) {
	header := capHeader{Version: capsetVersion3}
	var data capSets
	if _, _, errno := syscall.Syscall(syscall.SYS_CAPGET,
		uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&data[0])), 0); errno != 0 {
		return data, fmt.Errorf("capget: %w", errno)
	}
	return data, nil
}

func capApply(effective, permitted, inheritable uint64) error {
	header := capHeader{Version: capsetVersion3}
	var data capSets
	data[0] = capData{
		Effective:   uint32(effective),
		Permitted:   uint32(permitted),
		Inheritable: uint32(inheritable),
	}
	data[1] = capData{
		Effective:   uint32(effective >> 32),
		Permitted:   uint32(permitted >> 32),
		Inheritable: uint32(inheritable >> 32),
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_CAPSET,
		uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&data[0])), 0); errno != 0 {
		return fmt.Errorf("capset(eff=%#x prm=%#x inh=%#x): %w", effective, permitted, inheritable, errno)
	}
	return nil
}

func effectiveHas(sets capSets, cap int) bool {
	if cap < 32 {
		return sets[0].Effective&(1<<uint(cap)) != 0
	}
	return sets[1].Effective&(1<<uint(cap-32)) != 0
}

// requireStartingPrivilege fails loudly, and by name, when the launcher was
// started without the authority its own transition needs. p0-launcher is
// always spawned pre-uid-switch as root, so a miss here means something
// upstream (a container's default capability mask, an inherited securebits
// setting, or a bounding-set drop by a parent) already took the capability
// away -- which is a very different failure from a malformed capset call.
func requireStartingPrivilege() error {
	sets, err := capGet()
	if err != nil {
		return newLaunchFailure(launchExitCapGet, "capget", err)
	}
	var missing []string
	for _, cap := range []int{capSetPcap, capSetUid, capSetGid, capNetRaw} {
		if !effectiveHas(sets, cap) {
			missing = append(missing, capabilityName(cap))
		}
	}
	if len(missing) > 0 {
		return newLaunchFailure(launchExitStartCaps, "starting privilege", fmt.Errorf(
			"launcher started without %s in its effective set (uid=%d CapEff=%08x%08x); "+
				"the native transition must run as fully privileged root before the uid switch",
			strings.Join(missing, ", "), os.Getuid(), sets[1].Effective, sets[0].Effective))
	}
	return nil
}

// launchTransition performs the capability transition the learning-tool plan
// pins setpriv(1) to, in the one order the kernel actually permits:
//
//	(1) PR_SET_NO_NEW_PRIVS
//	(2) PR_SET_SECUREBITS -- needs CAP_SETPCAP, and SECBIT_KEEP_CAPS must be
//	    in place BEFORE the uid switch or the permitted set is wiped by it
//	(3) PR_CAPBSET_DROP loop -- needs CAP_SETPCAP; dropping a capability from
//	    the bounding set does not remove it from permitted/effective, so the
//	    loop may drop CAP_SETPCAP/SETUID/SETGID from the bounding set and still
//	    use them below
//	(4) setgroups/setgid/setuid -- need CAP_SETGID/CAP_SETUID effective, so
//	    they must happen while the effective set is still full
//	(5) ONE final capset to the desired end state. This is a pure reduction of
//	    an already-nonroot process, so it needs no CAP_SETPCAP of its own --
//	    which is exactly why there is no separate "make myself capable again"
//	    step after it.
//	(6) PR_CAP_AMBIENT_RAISE (needs CAP_NET_RAW in both permitted and
//	    inheritable, established by step 5), then execve.
//
// The previous ordering ran capset() before the uid switch with an empty
// effective set, which would have made setgroups/setgid/setuid fail EPERM even
// if the capset call itself had been well-formed.
func launchTransition(name, target string, args []string) error {
	account, err := user.Lookup(name)
	if err != nil {
		return newLaunchFailure(launchExitLookupUser, "lookup user", fmt.Errorf("lookup user %q: %w", name, err))
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return newLaunchFailure(launchExitParseUID, "parse uid", fmt.Errorf("parse uid: %w", err))
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return newLaunchFailure(launchExitParseGID, "parse gid", fmt.Errorf("parse gid: %w", err))
	}

	// prctl(2), capset(2) and the set*id(2) calls below are per-THREAD on
	// Linux, and execve(2) inherits the credentials of the calling thread.
	// Without this pin the Go scheduler could migrate the goroutine between
	// steps and leave the transition spread over several threads. There is no
	// matching UnlockOSThread: this thread execve()s.
	runtime.LockOSThread()

	if err := requireStartingPrivilege(); err != nil {
		return err
	}

	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return newLaunchFailure(launchExitNoNewPrivs, "PR_SET_NO_NEW_PRIVS", errno)
	}

	securebits := uintptr(securebitNoroot | securebitNorootLock | securebitKeepCaps | securebitKeepLock)
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetSecurebits, securebits, 0, 0, 0, 0); errno != 0 {
		return newLaunchFailure(launchExitSecurebits, "PR_SET_SECUREBITS", fmt.Errorf("PR_SET_SECUREBITS(%#x): %w", securebits, errno))
	}

	for cap := 0; cap <= capProbeLast; cap++ {
		if cap == capNetRaw {
			continue
		}
		// EINVAL just means this kernel has no such capability number.
		if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prCapBsetDrop, uintptr(cap), 0, 0, 0, 0); errno != 0 && errno != syscall.EINVAL {
			return newLaunchFailure(launchExitBoundingSet, "PR_CAPBSET_DROP", fmt.Errorf("PR_CAPBSET_DROP %s: %w", capabilityName(cap), errno))
		}
	}

	if _, _, errno := syscall.RawSyscall(syscall.SYS_SETGROUPS, 0, 0, 0); errno != 0 {
		return newLaunchFailure(launchExitSetgroups, "setgroups", fmt.Errorf("clear supplementary groups: %w", errno))
	}
	if _, _, errno := syscall.RawSyscall(syscall.SYS_SETGID, uintptr(gid), 0, 0); errno != 0 {
		return newLaunchFailure(launchExitSetgid, "setgid", fmt.Errorf("setgid %d: %w", gid, errno))
	}
	if _, _, errno := syscall.RawSyscall(syscall.SYS_SETUID, uintptr(uid), 0, 0); errno != 0 {
		return newLaunchFailure(launchExitSetuid, "setuid", fmt.Errorf("setuid %d: %w", uid, errno))
	}

	raw := uint64(1) << capNetRaw
	if err := capApply(raw, raw, raw); err != nil {
		return newLaunchFailure(launchExitCapset, "capset", err)
	}
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prCapAmbient, prCapAmbientRaise, capNetRaw, 0, 0, 0); errno != 0 {
		return newLaunchFailure(launchExitAmbient, "PR_CAP_AMBIENT_RAISE", fmt.Errorf("PR_CAP_AMBIENT_RAISE %s: %w", capabilityName(capNetRaw), errno))
	}

	return newLaunchFailure(launchExitExec, "execve", fmt.Errorf("execve %q: %w", target,
		syscall.Exec(target, append([]string{target}, args...), os.Environ())))
}

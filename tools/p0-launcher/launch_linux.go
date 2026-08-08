//go:build linux

package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	capNetRaw           = 13
	capLast             = 40
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

type capData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

func launchTransition(name, target string, args []string) error {
	account, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("lookup user %q: %w", name, err)
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse uid: %w", err)
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return fmt.Errorf("parse gid: %w", err)
	}

	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("PR_SET_NO_NEW_PRIVS: %w", errno)
	}
	for cap := 0; cap <= capLast; cap++ {
		if cap == capNetRaw {
			continue
		}
		if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prCapBsetDrop, uintptr(cap), 0, 0, 0, 0); errno != 0 && errno != syscall.EINVAL {
			return fmt.Errorf("PR_CAPBSET_DROP cap %d: %w", cap, errno)
		}
	}

	securebits := uintptr(securebitNoroot | securebitNorootLock | securebitKeepCaps | securebitKeepLock)
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetSecurebits, securebits, 0, 0, 0, 0); errno != 0 {
		return fmt.Errorf("PR_SET_SECUREBITS: %w", errno)
	}

	raw := uint32(1 << capNetRaw)
	header := capHeader{Version: capsetVersion3}
	data := capData{Permitted: raw, Inheritable: raw}
	if _, _, errno := syscall.Syscall(syscall.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&data)), 0); errno != 0 {
		return fmt.Errorf("capset: %w", errno)
	}
	if err := syscall.Setgroups([]int{}); err != nil {
		return fmt.Errorf("clear supplementary groups: %w", err)
	}
	if err := syscall.Setgid(int(gid)); err != nil {
		return fmt.Errorf("setgid: %w", err)
	}
	if err := syscall.Setuid(int(uid)); err != nil {
		return fmt.Errorf("setuid: %w", err)
	}
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prCapAmbient, prCapAmbientRaise, capNetRaw, 0, 0, 0); errno != 0 {
		return fmt.Errorf("PR_CAP_AMBIENT_RAISE: %w", errno)
	}

	return syscall.Exec(target, append([]string{target}, args...), os.Environ())
}

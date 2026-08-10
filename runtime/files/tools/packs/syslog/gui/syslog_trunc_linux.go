//go:build linux

package main

import "syscall"

func msgTruncFlag() int { return syscall.MSG_TRUNC }

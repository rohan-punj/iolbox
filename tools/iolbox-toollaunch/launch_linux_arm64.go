//go:build linux && arm64

package main

// sysSetns is linux/arm64's syscall number for setns(2). See
// launch_linux_amd64.go for why this is pinned locally instead of using
// syscall.SYS_SETNS, and why it is split per-GOARCH: linux/amd64 uses 308,
// which is NOT the same number on arm64.
const sysSetns = 268

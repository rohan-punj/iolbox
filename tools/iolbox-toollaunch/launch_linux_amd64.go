//go:build linux && amd64

package main

// sysSetns is linux/amd64's syscall number for setns(2). Not every Go
// release's syscall package exports SYS_SETNS (it did not at the time this
// was written), so this is pinned locally the same way the prctl/capset
// numbers in launch_linux.go are. Split by GOARCH because the number is
// architecture-specific -- see launch_linux_arm64.go for linux/arm64's.
const sysSetns = 308

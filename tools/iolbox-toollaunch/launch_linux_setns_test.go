//go:build linux

package main

import (
	"runtime"
	"testing"
)

// TestSysSetns pins sysSetns to the correct per-arch setns(2) syscall
// number. sysSetns is defined in an arch-tagged file (launch_linux_amd64.go
// / launch_linux_arm64.go); this test fails on whichever arch's file has
// the wrong number instead of only surfacing as a broken join at runtime.
func TestSysSetns(t *testing.T) {
	want := map[string]int{
		"amd64": 308,
		"arm64": 268,
	}[runtime.GOARCH]
	if want == 0 {
		t.Skipf("no known setns(2) number recorded for GOARCH=%s", runtime.GOARCH)
	}
	if sysSetns != want {
		t.Fatalf("sysSetns = %d, want %d for GOARCH=%s", sysSetns, want, runtime.GOARCH)
	}
}

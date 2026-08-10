//go:build linux

package tool

import (
	"os"
	"testing"
)

func TestCageLinuxReadableCgroupV2(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real cgroup filesystem checks require root")
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("cgroup v2 is not mounted")
	}
	if _, err := CagePopulated("/sys/fs/cgroup"); err != nil {
		t.Fatalf("CagePopulated on the cgroup v2 root: %v", err)
	}
}

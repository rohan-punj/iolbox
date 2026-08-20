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

	root, err := InitCgroupRoot()
	if err != nil {
		t.Fatalf("InitCgroupRoot() error = %v", err)
	}
	if root.Delegated == "/sys/fs/cgroup" || root.SupervisorLeaf == "/sys/fs/cgroup" {
		t.Fatalf("InitCgroupRoot() returned hierarchy root instead of delegated paths: %+v", root)
	}

	nodeID := 9901
	cagePath, cageFD, err := CreateCage(root, nodeID, DefaultLimits())
	if err != nil {
		t.Fatalf("CreateCage() error = %v", err)
	}
	defer cageFD.Close()
	defer RemoveCage(cagePath)

	if populated, err := CagePopulated(cagePath); err != nil {
		t.Fatalf("CagePopulated(%q) error = %v", cagePath, err)
	} else if populated {
		t.Fatalf("new cage %q unexpectedly reports populated", cagePath)
	}
}

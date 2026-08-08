//go:build linux

package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLinuxProbeCleansEveryObject(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the operational tool probe requires root")
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("cgroup v2 is unavailable")
	}

	root, err := InitCgroupRoot()
	if err != nil {
		t.Fatalf("InitCgroupRoot() error = %v", err)
	}
	caps := Detect(root)
	if !caps.OK() {
		t.Fatalf("Detect() returned unsupported matrix: %+v", caps)
	}

	probeID := detectProbeNodeID
	if present, err := detectProbeNetnsPresent(probeID); err != nil {
		t.Fatalf("verify probe netns removal: %v", err)
	} else if present {
		t.Fatalf("probe netns %q remains", NetnsName(probeID))
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", HostVethName(probeID))); err == nil {
		t.Fatalf("probe veth %q remains", HostVethName(probeID))
	} else if !os.IsNotExist(err) {
		t.Fatalf("verify probe veth removal: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root.Delegated, CageName(probeID))); err == nil {
		t.Fatalf("probe cgroup remains")
	} else if !os.IsNotExist(err) {
		t.Fatalf("verify probe cgroup removal: %v", err)
	}
	if _, err := os.Lstat(SocketDir("", probeID)); err == nil {
		t.Fatalf("probe socket directory remains")
	} else if !os.IsNotExist(err) {
		t.Fatalf("verify probe socket directory removal: %v", err)
	}
}

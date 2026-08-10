//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func killCgroup(path string) error {
	if err := os.WriteFile(filepath.Join(path, "cgroup.kill"), []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("cgroup.kill %s: %w", path, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
		if err == nil && populatedZero(string(data)) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove cgroup %s: %w", path, err)
			}
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("cgroup %s remained populated", path)
}

func populatedZero(events string) bool {
	for _, line := range strings.Split(events, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "populated" {
			return fields[1] == "0"
		}
	}
	return false
}

func deleteKernelObjects(netns, veth string) error {
	if err := exec.Command("ip", "netns", "del", netns).Run(); err != nil && !ipObjectMissing("netns", netns) {
		return fmt.Errorf("delete netns %s: %w", netns, err)
	}
	if err := exec.Command("ip", "link", "del", "dev", veth).Run(); err != nil && !ipObjectMissing("link", veth) {
		return fmt.Errorf("delete veth %s: %w", veth, err)
	}
	return nil
}

func ipObjectMissing(kind, name string) bool {
	args := []string{"link", "show", "dev", name}
	if kind == "netns" {
		args = []string{"netns", "list"}
	}
	output, err := exec.Command("ip", args...).CombinedOutput()
	if err == nil {
		return !strings.Contains(string(output), name)
	}
	return false
}

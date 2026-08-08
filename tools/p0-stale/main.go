// p0-stale is the standalone durable-id/state-file cleanup probe. It is kept
// separate from the future internal/tool package so P0 remains a spike and
// does not introduce the P1 engine early.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type state struct {
	InstanceID string `json:"instance_id"`
	CgroupPath string `json:"cgroup_path"`
	NetNS      string `json:"netns"`
	Veth       string `json:"veth"`
	RunDir     string `json:"run_dir"`
}

func main() {
	instanceFile := flag.String("instance-file", "", "durable install instance-id file")
	stateDir := flag.String("state-dir", "", "directory containing per-instance state files")
	cgroupRoot := flag.String("cgroup-root", "", "delegated cgroup root")
	runRoot := flag.String("run-root", "", "root of per-node runtime directories")
	flag.Parse()
	if *instanceFile == "" || *stateDir == "" || *cgroupRoot == "" || *runRoot == "" {
		fmt.Fprintln(os.Stderr, "usage: p0-stale --instance-file PATH --state-dir DIR --cgroup-root DIR --run-root DIR")
		os.Exit(2)
	}
	if err := reapStale(*instanceFile, *stateDir, *cgroupRoot, *runRoot); err != nil {
		fmt.Fprintf(os.Stderr, "p0-stale: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("STALE_REAP PASS")
}

func reapStale(instanceFile, stateDir, cgroupRoot, runRoot string) error {
	instanceBytes, err := os.ReadFile(instanceFile)
	if err != nil {
		return fmt.Errorf("read durable instance id: %w", err)
	}
	instanceID := strings.TrimSpace(string(instanceBytes))
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).MatchString(instanceID) {
		return errors.New("invalid durable instance id")
	}
	statePath := filepath.Join(stateDir, instanceID+".json")
	if !contained(stateDir, statePath) {
		return errors.New("state path escapes state directory")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		return fmt.Errorf("read state file: %w", err)
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("decode state file: %w", err)
	}
	if st.InstanceID != instanceID {
		return errors.New("state instance id does not match durable instance id")
	}
	if !regexp.MustCompile(`^iolt[0-9]+$`).MatchString(st.NetNS) || !regexp.MustCompile(`^vtool[0-9]+$`).MatchString(st.Veth) {
		return errors.New("state contains an unexpected kernel object name")
	}
	if !contained(cgroupRoot, st.CgroupPath) {
		return errors.New("state cgroup escapes delegated root")
	}
	if !contained(runRoot, st.RunDir) {
		return errors.New("state run directory escapes run root")
	}

	if err := killCgroup(st.CgroupPath); err != nil {
		return err
	}
	if err := deleteKernelObjects(st.NetNS, st.Veth); err != nil {
		return err
	}
	if err := os.RemoveAll(st.RunDir); err != nil {
		return fmt.Errorf("remove run directory: %w", err)
	}
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove state file: %w", err)
	}
	return nil
}

func contained(root, target string) bool {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

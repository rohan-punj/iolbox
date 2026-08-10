package tool

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// cageWrite is the pure description of one resource-limit write. Keeping the
// sequence separate from the Linux filesystem calls makes the limit-before-fd
// contract testable without requiring a delegated cgroup on the test host.
type cageWrite struct {
	name  string
	path  string
	value string
}

// cageLimitWrites returns the cgroup files in the order CreateCage must write
// them, before it opens the cage for atomic child placement.
func cageLimitWrites(cagePath string, lim Limits) []cageWrite {
	return []cageWrite{
		{name: "memory.max", path: filepath.Join(cagePath, "memory.max"), value: strconv.FormatInt(lim.MemoryMax, 10)},
		{name: "pids.max", path: filepath.Join(cagePath, "pids.max"), value: strconv.Itoa(lim.PidsMax)},
		{name: "cpu.max", path: filepath.Join(cagePath, "cpu.max"), value: lim.CPUMax},
		{name: "memory.swap.max", path: filepath.Join(cagePath, "memory.swap.max"), value: strconv.FormatInt(lim.SwapMax, 10)},
	}
}

// cageCreateOrder records the externally important CreateCage ordering: all
// limits bind before the directory fd can be handed to the child launcher.
func cageCreateOrder() []string {
	return []string{"memory.max", "pids.max", "cpu.max", "memory.swap.max", "open"}
}

// cageSelectRoot applies the idempotent delegated-root rule to a unified
// cgroup path from /proc/self/cgroup. A supervisor leaf means the process was
// already migrated on an earlier startup; its parent remains the controller
// root and the discovered path is reused as the leaf.
func cageSelectRoot(procCgroupPath string) (delegated, leaf string) {
	procCgroupPath = path.Clean(procCgroupPath)
	if path.Base(procCgroupPath) == SupervisorLeafName {
		return path.Dir(procCgroupPath), procCgroupPath
	}
	return procCgroupPath, path.Join(procCgroupPath, SupervisorLeafName)
}

// cageParseProcCgroup extracts the unified hierarchy path. Cgroup v2 exposes
// it as the line whose hierarchy id is 0 and whose controller list is empty.
func cageParseProcCgroup(contents string) (string, error) {
	for lineNumber, line := range strings.Split(contents, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			return "", fmt.Errorf("tool: malformed /proc/self/cgroup line %d", lineNumber+1)
		}
		if fields[0] == "0" && fields[1] == "" {
			if fields[2] == "" {
				return "", fmt.Errorf("tool: empty unified cgroup path")
			}
			return path.Clean(fields[2]), nil
		}
	}
	return "", fmt.Errorf("tool: unified cgroup hierarchy is absent")
}

// cageParsePopulated reads the populated field while allowing the other
// standard cgroup.events fields to evolve independently. A malformed record
// is rejected so a failed read cannot be mistaken for an empty cage.
func cageParsePopulated(events string) (bool, error) {
	var populated bool
	found := false
	for lineNumber, line := range strings.Split(events, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return false, fmt.Errorf("tool: malformed cgroup.events line %d", lineNumber+1)
		}
		if fields[0] != "populated" {
			continue
		}
		if found {
			return false, fmt.Errorf("tool: duplicate populated field")
		}
		switch fields[1] {
		case "0":
			populated = false
		case "1":
			populated = true
		default:
			return false, fmt.Errorf("tool: invalid cgroup.events populated value %q", fields[1])
		}
		found = true
	}
	if !found {
		return false, fmt.Errorf("tool: cgroup.events has no populated field")
	}
	return populated, nil
}

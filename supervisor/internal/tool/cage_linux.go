//go:build linux

package tool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	cageCgroupMount = "/sys/fs/cgroup"
	// Linux exposes O_PATH in fcntl.h, but this Go version omits it from
	// syscall, so keep the stable ABI value local to the Linux implementation.
	cageOPath = 0x200000
)

var (
	cageRootMu    sync.Mutex
	cageRoot      CgroupRoot
	cageRootReady bool
)

// InitCgroupRoot discovers and initializes the install-scoped delegated
// cgroup. The supervisor must first move itself into <D>/supervisor so <D>
// has no internal processes; enabling controllers before that migration is
// rejected by cgroup v2 with EBUSY, so this order is deliberately fixed.
func InitCgroupRoot() (CgroupRoot, error) {
	cageRootMu.Lock()
	defer cageRootMu.Unlock()
	if cageRootReady {
		return cageRoot, nil
	}

	contents, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return CgroupRoot{}, fmt.Errorf("tool: read /proc/self/cgroup: %w", err)
	}
	discovered, err := cageParseProcCgroup(string(contents))
	if err != nil {
		return CgroupRoot{}, err
	}
	delegatedPath, supervisorPath := cageSelectRoot(discovered)
	delegated, err := cageMountPath(delegatedPath)
	if err != nil {
		return CgroupRoot{}, err
	}
	supervisorLeaf, err := cageMountPath(supervisorPath)
	if err != nil {
		return CgroupRoot{}, err
	}

	if err := os.MkdirAll(supervisorLeaf, 0o755); err != nil {
		return CgroupRoot{}, fmt.Errorf("tool: create supervisor cgroup %s: %w", supervisorLeaf, err)
	}
	if err := cageMigratePID(supervisorLeaf, os.Getpid()); err != nil {
		return CgroupRoot{}, err
	}
	if err := cageEnableControllers(delegated); err != nil {
		return CgroupRoot{}, err
	}

	cageRoot = CgroupRoot{Delegated: delegated, SupervisorLeaf: supervisorLeaf}
	cageRootReady = true
	return cageRoot, nil
}

// CreateCage creates a sibling leaf, binds its limits, and returns an O_PATH
// directory descriptor for clone3(CLONE_INTO_CGROUP). The limits are written
// before the descriptor is opened so a child cannot allocate outside its cage
// before placement.
func CreateCage(root CgroupRoot, nodeID int, lim Limits) (path string, fd *os.File, err error) {
	path = filepath.Join(root.Delegated, CageName(nodeID))
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", nil, fmt.Errorf("tool: create cage %s: %w", path, err)
	}
	for _, write := range cageLimitWrites(path, lim) {
		if err := os.WriteFile(write.path, []byte(write.value+"\n"), 0o644); err != nil {
			return "", nil, fmt.Errorf("tool: write %s: %w", write.path, err)
		}
	}
	fd, err = os.OpenFile(path, cageOPath|syscall.O_DIRECTORY, 0)
	if err != nil {
		return "", nil, fmt.Errorf("tool: open cage %s: %w", path, err)
	}
	return path, fd, nil
}

// KillCage terminates every process in the cage subtree with cgroup.kill.
// This terminates but does not reap; the separate supervisor subreaper loop
// owns reaping orphaned descendants.
func KillCage(path string) error {
	if err := os.WriteFile(filepath.Join(path, "cgroup.kill"), []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("tool: kill cage %s: %w", path, err)
	}
	return nil
}

// CagePopulated reports whether cgroup.events says the cage contains a
// process, using the same populated field that cgroup.kill teardown observes.
func CagePopulated(path string) (bool, error) {
	contents, err := os.ReadFile(filepath.Join(path, "cgroup.events"))
	if err != nil {
		return false, fmt.Errorf("tool: read cage events %s: %w", path, err)
	}
	populated, err := cageParsePopulated(string(contents))
	if err != nil {
		return false, err
	}
	return populated, nil
}

// WaitCageEmpty waits until cgroup.events reports no processes or the bound
// expires. Polling is intentional because cgroup.events has no portable wait
// primitive and the cage is expected to disappear immediately after reaping.
func WaitCageEmpty(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		populated, err := CagePopulated(path)
		if err != nil {
			return err
		}
		if !populated {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("tool: cage %s remained populated", path)
		}
		remaining := time.Until(deadline)
		if remaining > 20*time.Millisecond {
			remaining = 20 * time.Millisecond
		}
		time.Sleep(remaining)
	}
}

// RemoveCage removes an empty cage directory. Missing cages are already
// cleaned up, so teardown treats them as success.
func RemoveCage(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tool: remove cage %s: %w", path, err)
	}
	return nil
}

// ListCages returns direct tool leaves under the install-scoped delegated
// root. The supervisor leaf is excluded explicitly because it contains the
// supervisor process and is never a tool cage.
func ListCages(root CgroupRoot) ([]string, error) {
	entries, err := os.ReadDir(root.Delegated)
	if err != nil {
		return nil, fmt.Errorf("tool: list cages under %s: %w", root.Delegated, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == SupervisorLeafName || !strings.HasPrefix(entry.Name(), "tool-") || !entry.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(root.Delegated, entry.Name()))
	}
	return paths, nil
}

func cageMountPath(cgroupPath string) (string, error) {
	if cgroupPath == "" || !strings.HasPrefix(cgroupPath, "/") {
		return "", fmt.Errorf("tool: invalid delegated cgroup path %q", cgroupPath)
	}
	trimmed := strings.TrimPrefix(cgroupPath, "/")
	joined := filepath.Join(cageCgroupMount, trimmed)
	if joined != cageCgroupMount && !strings.HasPrefix(joined, cageCgroupMount+string(filepath.Separator)) {
		return "", fmt.Errorf("tool: delegated cgroup path escapes %s", cageCgroupMount)
	}
	return joined, nil
}

func cageMigratePID(leaf string, pid int) error {
	procsPath := filepath.Join(leaf, "cgroup.procs")
	contents, err := os.ReadFile(procsPath)
	if err == nil {
		needle := strconv.Itoa(pid)
		for _, line := range strings.Fields(string(contents)) {
			if line == needle {
				return nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tool: read %s: %w", procsPath, err)
	}
	if err := os.WriteFile(procsPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return fmt.Errorf("tool: migrate pid %d to %s: %w", pid, leaf, err)
	}
	return nil
}

func cageEnableControllers(delegated string) error {
	controlPath := filepath.Join(delegated, "cgroup.subtree_control")
	contents, err := os.ReadFile(controlPath)
	if err != nil {
		return fmt.Errorf("tool: read %s: %w", controlPath, err)
	}
	controllers := strings.Fields(string(contents))
	needed := map[string]bool{"memory": false, "pids": false, "cpu": false}
	for _, controller := range controllers {
		if _, ok := needed[controller]; ok {
			needed[controller] = true
		}
	}
	if needed["memory"] && needed["pids"] && needed["cpu"] {
		return nil
	}
	if err := os.WriteFile(controlPath, []byte("+memory +pids +cpu\n"), 0o644); err != nil {
		return fmt.Errorf("tool: enable controllers in %s: %w", controlPath, err)
	}
	return nil
}

//go:build !linux

package tool

import (
	"os"
	"time"
)

// InitCgroupRoot cannot migrate a process or enable cgroup-v2 controllers off
// Linux, so the portable server receives the package's standard unsupported
// capability error.
func InitCgroupRoot() (CgroupRoot, error) { return CgroupRoot{}, ErrUnsupportedPlatform }

// CreateCage has no cgroup-v2 equivalent off Linux.
func CreateCage(root CgroupRoot, nodeID int, lim Limits) (string, *os.File, error) {
	return "", nil, ErrUnsupportedPlatform
}

// KillCage has no cgroup-v2 equivalent off Linux.
func KillCage(path string) error { return ErrUnsupportedPlatform }

// CagePopulated has no cgroup-v2 equivalent off Linux.
func CagePopulated(path string) (bool, error) { return false, ErrUnsupportedPlatform }

// WaitCageEmpty has no cgroup-v2 equivalent off Linux.
func WaitCageEmpty(path string, timeout time.Duration) error { return ErrUnsupportedPlatform }

// RemoveCage has no cgroup-v2 equivalent off Linux.
func RemoveCage(path string) error { return ErrUnsupportedPlatform }

// ListCages has no cgroup-v2 equivalent off Linux.
func ListCages(root CgroupRoot) ([]string, error) { return nil, ErrUnsupportedPlatform }

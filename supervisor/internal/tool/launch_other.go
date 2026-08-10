//go:build !linux

package tool

import "os/exec"

// LauncherAvailable cannot probe the Linux transition mechanisms off Linux.
func LauncherAvailable() (string, error) {
	return "", ErrUnsupportedPlatform
}

// Launch cannot create a Linux netns/cgroup child off Linux.
func Launch(spec LaunchSpec) (*exec.Cmd, error) {
	return nil, ErrUnsupportedPlatform
}

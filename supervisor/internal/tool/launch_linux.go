//go:build linux

package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// LauncherAvailable selects util-linux setpriv when its ambient-capability
// support is new enough; the shipped helper remains the fallback for systems
// whose setpriv cannot express the pinned securebits transition.
func LauncherAvailable() (mode string, err error) {
	setprivOutput, setprivErr := exec.Command("setpriv", "--version").CombinedOutput()
	if setprivErr == nil {
		if major, minor, ok := launchParseSetprivVersion(string(setprivOutput)); ok &&
			(major > 2 || (major == 2 && minor >= 33)) {
			return "setpriv", nil
		}
	}

	nativeInfo, nativeErr := os.Stat(launchNativePath)
	if nativeErr == nil && !nativeInfo.IsDir() && nativeInfo.Mode()&0o111 != 0 {
		return "native", nil
	}
	return "", fmt.Errorf("tool: no usable cap-transition launcher: setpriv: %v; native: %v", setprivErr, nativeErr)
}

// Launch starts a tool inside its node namespace and cgroup before returning
// the command to the endpoint lifecycle. CgroupFD is preferred because it
// gives clone3 atomic placement; every successful direct child is registered
// immediately so the process-wide subreaper leaves its exit status to the
// caller's cmd.Wait(). The caller must remove the PID from Registry after
// Wait returns; this ownership split is what prevents the subreaper from
// stealing the direct child's exit status.
func Launch(spec LaunchSpec) (*exec.Cmd, error) {
	if spec.Binary == "" {
		return nil, fmt.Errorf("tool: launch target is empty")
	}

	mode, err := LauncherAvailable()
	if err != nil {
		return nil, err
	}
	useCgroupFD := spec.CgroupFD != nil
	if !useCgroupFD && spec.CgroupPath == "" {
		return nil, fmt.Errorf("tool: cgroup placement unavailable: no cgroup fd or path")
	}

	if !useCgroupFD {
		// os/exec has no safe arbitrary pre-exec hook in Go's threaded runtime.
		// Without a usable CgroupFD, launch through the native helper with its
		// --cgroup path: it writes its pid while still root, before dropping
		// privileges and execveing the target. Launching setpriv un-caged would
		// let the tool allocate outside its limits, so it is a hard failure path.
		mode = "native"
	}

	inner := launchTransitionArgv(mode, spec, !useCgroupFD)
	cmd := launchBuildCommand(spec, NetnsExecArgs(spec.NodeID, inner), useCgroupFD)
	if err := cmd.Start(); err == nil {
		// Registry.Add must remain immediately after Start: this direct child is
		// owned by its caller's cmd.Wait, not by the supervisor subreaper loop.
		Registry.Add(cmd.Process.Pid)
		return cmd, nil
	} else if !useCgroupFD {
		return nil, fmt.Errorf("tool: start %s launcher: %w", mode, err)
	} else {
		firstErr := err
		// A failed UseCgroupFD start can mean an older kernel or an unavailable
		// fd placement path. Do not attempt a Go-side cgroup.procs write: Go
		// offers no safe arbitrary pre-exec hook from its threaded runtime.
		// Retry only through the native helper, which writes its own pid to
		// --cgroup while root and before its privilege transition/final execve.
		// An un-caged setpriv retry is forbidden because limits must bind before
		// the target can allocate.
		if spec.CgroupPath == "" {
			return nil, fmt.Errorf("tool: cgroup-fd launch failed: %w; native fallback requires cgroup path", firstErr)
		}
		mode = "native"
		fallback := launchTransitionArgv(mode, spec, true)
		fallbackCmd := launchBuildCommand(spec, NetnsExecArgs(spec.NodeID, fallback), false)
		if fallbackErr := fallbackCmd.Start(); fallbackErr != nil {
			return nil, fmt.Errorf("tool: cgroup-fd launch failed: %w; native cgroup fallback failed: %v", firstErr, fallbackErr)
		}
		// Registry.Add must remain immediately after Start for the fallback too;
		// cmd.Wait owns this direct child's status from this point onward.
		Registry.Add(fallbackCmd.Process.Pid)
		return fallbackCmd, nil
	}
}

// launchTransitionArgv chooses only the transition portion; namespace entry
// is assembled by Launch through the shared NetnsExecArgs contract.
func launchTransitionArgv(mode string, spec LaunchSpec, withCgroup bool) []string {
	if mode == "native" {
		return launchNativeArgv(spec, withCgroup)
	}
	return launchSetprivArgv(spec)
}

// launchBuildCommand keeps process attributes identical for setpriv and the
// native fallback, including a private process group for bounded shutdown.
func launchBuildCommand(spec LaunchSpec, argv []string, useCgroupFD bool) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = ScrubEnv(launchEnvMap(spec.Env))
	cmd.Dir = spec.WorkDir
	procAttr := &syscall.SysProcAttr{Setpgid: true}
	if useCgroupFD {
		procAttr.CgroupFD = int(spec.CgroupFD.Fd())
		procAttr.UseCgroupFD = true
	}
	cmd.SysProcAttr = procAttr
	return cmd
}

// launchParseSetprivVersion extracts the first dotted numeric version from
// util-linux's human-readable --version output without depending on its exact
// wording across distributions.
func launchParseSetprivVersion(output string) (major, minor int, ok bool) {
	for _, field := range strings.Fields(output) {
		parts := strings.Split(field, ".")
		if len(parts) < 2 {
			continue
		}
		parsedMajor, majorErr := strconv.Atoi(parts[0])
		parsedMinor, minorErr := strconv.Atoi(parts[1])
		if majorErr == nil && minorErr == nil {
			return parsedMajor, parsedMinor, true
		}
	}
	return 0, 0, false
}

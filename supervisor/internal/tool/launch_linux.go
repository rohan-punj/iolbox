//go:build linux

package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// launchVerifyTimeout bounds the one-shot setpriv verification so a wedged
// launcher cannot stall the first tool-node start indefinitely.
const launchVerifyTimeout = 5 * time.Second

// launchLauncherSelector holds the process-wide memoized decision. It is a
// package-level singleton because the verification below runs a real
// subprocess and Launch consults LauncherAvailable on every start.
var launchLauncherSelector = &launchSelector{probe: launchProbeLaunchers}

// LauncherAvailable selects util-linux setpriv only when the pinned
// securebits/ambient transition has been empirically verified on this host;
// the shipped helper remains the fallback for systems whose setpriv cannot
// express that transition.
//
// A version check alone is NOT a sufficient signal and must not be restored as
// the only test. The target appliance (Debian 12 bookworm, util-linux setpriv
// 2.38.1 — comfortably past the 2.33 ambient-caps floor) rejects the pinned
// invocation at runtime with `setpriv: unknown capability "cap_net_raw"`:
// whether a given setpriv build can name a capability depends on how it was
// compiled and which capability table it was linked against, which is a
// build/distribution property, not a version property. The P0 spike
// (docs/tests/p0-spike.sh) already selected its launcher empirically for this
// reason and logged the native fallback on every run against this appliance.
// Trusting the version number here would make every real tool launch fail at
// exec time with no diagnosis pointing at the launcher.
//
// The decision is memoized for the process lifetime; see launchSelector.
func LauncherAvailable() (mode string, err error) {
	return launchLauncherSelector.selectMode()
}

// launchProbeLaunchers performs the uncached work behind LauncherAvailable:
// verify setpriv for real, then fall back to the shipped helper.
func launchProbeLaunchers() (string, error) {
	return launchSelectMode(launchVerifySetpriv(), launchVerifyNative())
}

// launchVerifySetpriv gates on the ambient-caps version floor first because a
// version that predates the feature cannot possibly work and a subprocess is
// not worth spending, then proves the pinned transition by running it.
func launchVerifySetpriv() error {
	output, err := exec.Command("setpriv", "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("tool: setpriv --version: %w", err)
	}
	major, minor, ok := launchParseSetprivVersion(string(output))
	if !ok {
		return fmt.Errorf("tool: setpriv version is unparseable: %q", strings.TrimSpace(string(output)))
	}
	if major < 2 || (major == 2 && minor < 33) {
		return fmt.Errorf("tool: setpriv %d.%d predates ambient capability support", major, minor)
	}
	return launchVerifySetprivTransition()
}

// launchVerifySetprivTransition runs the production setpriv argv against a
// minimal target that asserts its own resulting capability state, so a build
// that parses the flags but cannot apply them (or cannot even name
// cap_net_raw) is caught here instead of at the first real tool launch. The
// probe deliberately goes through setpriv as the ioltool user exactly the way
// production does; an unprivileged or namespace-free shortcut would report
// success on a host where the real transition fails.
func launchVerifySetprivTransition() error {
	ctx, cancel := context.WithTimeout(context.Background(), launchVerifyTimeout)
	defer cancel()

	argv := launchSetprivProbeArgv()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = ScrubEnv(launchEnvMap([]string{"PATH=/usr/bin:/bin"}))
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("tool: setpriv did not deliver the pinned cap transition: %w", err)
		}
		return fmt.Errorf("tool: setpriv did not deliver the pinned cap transition: %w: %s", err, message)
	}
	return nil
}

// launchSetprivProbeArgv builds the verification argv from the production
// transition builder so the probe can never diverge from what a real launch
// executes.
func launchSetprivProbeArgv() []string {
	return launchSetprivArgv(LaunchSpec{
		Binary: "/bin/sh",
		// detectProbeCapabilityScript is reused rather than duplicated: the
		// launcher and the startup capability matrix must assert the identical
		// raw-only final state, and two copies would be free to drift apart.
		Args: []string{"-c", detectProbeCapabilityScript},
	})
}

// launchVerifyNative keeps the original presence/executability contract for
// the shipped helper.
func launchVerifyNative() error {
	info, err := os.Stat(launchNativePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("tool: native launcher %s is a directory", launchNativePath)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("tool: native launcher %s is not executable", launchNativePath)
	}
	return nil
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

	inner := launchTransitionArgv(mode, spec, !useCgroupFD, -1)
	cmd := launchBuildCommand(spec, NetnsExecArgs(spec.NodeID, inner), useCgroupFD, nil)
	// StartAndAdd holds the registry lock across the fork+exec and the PID
	// registration: this direct child is owned by its caller's cmd.Wait, not by
	// the supervisor subreaper loop, and the loop must not be able to observe it
	// as an unregistered orphan in between.
	if err := Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid }); err == nil {
		return cmd, nil
	} else if !useCgroupFD {
		return nil, fmt.Errorf("tool: start %s launcher: %w", mode, err)
	} else {
		firstErr := err
		// A failed UseCgroupFD start can mean an older kernel, an unavailable
		// fd placement path, or (proven on real Apple Silicon hardware, where
		// clone3's CLONE_INTO_CGROUP does not take effect through this
		// process's Rosetta-translated syscalls) a host whose clone3 simply
		// never places the child despite returning success upstream. Do not
		// attempt a Go-side cgroup.procs write: Go offers no safe arbitrary
		// pre-exec hook from its threaded runtime. Retry only through the
		// native helper, which writes its own pid while still root and before
		// its privilege transition/final execve. Prefer handing it the same
		// already-open cgroup dir fd (via ExtraFiles + --cgroup-fd): this
		// process runs wrapped in `ip netns exec`, whose fresh mount
		// namespace for /sys hides any cgroup2 path from the parent
		// namespace, so a path-based --cgroup write reliably fails "no such
		// file or directory" for a directory that plainly exists. The fd
		// route has no such problem — it resolves via the fd table, not a
		// path lookup. Fall back to the path only if no fd was ever opened.
		// An un-caged setpriv retry is forbidden because limits must bind
		// before the target can allocate.
		if spec.CgroupFD == nil && spec.CgroupPath == "" {
			return nil, fmt.Errorf("tool: cgroup-fd launch failed: %w; native fallback requires a cgroup fd or path", firstErr)
		}
		mode = "native"
		var fallback []string
		var fallbackExtraFD *os.File
		if spec.CgroupFD != nil {
			fallback = launchTransitionArgv(mode, spec, true, 3)
			fallbackExtraFD = spec.CgroupFD
		} else {
			fallback = launchTransitionArgv(mode, spec, true, -1)
		}
		fallbackCmd := launchBuildCommand(spec, NetnsExecArgs(spec.NodeID, fallback), false, fallbackExtraFD)
		// The fallback registers under the same lock for the same reason;
		// cmd.Wait owns this direct child's status from this point onward.
		if fallbackErr := Registry.StartAndAdd(fallbackCmd.Start, func() int { return fallbackCmd.Process.Pid }); fallbackErr != nil {
			return nil, fmt.Errorf("tool: cgroup-fd launch failed: %w; native cgroup fallback failed: %v", firstErr, fallbackErr)
		}
		return fallbackCmd, nil
	}
}

// launchTransitionArgv chooses only the transition portion; namespace entry
// is assembled by Launch through the shared NetnsExecArgs contract.
// cgroupFDArg is forwarded to launchNativeArgv unchanged (-1 for path-based
// or no-cgroup); it has no effect on the setpriv mode, which never carries a
// cgroup flag of its own (see Launch: setpriv is only selected when
// useCgroupFD placed the process before exec, via SysProcAttr).
func launchTransitionArgv(mode string, spec LaunchSpec, withCgroup bool, cgroupFDArg int) []string {
	if mode == "native" {
		return launchNativeArgv(spec, withCgroup, cgroupFDArg)
	}
	return launchSetprivArgv(spec)
}

// launchBuildCommand keeps process attributes identical for setpriv and the
// native fallback, including a private process group for bounded shutdown.
// extraCgroupFD, when non-nil, is inherited by the child as the lowest
// available fd (ExtraFiles start at 3) so the native helper can join the
// cgroup via /proc/self/fd/N instead of a path lookup — see launchNativeArgv.
func launchBuildCommand(spec LaunchSpec, argv []string, useCgroupFD bool, extraCgroupFD *os.File) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = ScrubEnv(launchEnvMap(spec.Env))
	cmd.Dir = spec.WorkDir
	procAttr := &syscall.SysProcAttr{Setpgid: true}
	if useCgroupFD {
		procAttr.CgroupFD = int(spec.CgroupFD.Fd())
		procAttr.UseCgroupFD = true
	}
	cmd.SysProcAttr = procAttr
	if extraCgroupFD != nil {
		cmd.ExtraFiles = []*os.File{extraCgroupFD}
	}
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

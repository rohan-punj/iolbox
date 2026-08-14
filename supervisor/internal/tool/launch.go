package tool

import (
	"fmt"
	"strings"
	"sync"
)

const launchNativePath = "/opt/iolbox/iolbox-toollaunch"

// launchSelector memoizes one launcher decision per supervisor process. The
// empirical setpriv verification behind probe costs a real subprocess, and
// LauncherAvailable is consulted on every tool-node launch, so the verified
// answer (success or failure) is computed once and reused. Keeping the type
// portable lets tests exercise the caching contract with an injected probe on
// any platform.
type launchSelector struct {
	once  sync.Once
	probe func() (string, error)
	mode  string
	err   error
}

// selectMode returns the memoized launcher decision, running probe at most
// once. Failures are cached too: a launcher that could not be verified must
// not be re-probed per launch and must not become usable by retrying.
func (s *launchSelector) selectMode() (string, error) {
	s.once.Do(func() {
		if s.probe == nil {
			s.mode, s.err = "", fmt.Errorf("tool: launcher probe is not configured")
			return
		}
		s.mode, s.err = s.probe()
	})
	return s.mode, s.err
}

// launchSelectMode applies the fail-closed launcher rule: setpriv is chosen
// only when it has been verified to actually perform the pinned transition,
// the shipped native helper is the fallback, and an unverifiable pair is an
// error rather than an optimistic "try setpriv anyway" path.
func launchSelectMode(setprivErr, nativeErr error) (string, error) {
	if setprivErr == nil {
		return "setpriv", nil
	}
	if nativeErr == nil {
		return "native", nil
	}
	return "", fmt.Errorf("tool: no usable cap-transition launcher: setpriv: %v; native: %v", setprivErr, nativeErr)
}

// ScrubEnv keeps a pack's process environment deliberately small so a pack
// cannot use inherited supervisor settings to steer imports or discover
// unrelated runtime state. The caller supplies both allowlisted base values
// and the per-node IOLBOX_* additions; the result follows the frozen
// allowlist order for reproducible exec environments.
func ScrubEnv(extra map[string]string) []string {
	env := make([]string, 0, len(ScrubbedEnvAllowlist))
	for _, name := range ScrubbedEnvAllowlist {
		value, ok := extra[name]
		if ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// capFlagValue turns a manifest-style ambient-cap list ("NET_RAW",
// "NET_ADMIN", ...) into setpriv's "-all,+cap_x,+cap_y" flag value, lowercase
// and cap_-prefixed as setpriv expects. Empty/nil caps produces "-all" (drop
// everything, grant nothing) rather than silently keeping any capability.
func capFlagValue(caps []string) string {
	parts := make([]string, 0, len(caps)+1)
	parts = append(parts, "-all")
	for _, c := range caps {
		parts = append(parts, "+cap_"+strings.ToLower(c))
	}
	return strings.Join(parts, ",")
}

// capListValue turns the same list into the native helper's comma-separated
// "cap_x,cap_y" form (no leading -all — the helper's own bounding-set starts
// empty by construction, see tools/iolbox-toollaunch). Empty/nil caps
// produces "" (no capabilities).
func capListValue(caps []string) string {
	parts := make([]string, 0, len(caps))
	for _, c := range caps {
		parts = append(parts, "cap_"+strings.ToLower(c))
	}
	return strings.Join(parts, ",")
}

// launchSetprivArgv builds the pinned util-linux transition. Keeping this
// builder free of Linux syscalls lets portable tests verify the security-
// sensitive flag order even though process construction is Linux-only.
func launchSetprivArgv(spec LaunchSpec) []string {
	capFlag := capFlagValue(spec.AmbientCaps)
	argv := []string{
		"setpriv",
		"--reuid", "ioltool",
		"--regid", "ioltool",
		"--clear-groups",
		"--no-new-privs",
		"--bounding-set", capFlag,
		"--inh-caps", capFlag,
		"--ambient-caps", capFlag,
		"--",
		spec.Binary,
	}
	return append(argv, spec.Args...)
}

// launchNativeArgv builds the standalone helper invocation. The helper must
// see --cgroup before the transition flags so it can place itself while still
// root; after the separator, the target receives the exact requested argv.
//
// withCgroup also carries --netns, and the caller must NOT additionally wrap
// this argv in NetnsExecArgs: this is exactly the cgroup-fallback case (the
// clone3(CLONE_INTO_CGROUP) placement failed to start), and the helper joins
// the namespace itself, before the uid switch, instead of relying on `ip
// netns exec`. `ip netns exec` unshares the mount namespace and remounts
// /sys to reflect the target netns' interfaces, which shadows the cgroup2
// mount at /sys/fs/cgroup within that mount namespace — so a cgroup.procs
// write attempted from inside it fails with ENOENT even though the path
// exists in the root mount namespace. See tools/iolbox-toollaunch's joinNetns
// for the setns side of this.
func launchNativeArgv(spec LaunchSpec, withCgroup bool) []string {
	argv := []string{launchNativePath}
	if withCgroup {
		argv = append(argv, "--cgroup", spec.CgroupPath, "--netns", spec.Netns)
	}
	argv = append(argv, "--user", "ioltool", "--caps", capListValue(spec.AmbientCaps), "--", spec.Binary)
	return append(argv, spec.Args...)
}

// launchEnvMap converts exec-style entries into the map accepted by ScrubEnv;
// malformed entries are ignored because they cannot represent an environment
// assignment and must never broaden the allowlist.
func launchEnvMap(entries []string) map[string]string {
	extra := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		extra[name] = value
	}
	return extra
}

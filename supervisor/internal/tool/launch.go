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

// launchSetprivArgv builds the pinned util-linux transition. Keeping this
// builder free of Linux syscalls lets portable tests verify the security-
// sensitive flag order even though process construction is Linux-only.
func launchSetprivArgv(spec LaunchSpec) []string {
	argv := []string{
		"setpriv",
		"--reuid", "ioltool",
		"--regid", "ioltool",
		"--clear-groups",
		"--no-new-privs",
		"--bounding-set", "-all,+cap_net_raw",
		"--inh-caps", "-all,+cap_net_raw",
		"--ambient-caps", "-all,+cap_net_raw",
		"--",
		spec.Binary,
	}
	return append(argv, spec.Args...)
}

// launchNativeArgv builds the standalone helper invocation. The helper must
// see --cgroup before the transition flags so it can place itself while still
// root; after the separator, the target receives the exact requested argv.
func launchNativeArgv(spec LaunchSpec, withCgroup bool) []string {
	argv := []string{launchNativePath}
	if withCgroup {
		argv = append(argv, "--cgroup", spec.CgroupPath)
	}
	argv = append(argv, "--user", "ioltool", "--caps", "cap_net_raw", "--", spec.Binary)
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

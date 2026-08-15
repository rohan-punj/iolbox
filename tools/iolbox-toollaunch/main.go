// iolbox-toollaunch performs the native capability transition for a tool GUI.
// The supervisor invokes it from inside the target network namespace, normally
// through `ip netns exec`; this binary does not create or enter a netns itself.
// The supervisor may already have placed the process in the target cgroup with
// clone3, or may pass --cgroup so this process joins it before dropping the
// authority needed to write cgroup.procs.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const usageText = "usage: iolbox-toollaunch [--cgroup PATH | --cgroup-fd N] --user USER --caps cap_net_raw[,cap_net_admin,...] -- TARGET [ARGS...]"

// knownCapNumbers is the portable name->Linux-capability-number map (plain
// integers, not syscalls, so this stays buildable on every platform even
// though only launch_linux.go's capApply/PR_CAP_AMBIENT_RAISE calls actually
// use the numbers). Keep this in sync with supervisor/internal/tool.AllowedCaps
// (uppercase, no cap_ prefix there) — every name the supervisor can request
// must resolve here or the native launcher rejects it outright.
var knownCapNumbers = map[string]int{
	"cap_net_raw":   13,
	"cap_net_admin": 12,
}

// parseCapsList validates a comma-separated --caps value against
// knownCapNumbers. An empty string is valid (zero capabilities — most tool
// packs declare caps:[] and get none at all).
func parseCapsList(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var caps []string
	for _, name := range strings.Split(value, ",") {
		if _, ok := knownCapNumbers[name]; !ok {
			return nil, fmt.Errorf("--caps: unknown capability %q", name)
		}
		caps = append(caps, name)
	}
	return caps, nil
}

const (
	launchExitUsage       = 2
	launchExitCgroup      = 10
	launchExitLinuxOnly   = 20
	launchExitLookupUser  = 30
	launchExitParseUID    = 31
	launchExitParseGID    = 32
	launchExitCapGet      = 33
	launchExitStartCaps   = 34
	launchExitNoNewPrivs  = 35
	launchExitSecurebits  = 36
	launchExitBoundingSet = 37
	launchExitSetgroups   = 38
	launchExitSetgid      = 39
	launchExitSetuid      = 40
	launchExitCapset      = 41
	launchExitAmbient     = 42
	launchExitExec        = 43
)

type launchOptions struct {
	cgroup   string
	cgroupFD int // -1 means unset; see parseLaunchArgs
	user     string
	caps     []string
	target   string
	args     []string
}

// launchFailure carries a machine-distinguishable failure step while keeping
// the underlying errno or lookup detail in the message sent to stderr.
type launchFailure struct {
	code int
	step string
	err  error
}

func (e *launchFailure) Error() string {
	return fmt.Sprintf("tool: %s: %v", e.step, e.err)
}

func (e *launchFailure) Unwrap() error {
	return e.err
}

func newLaunchFailure(code int, step string, err error) error {
	return &launchFailure{code: code, step: step, err: err}
}

func main() {
	opts, err := parseLaunchArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "iolbox-toollaunch: tool: %v\n%s\n", err, usageText)
		os.Exit(launchExitUsage)
	}

	if err := launchAs(opts.user, opts.target, opts.args, opts.cgroup, opts.cgroupFD, opts.caps); err != nil {
		code := launchExitExec
		var failure *launchFailure
		if errors.As(err, &failure) {
			code = failure.code
		}
		fmt.Fprintf(os.Stderr, "iolbox-toollaunch: %v\n", err)
		os.Exit(code)
	}
}

func parseLaunchArgs(argv []string) (launchOptions, error) {
	opts := launchOptions{cgroupFD: -1}
	separator := -1
	seenUser := false
	seenCaps := false
	seenCgroup := false
	seenCgroupFD := false

	for i := 0; i < len(argv); i++ {
		if argv[i] == "--" {
			separator = i
			break
		}
		switch argv[i] {
		case "--cgroup":
			if seenCgroup || seenCgroupFD || i+1 >= len(argv) || argv[i+1] == "--" || argv[i+1] == "" {
				return launchOptions{}, errors.New("--cgroup requires one non-empty path before -- and cannot combine with --cgroup-fd")
			}
			opts.cgroup = argv[i+1]
			seenCgroup = true
			i++
		case "--cgroup-fd":
			// FD-based placement exists because this process is normally
			// invoked wrapped in `ip netns exec`, which unshares a fresh mount
			// namespace for its /sys view — that new namespace has no
			// visibility into a cgroup2 path created moments earlier in the
			// parent's mount namespace (reproduced on real Apple Silicon
			// hardware: --cgroup PATH fails "no such file or directory" for a
			// directory that plainly exists outside the netns-exec'd child).
			// A file descriptor inherited via ExtraFiles has no such problem:
			// it resolves through the kernel's per-process fd table, not a
			// path lookup in the current mount namespace, so /proc/self/fd/N
			// reaches the cgroup regardless of what the caller's `ip netns
			// exec` did to /sys.
			if seenCgroup || seenCgroupFD || i+1 >= len(argv) || argv[i+1] == "--" || argv[i+1] == "" {
				return launchOptions{}, errors.New("--cgroup-fd requires one non-empty fd number before -- and cannot combine with --cgroup")
			}
			fd, err := strconv.Atoi(argv[i+1])
			if err != nil || fd < 0 {
				return launchOptions{}, fmt.Errorf("--cgroup-fd: invalid fd number %q", argv[i+1])
			}
			opts.cgroupFD = fd
			seenCgroupFD = true
			i++
		case "--user":
			if seenUser || i+1 >= len(argv) || argv[i+1] == "--" || argv[i+1] == "" {
				return launchOptions{}, errors.New("--user requires one non-empty name before --")
			}
			opts.user = argv[i+1]
			seenUser = true
			i++
		case "--caps":
			if seenCaps || i+1 >= len(argv) || argv[i+1] == "--" {
				return launchOptions{}, errors.New("--caps requires a value before --")
			}
			caps, err := parseCapsList(argv[i+1])
			if err != nil {
				return launchOptions{}, err
			}
			opts.caps = caps
			seenCaps = true
			i++
		default:
			return launchOptions{}, fmt.Errorf("unknown option %q", argv[i])
		}
	}

	if separator < 0 {
		return launchOptions{}, errors.New("missing -- before target")
	}
	if !seenUser {
		return launchOptions{}, errors.New("--user is required")
	}
	if !seenCaps {
		return launchOptions{}, errors.New("--caps is required (comma-separated cap_* names, or empty)")
	}
	if separator+1 >= len(argv) || argv[separator+1] == "" {
		return launchOptions{}, errors.New("target is required after --")
	}

	opts.target = argv[separator+1]
	opts.args = argv[separator+2:]
	return opts, nil
}

// writeCgroupMembership must run before launchTransition. At this point the
// process is still root and still holds the capabilities needed to write a
// delegated cgroup.procs; after capset/setuid it no longer has that authority.
// The target inherits this membership through the final execve, so its limits
// bind before it can allocate. Absent path and fd is intentional: the
// supervisor has already placed the process through SysProcAttr.CgroupFD in
// that case (the clone3 CLONE_INTO_CGROUP path, used when it works).
//
// cgroupFD (>= 0) takes precedence over cgroupPath when both happen to be
// set, and is what the supervisor actually sends in practice: this process
// normally runs wrapped in `ip netns exec`, whose fresh mount namespace for
// /sys hides any cgroup2 path created in the parent namespace, so a path-based
// write reliably fails "no such file or directory" even though the directory
// exists. /proc/self/fd/N sidesteps that: it resolves via this process's own
// fd table, which survives both execve and the mount-namespace switch.
func writeCgroupMembership(cgroupPath string, cgroupFD int) error {
	var procsPath string
	switch {
	case cgroupFD >= 0:
		procsPath = filepath.Join("/proc/self/fd", strconv.Itoa(cgroupFD), "cgroup.procs")
	case cgroupPath != "":
		procsPath = filepath.Join(cgroupPath, "cgroup.procs")
	default:
		return nil
	}

	pid := []byte(strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(procsPath, pid, 0); err != nil {
		return newLaunchFailure(launchExitCgroup, "cgroup placement", fmt.Errorf("write %s: %w", procsPath, err))
	}
	return nil
}

func launchAs(user, target string, args []string, cgroupPath string, cgroupFD int, caps []string) error {
	if user == "" || target == "" {
		return newLaunchFailure(launchExitUsage, "arguments", errors.New("user and target are required"))
	}
	if err := writeCgroupMembership(cgroupPath, cgroupFD); err != nil {
		return err
	}
	return launchTransition(user, target, args, caps)
}

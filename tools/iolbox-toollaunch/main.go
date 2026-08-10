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
)

const usageText = "usage: iolbox-toollaunch [--cgroup PATH] --user USER --caps cap_net_raw -- TARGET [ARGS...]"

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
	cgroup string
	user   string
	target string
	args   []string
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

	if err := launchAs(opts.user, opts.target, opts.args, opts.cgroup); err != nil {
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
	var opts launchOptions
	separator := -1
	seenUser := false
	seenCaps := false
	seenCgroup := false

	for i := 0; i < len(argv); i++ {
		if argv[i] == "--" {
			separator = i
			break
		}
		switch argv[i] {
		case "--cgroup":
			if seenCgroup || i+1 >= len(argv) || argv[i+1] == "--" || argv[i+1] == "" {
				return launchOptions{}, errors.New("--cgroup requires one non-empty path before --")
			}
			opts.cgroup = argv[i+1]
			seenCgroup = true
			i++
		case "--user":
			if seenUser || i+1 >= len(argv) || argv[i+1] == "--" || argv[i+1] == "" {
				return launchOptions{}, errors.New("--user requires one non-empty name before --")
			}
			opts.user = argv[i+1]
			seenUser = true
			i++
		case "--caps":
			if seenCaps || i+1 >= len(argv) || argv[i+1] != "cap_net_raw" {
				return launchOptions{}, errors.New("--caps must be exactly cap_net_raw")
			}
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
		return launchOptions{}, errors.New("--caps cap_net_raw is required")
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
// bind before it can allocate. An absent path is intentional: the supervisor
// has already placed the process through SysProcAttr.CgroupFD in that case.
func writeCgroupMembership(cgroupPath string) error {
	if cgroupPath == "" {
		return nil
	}

	procsPath := filepath.Join(cgroupPath, "cgroup.procs")
	pid := []byte(strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(procsPath, pid, 0); err != nil {
		return newLaunchFailure(launchExitCgroup, "cgroup placement", fmt.Errorf("write %s: %w", procsPath, err))
	}
	return nil
}

func launchAs(user, target string, args []string, cgroupPath string) error {
	if user == "" || target == "" {
		return newLaunchFailure(launchExitUsage, "arguments", errors.New("user and target are required"))
	}
	if err := writeCgroupMembership(cgroupPath); err != nil {
		return err
	}
	return launchTransition(user, target, args)
}

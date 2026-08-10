//go:build linux

package tool

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The probe ID is deliberately far outside the lab node range while keeping
// vtoolp<id> within Linux's 15-character interface-name limit. Production
// constructors are used with this ID so the probe exercises the same names,
// ordering, and teardown paths as a real tool endpoint.
const detectProbeNodeID = 9000000

const detectProbeCapabilityScript = `
want=2000
for name in CapEff CapPrm CapInh CapAmb CapBnd; do
    value=$(sed -n "s/^${name}:[[:space:]]*//p" /proc/self/status)
    value=$(printf '%s' "$value" | sed 's/^0*//')
    [ "$value" = "$want" ] || exit 41
done
exit 0
`

const detectProbeWaitTimeout = 5 * time.Second

// detectProbeSocketTimeout bounds both the listener deadline and the dial of
// the AF_UNIX handshake. detectProbeAcceptTimeout is the backstop for waiting
// on the accept goroutine; it must exceed the listener deadline so a legitimate
// deadline expiry is reported as an accept failure rather than a probe hang.
const (
	detectProbeSocketTimeout = time.Second
	detectProbeAcceptTimeout = 3 * detectProbeSocketTimeout
)

type detectProbeUser struct {
	uid int
	gid int
}

// Detect runs the production namespace, veth, cgroup, cap-transition, and
// AF_UNIX operations against a throwaway sibling of the delegated root. The
// matrix fails closed whenever setup, the final-state assertion, or cleanup
// verification fails, because a leaked probe would make later starts less
// trustworthy than the failed detection itself.
func Detect(root CgroupRoot) Capabilities {
	return detectCapabilitiesFromResults(detectRunProbe(root))
}

func detectRunProbe(root CgroupRoot) map[string]detectStepResult {
	results := make(map[string]detectStepResult, len(detectProbeSteps))
	probeID := detectProbeNodeID

	probeUser, userErr := detectLookupUser()

	preexistingNetns, netnsListErr := detectProbeNetnsPresent(probeID)
	var netnsErr error
	if netnsListErr != nil {
		netnsErr = netnsListErr
	} else if preexistingNetns {
		netnsErr = fmt.Errorf("tool: probe netns %s already exists", NetnsName(probeID))
	} else {
		netnsErr = CreateNetns(probeID)
	}
	netnsCreated := netnsErr == nil
	if netnsErr != nil && !preexistingNetns {
		// `ip netns add` is normally atomic, but retain cleanup ownership if
		// a platform reports an error after installing the namespace.
		netnsCreated, _ = detectProbeNetnsPresent(probeID)
	}
	if netnsErr != nil {
		results["netnsCreate"] = detectStepResult{reason: detectProbeReason("netnsCreate", netnsErr)}
	} else {
		results["netnsCreate"] = detectStepResult{ok: true}
	}

	vethCreated := false
	if netnsErr != nil {
		results["vethCreate"] = detectStepResult{reason: detectProbeDependencyReason("vethCreate", "netnsCreate")}
		results["vethMoveRename"] = detectStepResult{reason: detectProbeDependencyReason("vethMoveRename", "netnsCreate")}
	} else {
		_, hostVethErr := os.Lstat(filepath.Join("/sys/class/net", HostVethName(probeID)))
		var vethErr error
		switch {
		case hostVethErr == nil:
			vethErr = fmt.Errorf("tool: probe host veth %s already exists", HostVethName(probeID))
		case !detectIsNotExist(hostVethErr):
			vethErr = fmt.Errorf("tool: inspect probe host veth: %w", hostVethErr)
		default:
			vethErr = CreateVethPair(probeID)
		}
		vethCreated = vethErr == nil
		if vethErr != nil && hostVethErr != nil && detectIsNotExist(hostVethErr) {
			// CreateVethPair has several kernel operations. If a later one
			// fails after `ip link add`, the root-side device still needs
			// teardown even though the constructor returned an error.
			vethCreated, _ = detectProbeHostVethPresence(probeID)
		}
		if vethErr != nil {
			results["vethCreate"] = detectStepResult{reason: detectProbeReason("vethCreate", vethErr)}
			results["vethMoveRename"] = detectStepResult{reason: detectProbeDependencyReason("vethMoveRename", "vethCreate")}
		} else if hostErr := detectProbeHostVeth(probeID); hostErr != nil {
			results["vethCreate"] = detectStepResult{reason: detectProbeReason("vethCreate", hostErr)}
			results["vethMoveRename"] = detectStepResult{reason: detectProbeDependencyReason("vethMoveRename", "vethCreate")}
		} else {
			results["vethCreate"] = detectStepResult{ok: true}
			if moveErr := detectProbeGuestVeth(probeID); moveErr != nil {
				results["vethMoveRename"] = detectStepResult{reason: detectProbeReason("vethMoveRename", moveErr)}
			} else {
				results["vethMoveRename"] = detectStepResult{ok: true}
			}
		}
	}

	var cagePath string
	var cageFD *os.File
	var cageErr error
	if root.Delegated == "" || root.SupervisorLeaf == "" {
		cageErr = fmt.Errorf("tool: delegated cgroup root is not initialized")
	} else {
		candidatePath := filepath.Join(root.Delegated, CageName(probeID))
		if _, err := os.Lstat(candidatePath); err == nil {
			cageErr = fmt.Errorf("tool: probe cage %s already exists", candidatePath)
		} else if !detectIsNotExist(err) {
			cageErr = fmt.Errorf("tool: inspect probe cage %s: %w", candidatePath, err)
		} else {
			cagePath = candidatePath
			_, cageFD, cageErr = CreateCage(root, probeID, DefaultLimits())
			if cageErr != nil {
				if _, statErr := os.Lstat(cagePath); detectIsNotExist(statErr) {
					cagePath = ""
				}
			}
		}
	}
	if cageErr != nil {
		results["cgroupDelegated"] = detectStepResult{reason: detectProbeReason("cgroupDelegated", cageErr)}
	} else {
		results["cgroupDelegated"] = detectStepResult{ok: true}
	}

	if userErr != nil {
		results["ambientCapTransition"] = detectStepResult{reason: detectProbeReason("ambientCapTransition", userErr)}
	} else if netnsErr != nil {
		results["ambientCapTransition"] = detectStepResult{reason: detectProbeDependencyReason("ambientCapTransition", "netnsCreate")}
	} else if !vethCreated {
		results["ambientCapTransition"] = detectStepResult{reason: detectProbeDependencyReason("ambientCapTransition", "vethCreate")}
	} else if cageErr != nil {
		results["ambientCapTransition"] = detectStepResult{reason: detectProbeDependencyReason("ambientCapTransition", "cgroupDelegated")}
	} else {
		launchErr := detectProbeTransition(probeID, cagePath, cageFD)
		if launchErr != nil {
			results["ambientCapTransition"] = detectStepResult{reason: detectProbeReason("ambientCapTransition", launchErr)}
		} else {
			results["ambientCapTransition"] = detectStepResult{ok: true}
		}
	}

	unixErr := detectProbeUnixSocket(probeID, probeUser, userErr)
	if unixErr != nil {
		results["unixProxy"] = detectStepResult{reason: detectProbeReason("unixProxy", unixErr)}
	} else {
		results["unixProxy"] = detectStepResult{ok: true}
	}

	for key, reason := range detectProbeCleanup(probeID, netnsCreated, vethCreated, cagePath, cageFD) {
		result := results[key]
		result.ok = false
		result.reason = reason
		results[key] = result
	}
	return results
}

func detectLookupUser() (detectProbeUser, error) {
	account, err := user.Lookup("ioltool")
	if err != nil {
		return detectProbeUser{}, fmt.Errorf("tool: lookup ioltool account: %w", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return detectProbeUser{}, fmt.Errorf("tool: parse ioltool uid %q: %w", account.Uid, err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return detectProbeUser{}, fmt.Errorf("tool: parse ioltool gid %q: %w", account.Gid, err)
	}
	return detectProbeUser{uid: uid, gid: gid}, nil
}

func detectProbeReason(key string, err error) string {
	for _, step := range detectProbeSteps {
		if step.key == key {
			if err == nil {
				return step.reason
			}
			return step.reason + ": " + err.Error()
		}
	}
	return "tool: capability probe failed: " + err.Error()
}

func detectProbeDependencyReason(key, dependency string) string {
	return detectProbeReason(key, fmt.Errorf("tool: prerequisite %s failed", dependency))
}

func detectProbeHostVeth(nodeID int) error {
	path := filepath.Join("/sys/class/net", HostVethName(nodeID))
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("tool: host veth %s is absent: %w", HostVethName(nodeID), err)
	}
	return nil
}

func detectProbeHostVethPresence(nodeID int) (bool, error) {
	_, err := os.Lstat(filepath.Join("/sys/class/net", HostVethName(nodeID)))
	if err == nil {
		return true, nil
	}
	if detectIsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("tool: inspect host veth %s: %w", HostVethName(nodeID), err)
}

func detectProbeGuestVeth(nodeID int) error {
	var out bytes.Buffer
	cmd := exec.Command("ip", "netns", "exec", NetnsName(nodeID), "ip", "link", "show", "dev", GuestIface)
	cmd.Stdout = &out
	cmd.Stderr = &out
	// Started via Registry.StartAndAdd, not CombinedOutput: a fast `ip` command
	// can exit before a separate registration step would run, letting the
	// subreaper reap it out from under this function's own Wait (the same race
	// found in runCmds/runCmdsBestEffort on real hardware).
	err := Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid })
	if err == nil {
		err = cmd.Wait()
		Registry.Remove(cmd.Process.Pid)
	}
	if err != nil {
		message := strings.TrimSpace(out.String())
		if message == "" {
			return fmt.Errorf("tool: inspect guest veth %s: %w", GuestIface, err)
		}
		return fmt.Errorf("tool: inspect guest veth %s: %w: %s", GuestIface, err, message)
	}
	return nil
}

func detectProbeTransition(nodeID int, cagePath string, cageFD *os.File) error {
	cmd, err := Launch(LaunchSpec{
		NodeID:     nodeID,
		Netns:      NetnsName(nodeID),
		CgroupFD:   cageFD,
		CgroupPath: cagePath,
		Binary:     "/bin/sh",
		Args:       []string{"-c", detectProbeCapabilityScript},
		Env:        []string{"PATH=/usr/bin:/bin"},
		User:       "ioltool",
		AmbientCaps: []string{
			"cap_net_raw",
		},
	})
	if err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(detectProbeWaitTimeout)
	defer timer.Stop()
	select {
	case waitErr := <-done:
		Registry.Remove(cmd.Process.Pid)
		if waitErr != nil {
			return fmt.Errorf("tool: capability assertion exited unsuccessfully: %w", waitErr)
		}
		return nil
	case <-timer.C:
		killErr := KillCage(cagePath)
		waitErr := <-done
		Registry.Remove(cmd.Process.Pid)
		if killErr != nil {
			return fmt.Errorf("tool: capability assertion timed out and cage kill failed: %w", killErr)
		}
		if waitErr != nil {
			return fmt.Errorf("tool: capability assertion timed out: %w", waitErr)
		}
		return fmt.Errorf("tool: capability assertion timed out")
	}
}

func detectProbeUnixSocket(nodeID int, owner detectProbeUser, lookupErr error) (probeErr error) {
	if lookupErr != nil {
		return fmt.Errorf("tool: cannot establish ioltool-owned socket directory: %w", lookupErr)
	}

	directory := SocketDir("", nodeID)
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("tool: create socket parent %s: %w", parent, err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		return fmt.Errorf("tool: set socket parent mode: %w", err)
	}
	if err := os.Chown(parent, 0, 0); err != nil {
		return fmt.Errorf("tool: set socket parent ownership: %w", err)
	}
	if _, err := os.Lstat(directory); err == nil {
		return fmt.Errorf("tool: probe socket directory %s already exists", directory)
	} else if !detectIsNotExist(err) {
		return fmt.Errorf("tool: inspect socket directory %s: %w", directory, err)
	}

	if err := os.Mkdir(directory, 0o700); err != nil {
		return fmt.Errorf("tool: create socket directory %s: %w", directory, err)
	}
	cleanupErr := func() error {
		if err := os.RemoveAll(directory); err != nil {
			return fmt.Errorf("tool: remove probe socket directory %s: %w", directory, err)
		}
		if _, err := os.Lstat(directory); err == nil {
			return fmt.Errorf("tool: probe socket directory %s remains", directory)
		} else if !detectIsNotExist(err) {
			return fmt.Errorf("tool: verify probe socket directory removal: %w", err)
		}
		return nil
	}
	defer func() {
		if err := cleanupErr(); err != nil && probeErr == nil {
			probeErr = err
		}
	}()

	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("tool: set socket directory mode: %w", err)
	}
	if err := os.Chown(directory, owner.uid, owner.gid); err != nil {
		return fmt.Errorf("tool: set socket directory ownership: %w", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("tool: stat socket directory: %w", err)
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("tool: socket directory mode is %o, want 700", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != owner.uid || int(stat.Gid) != owner.gid {
		return fmt.Errorf("tool: socket directory ownership is not ioltool")
	}

	return detectProbeSocketHandshake(filepath.Join(directory, "probe.sock"))
}

// detectProbeSocketHandshake binds an AF_UNIX listener, dials it, and requires
// both sides of the handshake to succeed.
//
// The accept result must be collected before the listener is closed. Closing a
// listener makes an in-flight Accept return "use of closed network connection",
// so closing first races the accept goroutine and reports a handshake that
// genuinely completed on the wire as a probe failure.
func detectProbeSocketHandshake(socketPath string) error {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("tool: bind probe AF_UNIX socket: %w", err)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		_ = unixListener.SetDeadline(time.Now().Add(detectProbeSocketTimeout))
	}

	acceptResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if connection != nil {
			_ = connection.Close()
		}
		acceptResult <- acceptErr
	}()

	connection, dialErr := net.DialTimeout("unix", socketPath, detectProbeSocketTimeout)
	if connection != nil {
		_ = connection.Close()
	}

	// The listener deadline bounds Accept even when the dial failed, so this
	// wait always completes. The timer is only a backstop for a platform that
	// refuses the deadline: closing the listener then releases Accept, which is
	// exactly the shutdown the success path must avoid doing early.
	listenerClosed := false
	var acceptErr error
	select {
	case acceptErr = <-acceptResult:
	case <-time.After(detectProbeAcceptTimeout):
		_ = listener.Close()
		listenerClosed = true
		acceptErr = <-acceptResult
	}

	var closeErr error
	if !listenerClosed {
		closeErr = listener.Close()
	}

	if dialErr != nil {
		return fmt.Errorf("tool: dial probe AF_UNIX socket: %w", dialErr)
	}
	if acceptErr != nil {
		return fmt.Errorf("tool: accept probe AF_UNIX socket: %w", acceptErr)
	}
	if closeErr != nil {
		return fmt.Errorf("tool: close probe AF_UNIX socket: %w", closeErr)
	}
	return nil
}

func detectProbeCleanup(nodeID int, netnsCreated, vethCreated bool, cagePath string, cageFD *os.File) map[string]string {
	failures := make(map[string]string)
	if cagePath != "" {
		if err := KillCage(cagePath); err != nil {
			failures["cgroupDelegated"] = detectProbeReason("cgroupDelegated", fmt.Errorf("cleanup kill: %w", err))
			failures["ambientCapTransition"] = detectProbeReason("ambientCapTransition", fmt.Errorf("cleanup kill: %w", err))
		}
		if err := WaitCageEmpty(cagePath, time.Second); err != nil {
			failures["cgroupDelegated"] = detectProbeReason("cgroupDelegated", fmt.Errorf("cleanup wait: %w", err))
			failures["ambientCapTransition"] = detectProbeReason("ambientCapTransition", fmt.Errorf("cleanup wait: %w", err))
		}
		if cageFD != nil {
			if err := cageFD.Close(); err != nil {
				failures["cgroupDelegated"] = detectProbeReason("cgroupDelegated", fmt.Errorf("cleanup close: %w", err))
			}
		}
		if err := RemoveCage(cagePath); err != nil {
			failures["cgroupDelegated"] = detectProbeReason("cgroupDelegated", fmt.Errorf("cleanup remove: %w", err))
		}
		if _, err := os.Lstat(cagePath); err == nil {
			failures["cgroupDelegated"] = detectProbeReason("cgroupDelegated", fmt.Errorf("cleanup cgroup %s remains", cagePath))
		} else if !detectIsNotExist(err) {
			failures["cgroupDelegated"] = detectProbeReason("cgroupDelegated", fmt.Errorf("cleanup verify cgroup: %w", err))
		}
	}

	if netnsCreated {
		_ = DeleteNetns(nodeID)
		present, err := detectProbeNetnsPresent(nodeID)
		if err != nil {
			failures["netnsCreate"] = detectProbeReason("netnsCreate", fmt.Errorf("cleanup verify netns: %w", err))
		} else if present {
			failures["netnsCreate"] = detectProbeReason("netnsCreate", fmt.Errorf("cleanup netns %s remains", NetnsName(nodeID)))
		}
	}
	if vethCreated {
		_ = DeleteVeth(nodeID)
		// A missing sysfs entry is the expected success case; only a stat
		// error that is not "does not exist" means absence could not be
		// verified. detectProbeHostVethPresence performs that classification
		// on the raw stat error, mirroring the netns block above.
		present, err := detectProbeHostVethPresence(nodeID)
		if err != nil {
			failures["vethCreate"] = detectProbeReason("vethCreate", fmt.Errorf("cleanup verify veth: %w", err))
			failures["vethMoveRename"] = detectProbeReason("vethMoveRename", fmt.Errorf("cleanup verify veth: %w", err))
		} else if present {
			failures["vethCreate"] = detectProbeReason("vethCreate", fmt.Errorf("cleanup veth %s remains", HostVethName(nodeID)))
			failures["vethMoveRename"] = detectProbeReason("vethMoveRename", fmt.Errorf("cleanup veth %s remains", HostVethName(nodeID)))
		}
	}
	return failures
}

func detectProbeNetnsPresent(nodeID int) (bool, error) {
	var out bytes.Buffer
	cmd := exec.Command("ip", "netns", "list")
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid })
	if err == nil {
		err = cmd.Wait()
		Registry.Remove(cmd.Process.Pid)
	}
	if err != nil {
		message := strings.TrimSpace(out.String())
		if message == "" {
			return false, fmt.Errorf("tool: list network namespaces: %w", err)
		}
		return false, fmt.Errorf("tool: list network namespaces: %w: %s", err, message)
	}
	name := NetnsName(nodeID)
	for _, line := range strings.Split(out.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == name || strings.HasPrefix(trimmed, name+" ") {
			return true, nil
		}
	}
	return false, nil
}

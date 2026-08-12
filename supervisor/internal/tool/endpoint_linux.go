//go:build linux

package tool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const (
	endpointReadinessTimeout = 10 * time.Second
	endpointProbeInterval    = 100 * time.Millisecond
	endpointLivenessInterval = 5 * time.Second
	endpointLivenessFailures = 3
	endpointStopTimeout      = 5 * time.Second
	endpointSocketName       = "gui.sock"
)

// Endpoint owns the kernel objects and GUI process for one tool node. Its
// state is deliberately kept here rather than in the child process: bridge
// attachment and health supervision must be able to change independently of
// the pack GUI, which is what makes hot-connect safe.
type Endpoint struct {
	endpointMu sync.Mutex

	endpointCfg          Config
	endpointState        string
	endpointPID          int
	endpointCmd          *exec.Cmd
	endpointWaitDone     chan struct{}
	endpointLivenessStop chan struct{}
	endpointLivenessOnce sync.Once
	endpointLivenessWG   sync.WaitGroup
	endpointTeardownOnce sync.Once

	endpointInstanceID string
	endpointCagePath   string
	endpointCageFD     *os.File
	endpointSocketDir  string
	endpointSocketPath string
	endpointBridge     string
}

// Start creates the cage, namespace, veth, socket directory, options file,
// and GUI in that order. The durable object record is written before each
// kernel or filesystem object is created, so a hard supervisor death leaves a
// later instance enough information to clean the node without guessing.
func Start(cfg Config) (*Endpoint, error) {
	cfg = endpointConfigDefaults(cfg)
	instanceID := cfg.InstanceID
	if instanceID == "" {
		var err error
		instanceID, err = InstanceID(cfg.StateDir)
		if err != nil {
			return nil, err
		}
	}
	uid, gid, err := endpointLookupUser(cfg.User)
	if err != nil {
		return nil, err
	}

	e := &Endpoint{
		endpointCfg:          cfg,
		endpointState:        "starting",
		endpointInstanceID:   instanceID,
		endpointCagePath:     filepath.Join(cfg.Root.Delegated, CageName(cfg.NodeID)),
		endpointSocketDir:    SocketDir(cfg.RunDir, cfg.NodeID),
		endpointSocketPath:   filepath.Join(SocketDir(cfg.RunDir, cfg.NodeID), endpointSocketName),
		endpointWaitDone:     make(chan struct{}),
		endpointLivenessStop: make(chan struct{}),
	}

	// A previous supervisor may have been killed while the node was running.
	// The cleanup pass is best-effort, with the same single delayed retry used
	// by extnet when a stale holder briefly keeps a device or cgroup busy.
	if err := e.endpointPreclean(); err != nil {
		return nil, err
	}

	if err := e.endpointRecordObject(); err != nil {
		return nil, err
	}
	if _, e.endpointCageFD, err = CreateCage(cfg.Root, cfg.NodeID, cfg.Limits); err != nil {
		return nil, e.endpointStartFailure(err)
	}

	if err := e.endpointRecordObject(); err != nil {
		return nil, e.endpointStartFailure(err)
	}
	if err := CreateNetns(cfg.NodeID); err != nil {
		return nil, e.endpointStartFailure(err)
	}

	if err := e.endpointRecordObject(); err != nil {
		return nil, e.endpointStartFailure(err)
	}
	if err := CreateVethPair(cfg.NodeID); err != nil {
		return nil, e.endpointStartFailure(err)
	}
	if cfg.Net != nil {
		if err := AssignAddr(cfg.NodeID, *cfg.Net); err != nil {
			return nil, e.endpointStartFailure(err)
		}
	}

	if err := e.endpointRecordObject(); err != nil {
		return nil, e.endpointStartFailure(err)
	}
	if err := endpointPrepareSocketDir(e.endpointSocketDir, uid, gid); err != nil {
		return nil, e.endpointStartFailure(err)
	}
	if err := endpointWriteOptions(e.endpointSocketDir, cfg.Options, uid, gid); err != nil {
		return nil, e.endpointStartFailure(err)
	}

	launchSpec := e.endpointLaunchSpec()
	cmd, err := Launch(launchSpec)
	if err != nil {
		return nil, e.endpointStartFailure(err)
	}
	e.endpointMu.Lock()
	e.endpointCmd = cmd
	e.endpointPID = cmd.Process.Pid
	e.endpointMu.Unlock()
	_ = e.endpointCageFD.Close()
	e.endpointCageFD = nil

	go e.endpointWatchExit(cmd, cmd.Process.Pid)
	if err := e.endpointAwaitReadiness(); err != nil {
		return nil, e.endpointStartFailure(err)
	}

	e.endpointMu.Lock()
	if e.endpointState != "starting" {
		state := e.endpointState
		e.endpointMu.Unlock()
		return nil, e.endpointStartFailure(fmt.Errorf("tool: GUI became %s before readiness", state))
	}
	e.endpointState = "running"
	e.endpointMu.Unlock()

	e.endpointLivenessWG.Add(1)
	go e.endpointWatchLiveness()
	return e, nil
}

// Stop marks the endpoint stopped before signalling its process group. That
// ordering is essential: the exit watcher must classify the expected SIGTERM
// as a normal shutdown, not as a crash, before teardown removes the cage.
func (e *Endpoint) Stop() error {
	e.endpointMu.Lock()
	if e.endpointState == "stopped" {
		e.endpointMu.Unlock()
		e.endpointTeardown()
		return nil
	}
	e.endpointState = "stopped"
	pid := e.endpointPID
	waitDone := e.endpointWaitDone
	e.endpointMu.Unlock()

	e.endpointStopLiveness()
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
	if waitDone != nil {
		timer := time.NewTimer(endpointStopTimeout)
		select {
		case <-waitDone:
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}

	// A GUI can leave descendants behind. cgroup.kill is intentionally the
	// escalation path because cgroup membership, unlike argv matching, names
	// exactly the subtree owned by this endpoint.
	if e.endpointCageNeedsKill() {
		_ = KillCage(e.endpointCagePath)
	}
	e.endpointTeardown()
	return nil
}

// AttachBridge changes only the root-side veth membership. The GUI process and
// its PID are untouched, which is the hot-connect seam used by fabric.
func (e *Endpoint) AttachBridge(br string) error {
	e.endpointMu.Lock()
	if e.endpointState == "stopped" || e.endpointState == "crashed" {
		e.endpointMu.Unlock()
		return nil
	}
	if e.endpointBridge == br {
		e.endpointMu.Unlock()
		return nil
	}
	previous := e.endpointBridge
	e.endpointMu.Unlock()
	if previous != "" {
		e.DetachBridge()
	}
	if err := AttachVethToBridge(e.endpointCfg.NodeID, br); err != nil {
		return err
	}
	e.endpointMu.Lock()
	e.endpointBridge = br
	e.endpointMu.Unlock()
	return nil
}

// DetachBridge removes bridge membership only. It is safe to call during
// teardown even when the in-memory bridge name was lost during a crash.
func (e *Endpoint) DetachBridge() {
	_ = DetachVethFromBridge(e.endpointCfg.NodeID)
	e.endpointMu.Lock()
	e.endpointBridge = ""
	e.endpointMu.Unlock()
}

// State reports the lifecycle state used by the server and fabric paths.
func (e *Endpoint) State() string {
	e.endpointMu.Lock()
	defer e.endpointMu.Unlock()
	return e.endpointState
}

// PID returns the GUI process ID, or zero before launch.
func (e *Endpoint) PID() int {
	e.endpointMu.Lock()
	defer e.endpointMu.Unlock()
	return e.endpointPID
}

// HostVeth returns the deterministic root-network-namespace veth name.
func (e *Endpoint) HostVeth() string { return HostVethName(e.endpointCfg.NodeID) }

// SocketPath returns the AF_UNIX socket path used by the tool GUI.
func (e *Endpoint) SocketPath() string { return e.endpointSocketPath }

// CLISocketPath returns the deterministic private PC CLI socket path. The
// launcher only exposes it to a child when Config.CLISocket is true.
func (e *Endpoint) CLISocketPath() string {
	return CLISocketFile(e.endpointCfg.RunDir, e.endpointCfg.NodeID)
}

func (e *Endpoint) endpointLaunchSpec() LaunchSpec {
	env := make([]string, 0, 10)
	for _, name := range []string{"PATH", "HOME", "LANG", "PYTHONHOME", "PYTHONPATH"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	env = append(env,
		"IOLBOX_TOOL_SOCK="+e.endpointSocketPath,
		"IOLBOX_TOOL_OPTIONS="+OptionsFile(e.endpointCfg.RunDir, e.endpointCfg.NodeID),
		"IOLBOX_PACK_DIR="+e.endpointCfg.Pack.Root,
		"IOLBOX_NODE_ID="+strconv.Itoa(e.endpointCfg.NodeID),
	)
	if e.endpointCfg.CLISocket {
		env = append(env, "IOLBOX_PC_CLI_SOCK="+CLISocketFile(e.endpointCfg.RunDir, e.endpointCfg.NodeID))
	}
	// Ambient caps are exactly what this pack's manifest declared (already
	// validated as a subset of AllowedCaps by manifestCheckCaps at load
	// time), not a blanket "every pack gets NET_RAW" grant — most packs
	// declare caps:[] and get none at all.
	return LaunchSpec{
		NodeID:      e.endpointCfg.NodeID,
		Netns:       NetnsName(e.endpointCfg.NodeID),
		CgroupFD:    e.endpointCageFD,
		CgroupPath:  e.endpointCagePath,
		Binary:      e.endpointCfg.Pack.GUIBin,
		Env:         env,
		User:        e.endpointCfg.User,
		AmbientCaps: e.endpointCfg.Pack.Manifest.Caps,
		WorkDir:     e.endpointCfg.Pack.Root,
	}
}

func (e *Endpoint) endpointRecordObject() error {
	return RecordObject(e.endpointCfg.StateDir, e.endpointInstanceID, ObjectRecord{
		NodeID:     e.endpointCfg.NodeID,
		CgroupPath: e.endpointCagePath,
		Netns:      NetnsName(e.endpointCfg.NodeID),
		HostVeth:   HostVethName(e.endpointCfg.NodeID),
		MgmtVeth:   "",
		SocketDir:  e.endpointSocketDir,
	})
}

func (e *Endpoint) endpointPreclean() error {
	err := e.endpointTeardownPass()
	if !errors.Is(err, syscall.EBUSY) {
		return err
	}
	time.Sleep(750 * time.Millisecond)
	if retryErr := e.endpointTeardownPass(); retryErr != nil {
		return err
	}
	return nil
}

func (e *Endpoint) endpointStartFailure(err error) error {
	e.endpointTeardown()
	return fmt.Errorf("tool: start endpoint: %w", err)
}

func (e *Endpoint) endpointWatchExit(cmd *exec.Cmd, pid int) {
	waitErr := cmd.Wait()
	// Registry.Remove must be the first action after Wait returns. The
	// supervisor-wide reaper uses this ownership bit to avoid stealing the
	// direct child's exit status.
	Registry.Remove(pid)
	close(e.endpointWaitDone)

	e.endpointMu.Lock()
	expected := e.endpointState == "stopped"
	if !expected && e.endpointState != "crashed" {
		e.endpointState = "crashed"
	}
	e.endpointMu.Unlock()
	if !expected && waitErr != nil {
		e.endpointTeardown()
	} else if !expected {
		e.endpointTeardown()
	}
}

func (e *Endpoint) endpointAwaitReadiness() error {
	deadline := time.Now().Add(endpointReadinessTimeout)
	var lastStatus int
	var lastErr error
	for {
		e.endpointMu.Lock()
		state := e.endpointState
		e.endpointMu.Unlock()
		if state == "crashed" || state == "stopped" {
			return fmt.Errorf("tool: GUI exited before readiness (state %s)", state)
		}
		status, err := e.endpointProbeHealth()
		lastStatus, lastErr = status, err
		if endpointReadinessFlip([]int{status}) {
			return nil
		}
		if !time.Now().Before(deadline) {
			if lastErr != nil {
				return fmt.Errorf("tool: GUI readiness timed out after %s: %w", endpointReadinessTimeout, lastErr)
			}
			return fmt.Errorf("tool: GUI readiness timed out after %s with status %d", endpointReadinessTimeout, lastStatus)
		}
		remaining := time.Until(deadline)
		if remaining > endpointProbeInterval {
			remaining = endpointProbeInterval
		}
		time.Sleep(remaining)
	}
}

func (e *Endpoint) endpointProbeHealth() (int, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: endpointProbeInterval}
			return dialer.DialContext(ctx, "unix", e.endpointSocketPath)
		},
	}
	client := &http.Client{Transport: transport, Timeout: endpointProbeInterval}
	defer client.CloseIdleConnections()
	request, err := http.NewRequest(http.MethodGet, "http://iolbox"+e.endpointCfg.Pack.Manifest.GUI.Health, nil)
	if err != nil {
		return 0, fmt.Errorf("tool: create health request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("tool: probe %s: %w", e.endpointCfg.Pack.Manifest.GUI.Health, err)
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

func (e *Endpoint) endpointWatchLiveness() {
	defer e.endpointLivenessWG.Done()
	ticker := time.NewTicker(endpointLivenessInterval)
	defer ticker.Stop()
	statuses := make([]int, 0, endpointLivenessFailures)
	for {
		select {
		case <-e.endpointLivenessStop:
			return
		case <-ticker.C:
			status, err := e.endpointProbeHealth()
			if err != nil {
				status = 0
			}
			statuses = append(statuses, status)
			if len(statuses) > endpointLivenessFailures {
				statuses = statuses[len(statuses)-endpointLivenessFailures:]
			}
			if endpointLivenessTrip(statuses) {
				e.endpointMu.Lock()
				if e.endpointState == "running" {
					e.endpointState = "crashed"
				}
				e.endpointMu.Unlock()
				_ = KillCage(e.endpointCagePath)
				e.endpointTeardown()
				return
			}
		}
	}
}

func (e *Endpoint) endpointStopLiveness() {
	e.endpointLivenessOnce.Do(func() { close(e.endpointLivenessStop) })
}

func (e *Endpoint) endpointCageNeedsKill() bool {
	if e.endpointCagePath == "" {
		return false
	}
	populated, err := CagePopulated(e.endpointCagePath)
	return err != nil || populated
}

func (e *Endpoint) endpointTeardown() {
	e.endpointTeardownOnce.Do(func() {
		e.endpointStopLiveness()
		_ = e.endpointTeardownPass()
	})
}

func (e *Endpoint) endpointTeardownPass() error {
	var teardownErr error
	if e.endpointCagePath != "" {
		populated, populatedErr := CagePopulated(e.endpointCagePath)
		if populatedErr != nil && !errors.Is(populatedErr, os.ErrNotExist) {
			teardownErr = errors.Join(teardownErr, populatedErr)
		}
		if populated || (populatedErr != nil && !errors.Is(populatedErr, os.ErrNotExist)) {
			if err := KillCage(e.endpointCagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				teardownErr = errors.Join(teardownErr, err)
			}
			if err := WaitCageEmpty(e.endpointCagePath, endpointStopTimeout); err != nil && !errors.Is(err, os.ErrNotExist) {
				teardownErr = errors.Join(teardownErr, err)
			}
		}
	}

	// Kill has already happened above. The kernel resources are removed in the
	// exact reverse of their creation order; the socket directory is removed
	// last as the pinned stop order requires.
	for _, step := range endpointTeardownSteps() {
		switch step {
		case "veth":
			e.DetachBridge()
			_ = DeleteVeth(e.endpointCfg.NodeID)
		case "netns":
			_ = DeleteNetns(e.endpointCfg.NodeID)
		case "cgroup":
			if err := RemoveCage(e.endpointCagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				teardownErr = errors.Join(teardownErr, err)
			}
		}
	}
	if e.endpointSocketDir != "" {
		if err := os.RemoveAll(e.endpointSocketDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			teardownErr = errors.Join(teardownErr, fmt.Errorf("tool: remove socket directory: %w", err))
		}
	}
	if e.endpointInstanceID != "" {
		if err := PruneObject(e.endpointCfg.StateDir, e.endpointInstanceID, e.endpointCfg.NodeID); err != nil {
			teardownErr = errors.Join(teardownErr, err)
		}
	}
	if e.endpointCageFD != nil {
		_ = e.endpointCageFD.Close()
		e.endpointCageFD = nil
	}
	return teardownErr
}

func endpointConfigDefaults(cfg Config) Config {
	if cfg.StateDir == "" {
		cfg.StateDir = "/var/lib/iolbox"
	}
	if cfg.RunDir == "" {
		cfg.RunDir = "/run/iolbox"
	}
	if cfg.User == "" {
		cfg.User = "ioltool"
	}
	if cfg.Limits == (Limits{}) {
		if cfg.Pack.Manifest.Limits != nil {
			cfg.Limits = *cfg.Pack.Manifest.Limits
		} else {
			cfg.Limits = DefaultLimits()
		}
	}
	return cfg
}

func endpointLookupUser(name string) (uid, gid int, err error) {
	account, err := user.Lookup(name)
	if err != nil {
		return 0, 0, fmt.Errorf("tool: lookup launch user %q: %w", name, err)
	}
	uid, err = strconv.Atoi(account.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("tool: invalid uid for launch user %q: %w", name, err)
	}
	gid, err = strconv.Atoi(account.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("tool: invalid gid for launch user %q: %w", name, err)
	}
	return uid, gid, nil
}

func endpointPrepareSocketDir(socketDir string, uid, gid int) error {
	parent := filepath.Dir(socketDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("tool: create socket parent %q: %w", parent, err)
	}
	if err := os.Chown(parent, 0, 0); err != nil {
		return fmt.Errorf("tool: own socket parent %q: %w", parent, err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		return fmt.Errorf("tool: mode socket parent %q: %w", parent, err)
	}
	if err := os.Mkdir(socketDir, 0o700); err != nil {
		return fmt.Errorf("tool: create socket directory %q: %w", socketDir, err)
	}
	if err := os.Chown(socketDir, uid, gid); err != nil {
		return fmt.Errorf("tool: own socket directory %q: %w", socketDir, err)
	}
	if err := os.Chmod(socketDir, 0o700); err != nil {
		return fmt.Errorf("tool: mode socket directory %q: %w", socketDir, err)
	}
	return nil
}

func endpointWriteOptions(socketDir string, options []byte, uid, gid int) error {
	payload := endpointOptionsPayload(options)
	temporary, err := os.CreateTemp(socketDir, ".options-*.tmp")
	if err != nil {
		return fmt.Errorf("tool: create options temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("tool: write options temporary file: %w", err)
	}
	if err := temporary.Chown(uid, gid); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("tool: own options temporary file: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("tool: mode options temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("tool: sync options temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("tool: close options temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(socketDir, "options.json")); err != nil {
		return fmt.Errorf("tool: replace options file: %w", err)
	}
	removeTemporary = false
	return nil
}

func endpointOptionsPayload(options []byte) []byte {
	if len(options) == 0 {
		return []byte("{}")
	}
	return append([]byte(nil), options...)
}

func endpointSetupSteps() []string {
	return []string{"cgroup", "netns", "veth"}
}

func endpointTeardownSteps() []string {
	setup := endpointSetupSteps()
	steps := make([]string, len(setup))
	for index := range setup {
		steps[len(setup)-index-1] = setup[index]
	}
	return steps
}

func endpointReadinessFlip(statuses []int) bool {
	return len(statuses) > 0 && statuses[len(statuses)-1] == http.StatusOK
}

func endpointLivenessTrip(statuses []int) bool {
	if len(statuses) < endpointLivenessFailures {
		return false
	}
	for _, status := range statuses[len(statuses)-endpointLivenessFailures:] {
		if status == http.StatusOK {
			return false
		}
	}
	return true
}

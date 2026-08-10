// Package tool implements a tool node: a supervised process tree inside a
// network namespace and cgroup-v2 cage, running with ambient CAP_NET_RAW only.
// IOL and NAT use the other two models in the system—a spawned-pty process
// (IOL) and a process-less tap with NAT (NAT)—while a tool node hosts a pack's
// own GUI and scripts in its cage. The privileged data plane is implemented by
// _linux files; _other files provide the unsupported-platform stubs.
package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// Kind identifies a tool endpoint.
type Kind string

const (
	// KindTool is a supervised tool-pack process tree.
	KindTool Kind = "tool"
)

// ErrUnsupportedPlatform is returned when a tool endpoint needs Linux kernel
// facilities that are not available on the current platform.
var ErrUnsupportedPlatform = errors.New("tool: tool endpoints are only supported on linux")

// NetnsName returns the deterministic network-namespace name for a node. The
// node ID is kept within the deployment's IFNAMSIZ budget when this name is
// used alongside the corresponding interface names.
func NetnsName(nodeID int) string { return fmt.Sprintf("iolt%d", nodeID) }

// HostVethName returns the root-network-namespace veth name for a node. Linux
// interface names have a 15-character IFNAMSIZ budget, so callers must use
// node IDs whose decimal form fits after the vtool prefix.
func HostVethName(nodeID int) string { return fmt.Sprintf("vtool%d", nodeID) }

// PeerTempName returns the deterministic temporary peer name used while a
// veth is moved into the node namespace. It is subject to the 15-character
// IFNAMSIZ budget and is never the name eth1 in the root namespace.
func PeerTempName(nodeID int) string { return fmt.Sprintf("vtoolp%d", nodeID) }

// MgmtVethName returns the deterministic host-side management-veth name. It
// is used only for the non-unix management fallback and has a 15-character
// IFNAMSIZ budget after the mtool prefix.
func MgmtVethName(nodeID int) string { return fmt.Sprintf("mtool%d", nodeID) }

// CageName returns the deterministic leaf-directory name for a node's cgroup.
// It is a cgroup directory name rather than a Linux interface name, so
// IFNAMSIZ does not constrain it.
func CageName(nodeID int) string { return fmt.Sprintf("tool-%d", nodeID) }

// SocketDir returns the per-node tool socket directory. An empty run root uses
// /run/iolbox; the resulting path is <runRoot>/tool/<id>.
func SocketDir(runRoot string, nodeID int) string {
	if runRoot == "" {
		runRoot = "/run/iolbox"
	}
	return filepath.Join(runRoot, "tool", fmt.Sprintf("%d", nodeID))
}

// GuestIface is the sole data-plane interface inside a unix-transport tool
// namespace.
const GuestIface = "eth1"

// NetnsExecArgs builds the portable argv for executing a command in a tool's
// namespace. It performs no syscall and always allocates a new slice, so this
// contract stays here for both netns setup and the launcher without aliasing
// the caller's argv.
func NetnsExecArgs(nodeID int, argv []string) []string {
	out := make([]string, 0, 4+len(argv))
	out = append(out, "ip", "netns", "exec", NetnsName(nodeID))
	return append(out, argv...)
}

// Limits are the cgroup-v2 resource limits applied to one tool cage.
type Limits struct {
	MemoryMax int64  `json:"memoryMax"`
	PidsMax   int    `json:"pidsMax"`
	CPUMax    string `json:"cpuMax"`
	SwapMax   int64  `json:"swapMax"`
}

// DefaultLimits returns the pinned default cage limits: 2048 MiB of memory,
// 512 processes, two CPUs expressed as a 200000/100000 quota, and no swap.
func DefaultLimits() Limits {
	return Limits{
		MemoryMax: 2048 * 1024 * 1024,
		PidsMax:   512,
		CPUMax:    "200000 100000",
		SwapMax:   0,
	}
}

// ManifestVersion is the supported major version of a tool pack manifest.
const ManifestVersion = 1

// AllowedCaps is the complete manifest capability allowlist. NET_ADMIN is
// intentionally absent: tool packs receive only ambient CAP_NET_RAW.
var AllowedCaps = []string{"NET_RAW"}

// Manifest is the supervisor-side metadata shipped as pack.json. It validates
// node configuration and drives palette display; the supervisor never
// executes from the manifest's modules list—the pack GUI's compiled module
// definitions remain authoritative for runtime behaviour.
type Manifest struct {
	ManifestVersion int      `json:"manifestVersion"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Icon            string   `json:"icon"`
	Interpreter     string   `json:"interpreter"`
	GUI             GUI      `json:"gui"`
	Caps            []string `json:"caps"`
	Options         []Option `json:"options"`
	Groups          []string `json:"groups"`
	Modules         []Module `json:"modules"`
	Limits          *Limits  `json:"limits,omitempty"`
}

// GUI describes the pack's compiled web GUI and its health endpoint.
type GUI struct {
	Bin         string       `json:"bin"`
	Transport   string       `json:"transport"`
	Console     string       `json:"console"`
	Health      string       `json:"health"`
	ProxyRoutes []ProxyRoute `json:"proxyRoutes"`
}

// ProxyRoute declares one path prefix exposed by a tool GUI through the
// supervisor's /tool/{nodeId}/ reverse proxy. WebSocket upgrades are accepted
// only when AllowWS is true for the matching route.
type ProxyRoute struct {
	Prefix  string `json:"prefix"`
	AllowWS bool   `json:"allowWS"`
}

// UnmarshalJSON validates route prefixes while manifests are decoded. The
// manifest validator lives in a separate legacy file outside this batch's
// ownership; decoding-time validation keeps malformed route declarations from
// entering the loaded-pack registry without changing that file.
func (r *ProxyRoute) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Prefix  string `json:"prefix"`
		AllowWS bool   `json:"allowWS"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	prefix := strings.TrimSpace(decoded.Prefix)
	if prefix == "" || !strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("tool: proxy route prefix %q must begin with /", decoded.Prefix)
	}
	decodedPrefix, err := url.PathUnescape(prefix)
	if err != nil || path.Clean(prefix) != prefix || path.Clean(decodedPrefix) != decodedPrefix || strings.Contains(prefix, "?") || strings.Contains(prefix, "#") {
		return fmt.Errorf("tool: proxy route prefix %q must be a clean path", decoded.Prefix)
	}
	for _, part := range strings.Split(decodedPrefix, "/") {
		if part == ".." {
			return fmt.Errorf("tool: proxy route prefix %q may not escape its root", decoded.Prefix)
		}
	}
	decoded.Prefix = prefix
	*r = ProxyRoute(decoded)
	return nil
}

// Module describes one manifest-visible tool module.
type Module struct {
	Key        string      `json:"key"`
	Label      string      `json:"label"`
	Group      string      `json:"group"`
	Script     string      `json:"script"`
	Fields     []Field     `json:"fields"`
	Mitigation *Mitigation `json:"mitigation"`
}

// Field describes one module input.
type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// Mitigation contains the human-readable defensive guidance for a module.
type Mitigation struct {
	Text string `json:"text"`
}

// Option describes one pack-level node option.
type Option struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

// Pack is a loaded, validated pack. GUIBin and Scripts contain absolute,
// canonicalized paths that have already been checked to remain under Root;
// the manifest itself stores pack-relative paths.
type Pack struct {
	ID       string
	Root     string
	Manifest Manifest
	GUIBin   string
	Scripts  map[string]string
}

// ScrubbedEnvAllowlist is the complete environment inherited by a pack GUI.
// An explicit list prevents one pack's Python from being steered to import
// another writable artifact. The option variable is deliberately named
// IOLBOX_TOOL_OPTIONS, not IOLBOX_TOOL_OPTS: the proven P0 stub GUI reads
// IOLBOX_TOOL_OPTIONS and exits non-zero when it or IOLBOX_TOOL_SOCK is empty.
var ScrubbedEnvAllowlist = []string{
	"PATH",
	"HOME",
	"LANG",
	"PYTHONHOME",
	"PYTHONPATH",
	"IOLBOX_TOOL_SOCK",
	"IOLBOX_TOOL_OPTIONS",
	"IOLBOX_PACK_DIR",
	"IOLBOX_NODE_ID",
}

// Capabilities reports the six runtime capabilities needed to support tool
// endpoints. Reasons records why an individual capability was unavailable.
type Capabilities struct {
	NetnsCreate          bool
	VethCreate           bool
	VethMoveRename       bool
	CgroupDelegated      bool
	AmbientCapTransition bool
	UnixProxy            bool
	Reasons              map[string]string
}

// OK reports whether every capability required by a tool endpoint passed.
func (c Capabilities) OK() bool {
	return c.NetnsCreate && c.VethCreate && c.VethMoveRename &&
		c.CgroupDelegated && c.AmbientCapTransition && c.UnixProxy
}

// GateFeatures returns the hello feature gate for tool endpoints, advertising
// tools only when all six required capabilities are available.
func (c Capabilities) GateFeatures() []string {
	if c.OK() {
		return []string{"tools"}
	}
	return nil
}

// Supports reports whether this runtime can run the requested node kind.
func (c Capabilities) Supports(k Kind) bool {
	return k == KindTool && c.OK()
}

// Config describes one tool endpoint to bring up. User defaults to ioltool,
// StateDir to /var/lib/iolbox, and RunDir to /run/iolbox when the endpoint
// applies zero-value configuration defaults.
type Config struct {
	NodeID     int
	Pack       Pack
	Limits     Limits
	Root       CgroupRoot
	StateDir   string
	RunDir     string
	User       string
	InstanceID string
	// Net is an optional static address for GuestIface (eth1); nil leaves the
	// interface unaddressed (the long-standing default — see NetAddrConfig).
	Net *NetAddrConfig
	// Options is the raw JSON payload written to the per-node options file
	// before launch. Nil or empty Options means the endpoint writes {}, never
	// leaves the file absent, because the GUI hard-exits when it cannot read it.
	Options []byte
}

// OptionsFile returns the exact per-node options path under the tool socket
// directory. The endpoint must create it owner ioltool:ioltool, mode 0600,
// inside the 0700 ioltool-owned socket directory; the GUI reads and rewrites
// this file at startup.
func OptionsFile(runRoot string, nodeID int) string {
	return filepath.Join(SocketDir(runRoot, nodeID), "options.json")
}

// CgroupRoot describes the supervisor's delegated cgroup and its process leaf.
// Delegated is <D>, the controller-enabling root that holds no processes;
// SupervisorLeaf is <D>/supervisor/.
type CgroupRoot struct {
	Delegated      string
	SupervisorLeaf string
}

// SupervisorLeafName is the exact cgroup leaf used to migrate the supervisor
// process out of its controller-enabling delegated root.
const SupervisorLeafName = "supervisor"

// LaunchSpec is the fully resolved launch request passed to the privileged
// launcher.
type LaunchSpec struct {
	NodeID      int
	Netns       string
	CgroupFD    *os.File
	CgroupPath  string
	Binary      string
	Args        []string
	Env         []string
	User        string
	AmbientCaps []string
	WorkDir     string
}

// ReapConfig contains the install paths and identity needed for stale-object
// cleanup.
type ReapConfig struct {
	Root       CgroupRoot
	StateDir   string
	RunDir     string
	InstanceID string
}

// ObjectRecord records the deterministic kernel and socket objects owned by a
// tool node in the durable state file.
type ObjectRecord struct {
	NodeID     int    `json:"nodeId"`
	CgroupPath string `json:"cgroupPath"`
	Netns      string `json:"netns"`
	HostVeth   string `json:"hostVeth"`
	MgmtVeth   string `json:"mgmtVeth"`
	SocketDir  string `json:"socketDir"`
}

// ObjectState is the on-disk shape of tool-objects.json. Objects is keyed by
// the decimal node ID.
type ObjectState struct {
	InstanceID string                  `json:"instanceId"`
	Objects    map[string]ObjectRecord `json:"objects"`
}

// PIDRegistry tracks direct children whose exec.Cmd.Wait call owns their exit
// status. exec.Cmd.Wait() is authoritative for every direct child; the
// subreaper loop peeks non-destructively with waitid+WNOWAIT and reaps only
// PIDs absent from this registry, so it can never steal a direct child's exit
// status.
//
// PR_SET_CHILD_SUBREAPER is process-wide, so this registry must hold every
// direct exec.Cmd child anywhere in the supervisor—IOL/VPCS, tcpdump capture,
// and every ip/sudo command run by extnet and fabric—not only tool children.
// A separate batch registers those existing sites; this registry is therefore
// supervisor-wide rather than tool-scoped.
type PIDRegistry struct {
	sync.Mutex
	pids map[int]struct{}
}

// NewPIDRegistry returns an empty mutex-guarded direct-child registry.
func NewPIDRegistry() *PIDRegistry {
	return &PIDRegistry{pids: make(map[int]struct{})}
}

// Add records a direct child before its cmd.Wait call can return.
func (r *PIDRegistry) Add(pid int) {
	r.Lock()
	defer r.Unlock()
	r.addLocked(pid)
}

// addLocked records a direct child while the caller already holds r's mutex.
// sync.Mutex is not re-entrant, so every path that already owns the lock—Add
// itself and StartAndAdd—must record through this unexported form rather than
// calling Add.
func (r *PIDRegistry) addLocked(pid int) {
	if r.pids == nil {
		r.pids = make(map[int]struct{})
	}
	r.pids[pid] = struct{}{}
}

// StartAndAdd makes spawning a direct child and registering its PID atomic
// with respect to ReapUnregistered, closing the fork/register race that the
// original "Add is the very next statement after Start" mitigation could not
// close. A command that exits within microseconds—an ordinary `ip link ...`
// invocation does—can be observed as exited by the subreaper's independent
// 10ms poll before the spawning goroutine executes its next statement, at
// which point the loop reaps it as an orphan and the spawner's cmd.Wait fails
// with "waitid: no child processes". This was observed on real hardware during
// a routine link.add, so the window is not negligible.
//
// start is the caller's fork+exec (cmd.Start, or a closure around pty.Start);
// pid reports the PID of the process start created and is called only after
// start reports success, so cmd.Process is guaranteed non-nil there. The lock
// is held across start+register only—never across cmd.Wait—so the registry
// mutex is never held for a command's runtime.
func (r *PIDRegistry) StartAndAdd(start func() error, pid func() int) error {
	r.Lock()
	defer r.Unlock()
	if err := start(); err != nil {
		return err
	}
	if p := pid(); p > 0 {
		r.addLocked(p)
	}
	return nil
}

// ReapUnregistered runs reap for pid only while the registry lock is held and
// pid is absent from it, so a spawner cannot register that PID between the
// check and the destructive collection. reap must be fast and non-blocking
// (the subreaper passes a WNOHANG wait4) because it runs under the lock every
// spawn site contends for.
func (r *PIDRegistry) ReapUnregistered(pid int, reap func(int)) {
	r.Lock()
	defer r.Unlock()
	if _, owned := r.pids[pid]; owned {
		return
	}
	reap(pid)
}

// Remove releases a direct child after its cmd.Wait call returns.
func (r *PIDRegistry) Remove(pid int) {
	r.Lock()
	defer r.Unlock()
	delete(r.pids, pid)
}

// Contains reports whether pid is currently owned by a direct cmd.Wait call.
func (r *PIDRegistry) Contains(pid int) bool {
	r.Lock()
	defer r.Unlock()
	_, ok := r.pids[pid]
	return ok
}

// Len returns the number of direct children currently registered.
func (r *PIDRegistry) Len() int {
	r.Lock()
	defer r.Unlock()
	return len(r.pids)
}

// Registry is the supervisor-wide direct-child registry used by the subreaper.
var Registry = NewPIDRegistry()

// cmdSpec describes one command for the shared tool command runner.
type cmdSpec struct {
	name string
	args []string
}

// runCmds executes commands in order and wraps the first failure with the
// command output so privileged data-plane callers can identify the failed
// kernel operation.
//
// Each command is started and registered atomically via Registry.StartAndAdd
// rather than exec.Command(...).CombinedOutput() (which folds Start+Wait into
// one call with no window to register in between). A short-lived command like
// `ip link ...` can exit before a separate registration step would run, and
// the subreaper loop's independent poll would then reap it out from under
// this function's own Wait -- the exact race found on real hardware when this
// package's own netns/veth commands (its only present callers) ran during a
// routine lab.start.
func runCmds(cmds []cmdSpec) error {
	for _, spec := range cmds {
		var out bytes.Buffer
		cmd := exec.Command(spec.name, spec.args...)
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
				return fmt.Errorf("tool: command %q failed: %w", spec.name, err)
			}
			return fmt.Errorf("tool: command %q failed: %w: %s", spec.name, err, message)
		}
	}
	return nil
}

// runCmdsBestEffort runs every command and deliberately ignores failures so
// teardown can continue past objects that have already disappeared. See
// runCmds for why registration must be atomic with Start.
func runCmdsBestEffort(cmds []cmdSpec) {
	for _, spec := range cmds {
		cmd := exec.Command(spec.name, spec.args...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid }); err == nil {
			_ = cmd.Wait()
			Registry.Remove(cmd.Process.Pid)
		}
	}
}

// fileExists reports whether path can be statted. It is kept portable because
// capability probes and non-Linux stubs share the same package contract.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// contained resolves target and verifies that its canonical absolute path is
// below root. Symlinks and traversal are rejected by the canonical-prefix
// check, which protects pack paths from escaping their immutable root.
func contained(root, target string) (string, bool) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", false
	}
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rootResolved, rootErr := filepath.EvalSymlinks(root)
	targetResolved, targetErr := filepath.EvalSymlinks(target)
	if rootErr == nil && targetErr == nil {
		root = filepath.Clean(rootResolved)
		target = filepath.Clean(targetResolved)
	} else {
		// Some restricted Windows runners deny the final-path query used by
		// EvalSymlinks even for ordinary files. The lexical path is safe here
		// only after checking every component for a symlink, including the
		// root itself; any inability to inspect a component remains a reject.
		rootInfo, rootStatErr := os.Lstat(root)
		if rootStatErr != nil || rootInfo.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
		if !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return "", false
		}
		relative, relativeErr := filepath.Rel(root, target)
		if relativeErr != nil {
			return "", false
		}
		current := root
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, statErr := os.Lstat(current)
			if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
				return "", false
			}
		}
	}
	if !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", false
	}
	return target, true
}

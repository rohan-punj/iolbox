//go:build linux

package fabric

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// cmdTimeout bounds each privileged command so a wedged `sudo` surfaces as a
// clean error instead of blocking the caller indefinitely. Mirrors
// extnet's cmdTimeout.
const cmdTimeout = 20 * time.Second

// Manager creates/destroys Linux taps and bridges and attaches/detaches taps,
// via privileged `ip` commands run as `sudo -n`. It tracks nothing global;
// the caller (the server) owns lifecycle bookkeeping of which taps/bridges
// exist.
type Manager struct{}

// NewManager returns a ready-to-use Manager.
func NewManager() *Manager { return &Manager{} }

// EnsureTap creates the named tap device owned by uid and brings it up. It is
// idempotent: if the device already exists, the create step's failure is
// treated as success and the up step still runs.
func (m *Manager) EnsureTap(ctx context.Context, name string, uid int) error {
	return m.runIdempotent(ctx, opCreateTap, tapCreateCmds(name, uid))
}

// DeleteTap deletes the named tap device, tolerating it already being gone.
func (m *Manager) DeleteTap(ctx context.Context, name string) error {
	return m.runIdempotent(ctx, opDeleteTap, tapDeleteCmds(name))
}

// EnsureBridge creates the named bridge device and brings it up. It is
// idempotent: if the bridge already exists, the create step's failure is
// treated as success and the up step still runs.
func (m *Manager) EnsureBridge(ctx context.Context, name string) error {
	return m.runIdempotent(ctx, opCreateBridge, bridgeCreateCmds(name))
}

// DeleteBridge deletes the named bridge device, tolerating it already being
// gone.
func (m *Manager) DeleteBridge(ctx context.Context, name string) error {
	return m.runIdempotent(ctx, opDeleteBridge, bridgeDeleteCmds(name))
}

// Attach attaches tap to bridge (`ip link set <tap> master <bridge>`) — the
// runtime hot-plug: safe against an already-running IOL instance holding the
// tap open.
func (m *Manager) Attach(ctx context.Context, bridge, tap string) error {
	return m.runIdempotent(ctx, opAttach, attachCmds(bridge, tap))
}

// Detach detaches tap from whatever bridge it is currently a member of.
func (m *Manager) Detach(ctx context.Context, tap string) error {
	return m.runIdempotent(ctx, opDetach, detachCmds(tap))
}

// SetNetem atomically replaces the one flat netem qdisc on dev. Unlike clear,
// failure is never treated as an idempotent no-op: a missing device or qdisc
// indicates a caller bug or an unavailable runtime dependency.
func (m *Manager) SetNetem(ctx context.Context, dev string, n Netem) error {
	return m.runIdempotent(ctx, opNetemSet, netemCmds(dev, n))
}

// ClearNetem removes dev's root qdisc. An absent qdisc or absent device is a
// successful, idempotent no-op because teardown can race endpoint deletion.
func (m *Manager) ClearNetem(ctx context.Context, dev string) error {
	return m.runIdempotent(ctx, opNetemClear, netemClearCmds(dev))
}

// runIdempotent runs each argv in cmds (via runOne — bare if we're already
// root, else `sudo -n`), capturing combined output. A failure on the first
// command of a create/delete pair is tolerated when isBenign classifies its
// output as an idempotent no-op (device already exists / already gone); the
// remaining commands still run so e.g. `up` is applied even when create found
// the device already there. Any other failure aborts and is returned — EXCEPT
// a "sysctl" command (the disable_ipv6 hardening step in tapCreateCmds/
// bridgeCreateCmds), which is best-effort by design: it stops IPv6 background
// noise from leaking into the emulated fabric (see tapCreateCmds's doc), but
// it is not load-bearing for the device actually working, and a runtime
// without IPv6 support at all (sysctl path missing) must not fail every tap/
// bridge creation over a purely cosmetic hardening step.
func (m *Manager) runIdempotent(ctx context.Context, o op, cmds [][]string) error {
	for i, argv := range cmds {
		out, err := runOne(ctx, argv)
		if err != nil {
			if i == 0 && isBenign(o, strings.ToLower(out)) {
				continue
			}
			if len(argv) > 0 && argv[0] == "sysctl" {
				continue
			}
			return fmt.Errorf("fabric: `%s` failed: %v: %s",
				strings.Join(argv, " "), err, strings.TrimSpace(out))
		}
	}
	return nil
}

// runOne executes one argv under cmdTimeout, returning its combined
// stdout+stderr. When the supervisor is already running as root (its normal
// systemd identity), the argv runs directly with no `sudo` wrapper — see
// sudoArgv. Non-root callers (builder smokes as the `iolab` NOPASSWD-sudo
// user) keep the `sudo -n` prefix.
func runOne(ctx context.Context, argv []string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	name, args := sudoArgv(os.Geteuid(), argv)
	out := &bytes.Buffer{}
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	// Start and register atomically: a bridge attach runs several fast `ip`
	// commands, any of which can exit before a separate Add executes, letting
	// the subreaper reap it and fail this Wait with "no child processes".
	err := tool.Registry.StartAndAdd(cmd.Start, func() int { return cmd.Process.Pid })
	if err == nil {
		err = cmd.Wait()
		tool.Registry.Remove(cmd.Process.Pid)
	}
	if cctx.Err() == context.DeadlineExceeded {
		return out.String(), fmt.Errorf("timed out after %s", cmdTimeout)
	}
	return out.String(), err
}

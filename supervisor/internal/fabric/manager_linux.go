//go:build linux

package fabric

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// cmdTimeout bounds each privileged command so a wedged `sudo` surfaces as a
// clean error instead of blocking the caller indefinitely. Mirrors
// extnet's cmdTimeout.
const cmdTimeout = 20 * time.Second

// op identifies which operation is running, so isBenign can apply the right
// idempotency rule for the error text it sees.
type op int

const (
	opCreateTap op = iota
	opDeleteTap
	opCreateBridge
	opDeleteBridge
	opAttach
	opDetach
)

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

// runIdempotent runs each argv in cmds via `sudo -n`, capturing combined
// output. A failure on the first command of a create/delete pair is tolerated
// when isBenign classifies its output as an idempotent no-op (device already
// exists / already gone); the remaining commands still run so e.g. `up` is
// applied even when create found the device already there. Any other failure
// aborts and is returned.
func (m *Manager) runIdempotent(ctx context.Context, o op, cmds [][]string) error {
	for i, argv := range cmds {
		out, err := runOne(ctx, argv)
		if err != nil {
			if i == 0 && isBenign(o, strings.ToLower(out)) {
				continue
			}
			return fmt.Errorf("fabric: `sudo -n %s` failed: %v: %s",
				strings.Join(argv, " "), err, strings.TrimSpace(out))
		}
	}
	return nil
}

// runOne executes one argv as `sudo -n <argv...>` under cmdTimeout, returning
// its combined stdout+stderr.
func runOne(ctx context.Context, argv []string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	full := append([]string{"-n"}, argv...)
	out := &bytes.Buffer{}
	cmd := exec.CommandContext(cctx, "sudo", full...)
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	if cctx.Err() == context.DeadlineExceeded {
		return out.String(), fmt.Errorf("timed out after %s", cmdTimeout)
	}
	return out.String(), err
}

// isBenign reports whether outputLower (already lowercased) from the given
// operation's first command represents an idempotent no-op rather than a real
// failure: the device/bridge already existing for a create, or already being
// gone for a delete/detach.
func isBenign(o op, outputLower string) bool {
	switch o {
	case opCreateTap, opCreateBridge:
		return strings.Contains(outputLower, "file exists") ||
			strings.Contains(outputLower, "device or resource busy")
	case opDeleteTap, opDeleteBridge, opDetach:
		return strings.Contains(outputLower, "cannot find device") ||
			strings.Contains(outputLower, "does not exist") ||
			strings.Contains(outputLower, "no such device")
	case opAttach:
		// Re-attaching to the same bridge it's already a member of is a
		// no-op in practice; treat "file exists" defensively the same way.
		return strings.Contains(outputLower, "file exists")
	default:
		return false
	}
}

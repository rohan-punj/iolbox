//go:build linux

package extnet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Endpoint is one running external-net endpoint: a nat node's tap fd plus a
// DHCP server on that tap. It is process-less — the server drives its lifecycle
// directly (Start/Close) rather than through node.Process, but reports
// running/stopped the same way.
type Endpoint struct {
	dev string // tap device name

	tap  *os.File    // /dev/net/tun (nat) fd
	dhcp *dhcpServer // nat only

	closeOnce sync.Once
	closed    chan struct{}

	// mu guards cfg, brName and the DHCP pump generation (pumpStop/pumpWG).
	mu       sync.Mutex
	cfg      Config
	pumpStop chan struct{}  // closed to stop the current DHCP pump generation
	pumpWG   sync.WaitGroup // the current generation's pump

	// brName is the link bridge this nat tap is currently attached to ("" until
	// AttachBridge). Guarded by mu.
	brName string
}

// ioctl constants for TUNSETIFF (opening a tap via /dev/net/tun).
const (
	tunSetIff = 0x400454ca // TUNSETIFF
	iffTap    = 0x0002     // IFF_TAP
	iffNoPI   = 0x1000     // IFF_NO_PI (no 4-byte packet-info prefix)
	ifnamsiz  = 16
	frameMax  = 65536
)

// cmdTimeout bounds each privileged command so a wedged `sudo` (e.g. a ~10s
// hostname-resolution stall when the runtime's hostname isn't in /etc/hosts —
// see the firstboot note in docs) surfaces as a clean node failure instead of
// blocking lab.start on the control loop. Setup issues a handful of commands,
// so this is a generous ceiling, not a normal-case wait.
const cmdTimeout = 20 * time.Second

// runCmds executes each command (bare if we're already root, else via
// `sudo -n` — see sudoArgv), wrapping the first failure with its combined
// stderr so the caller sees exactly which privileged step failed and why. It
// stops at the first error. Each command is bounded by cmdTimeout.
func runCmds(cmds []cmd) error {
	for _, c := range cmds {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		name, args := sudoArgv(os.Geteuid(), c.args)
		out := &bytes.Buffer{}
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stderr = out
		cmd.Stdout = out
		err := cmd.Run()
		cancel()
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("extnet: `%s` timed out after %s "+
				"(is the runtime hostname in /etc/hosts?): %s", c.String(), cmdTimeout, strings.TrimSpace(out.String()))
		}
		if err != nil {
			return fmt.Errorf("extnet: `%s` failed: %v: %s", c.String(), err, strings.TrimSpace(out.String()))
		}
	}
	return nil
}

// runCmdsBestEffort runs teardown commands, ignoring failures (a device already
// gone or a rule already removed must not abort the rest of teardown). It
// returns the joined errors for logging only.
func runCmdsBestEffort(cmds []cmd) error {
	var errs error
	for _, c := range cmds {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		name, args := sudoArgv(os.Geteuid(), c.args)
		out := &bytes.Buffer{}
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("`%s`: %v: %s", c.String(), err, strings.TrimSpace(out.String())))
		}
		cancel()
	}
	return errs
}

// Start brings up the endpoint: runs the privileged setup commands, opens the
// tap fd (created unbridged — AttachBridge joins it to a link bridge later), and
// launches the userspace DHCP server on the tap. On any failure it reverses
// whatever it already did. The returned Endpoint owns an fd + goroutine and must
// be Closed to tear everything down.
func Start(cfg Config) (*Endpoint, error) {
	dev, err := devName(cfg.Kind, cfg.NodeID)
	if err != nil {
		return nil, err
	}
	owner, err := currentUser()
	if err != nil {
		return nil, err
	}

	e := &Endpoint{cfg: cfg, dev: dev, closed: make(chan struct{})}

	// Clear any leftover device of the same name from a prior run that didn't
	// tear down cleanly (e.g. the supervisor was SIGKILLed), so setup's
	// `ip ... add` doesn't fail with "File exists". Best-effort: a missing
	// device is fine.
	e.runTeardown()

	// setupWithRetry runs the privileged setup, and on failure re-runs the
	// best-effort preclean and tries ONCE more after a short pause. This makes
	// endpoint start self-healing against stale kernel state a single preclean
	// can't clear in one pass — e.g. a leftover device whose fd was still held
	// when the preclean's `ip ... del` ran (EBUSY), making the subsequent
	// `ip ... add` fail with "File exists". Observed after hard-killed
	// supervisors; by the retry the old holder is gone and the delete lands.
	setupWithRetry := func(setup []cmd) error {
		err := runCmds(setup)
		if err == nil {
			return nil
		}
		e.runTeardown()
		time.Sleep(750 * time.Millisecond)
		if rerr := runCmds(setup); rerr == nil {
			return nil
		}
		return err // report the FIRST failure — it names the real obstacle
	}

	switch cfg.Kind {
	case KindNAT:
		sub := Subnet{Index: cfg.SubnetIndex}
		// Create the tap unbridged, with no gateway/NAT yet (AttachBridge wires
		// those onto the link bridge when the link exists). The DHCP server runs
		// directly on the tap fd; there is no relay socket.
		if err := setupWithRetry(natBridgeTapCmds(dev, owner)); err != nil {
			return nil, err
		}
		tap, oerr := openTap(dev)
		if oerr != nil {
			_ = runCmdsBestEffort(natBridgeTapDelCmds(dev))
			return nil, oerr
		}
		e.tap = tap
		e.dhcp = newDHCPServer(net.ParseIP(sub.GatewayIP()), sub)
		e.startDHCPPump()
		return e, nil
	default:
		return nil, fmt.Errorf("extnet: unknown kind %q", cfg.Kind)
	}
}

// startDHCPPump launches the DHCP loop: a single goroutine that
// reads frames off the tap fd (the bridge floods broadcast DISCOVER/REQUEST to
// it), answers DHCP from the userspace server, and drops everything else (real
// IP traffic and ARP are handled by the kernel bridge + gateway on br). No relay
// socket is involved. Recorded in pumpStop/pumpWG so Close reuses stopPumps.
func (e *Endpoint) startDHCPPump() {
	e.mu.Lock()
	stop := make(chan struct{})
	e.pumpStop = stop
	e.pumpWG.Add(1)
	e.mu.Unlock()
	go func() { defer e.pumpWG.Done(); e.pumpDHCPTap(stop) }()
}

// pumpDHCPTap is the bridge-mode DHCP loop (see startDHCPPump).
func (e *Endpoint) pumpDHCPTap(stop chan struct{}) {
	buf := make([]byte, frameMax)
	for {
		select {
		case <-stop:
			return
		case <-e.closed:
			return
		default:
		}
		_ = e.tap.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := e.tap.Read(buf)
		if err != nil {
			if os.IsTimeout(err) {
				continue
			}
			return
		}
		if n == 0 || e.dhcp == nil {
			continue
		}
		if reply, consumed := e.dhcp.consume(buf[:n]); consumed && reply != nil {
			if _, werr := e.tap.Write(reply); werr != nil && e.isClosed() {
				return
			}
		}
	}
}

// AttachBridge wires this nat endpoint onto the link's Linux bridge: it attaches
// the tap as an L2 member and moves the gateway address + MASQUERADE/FORWARD
// rules onto the bridge interface. Idempotent (a no-op if already attached to
// brName). This is the runtime hot-connect step — it never touches the running
// DHCP loop or the tap fd, so a link drawn to an already-running nat immediately
// starts serving DHCP with no restart.
func (e *Endpoint) AttachBridge(brName string) error {
	if e.isClosed() {
		return nil
	}
	e.mu.Lock()
	if e.brName == brName {
		e.mu.Unlock()
		return nil
	}
	prev := e.brName
	e.mu.Unlock()
	if prev != "" {
		e.DetachBridge() // moved to a different bridge (reshaped link)
	}
	sub := Subnet{Index: e.cfg.SubnetIndex}
	if err := runCmds(natBridgeAttachCmds(e.dev, brName, sub, e.cfg.DefaultIface)); err != nil {
		return err
	}
	e.mu.Lock()
	e.brName = brName
	e.mu.Unlock()
	return nil
}

// DetachBridge reverses AttachBridge (remove NAT rules + gateway address, detach
// the tap). The tap and DHCP loop persist — the nat is simply unconnected again,
// ready to reattach to a new link. Best-effort; the bridge device is deleted by
// the fabric, not here.
func (e *Endpoint) DetachBridge() {
	e.mu.Lock()
	br := e.brName
	e.brName = ""
	e.mu.Unlock()
	if br == "" {
		return
	}
	sub := Subnet{Index: e.cfg.SubnetIndex}
	_ = runCmdsBestEffort(natBridgeDetachCmds(e.dev, br, sub, e.cfg.DefaultIface))
}

// stopPumps signals the current pump generation and waits for it to exit. It
// nudges the tap read deadline so the blocked read observes the stop promptly
// instead of after a full 500ms poll.
func (e *Endpoint) stopPumps() {
	e.mu.Lock()
	stop := e.pumpStop
	e.pumpStop = nil
	e.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	if e.tap != nil {
		_ = e.tap.SetReadDeadline(time.Now().Add(-time.Second))
	}
	e.pumpWG.Wait()
}

func (e *Endpoint) isClosed() bool {
	select {
	case <-e.closed:
		return true
	default:
		return false
	}
}

// Close stops the DHCP pump, closes the tap, and runs the teardown commands
// (delete the tap, remove iptables rules by exact -D). Idempotent.
func (e *Endpoint) Close() error {
	e.closeOnce.Do(func() {
		close(e.closed)
		// Stop the current pump generation (also nudges the tap deadline).
		e.stopPumps()
		if e.tap != nil {
			_ = e.tap.Close()
		}
		e.runTeardown()
	})
	return nil
}

func (e *Endpoint) runTeardown() {
	switch e.cfg.Kind {
	case KindNAT:
		// Remove any bridge wiring (gateway/NAT rules + nomaster) then delete the
		// tap. The bridge device itself is fabric-owned.
		e.DetachBridge()
		_ = runCmdsBestEffort(natBridgeTapDelCmds(e.dev))
	}
}

// openTap opens /dev/net/tun and attaches it to the named tap device via the
// standard TUNSETIFF ioctl (IFF_TAP|IFF_NO_PI so reads/writes are bare ethernet
// frames with no 4-byte packet-info prefix). The device itself was created by
// `ip tuntap add` in setup; this just grabs a data fd for it.
func openTap(name string) (*os.File, error) {
	// Open the clone device raw and issue TUNSETIFF BEFORE the fd reaches the Go
	// runtime. os.OpenFile registers the fd with the network poller at open time —
	// before TUNSETIFF makes it a concrete tap — and that early registration is
	// unreliable for tun devices, so a later read/deadline flakily fails with
	// "not pollable"/EFAULT and kills the pump (dropping all inbound frames, e.g.
	// a node's DHCP requests). Registering post-TUNSETIFF via os.NewFile on a
	// non-blocking configured tap is reliable.
	fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("extnet: open /dev/net/tun: %w", err)
	}
	var ifr [ifnamsiz + 24]byte
	copy(ifr[:ifnamsiz-1], name)
	// flags at offset IFNAMSIZ (16): IFF_TAP | IFF_NO_PI.
	flags := uint16(iffTap | iffNoPI)
	ifr[ifnamsiz] = byte(flags)
	ifr[ifnamsiz+1] = byte(flags >> 8)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tunSetIff, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("extnet: TUNSETIFF %s: %v", name, errno)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("extnet: set nonblock %s: %w", name, err)
	}
	return os.NewFile(uintptr(fd), "/dev/net/tun"), nil
}

// currentUser returns the username that should own the tap (the process user,
// e.g. "iolbox"), so `ip tuntap add ... user <u>` grants us the fd without root.
func currentUser() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username, nil
	}
	if u := os.Getenv("USER"); u != "" {
		return u, nil
	}
	return "", errors.New("extnet: cannot determine current user for tap ownership")
}

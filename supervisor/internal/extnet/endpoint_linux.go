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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Endpoint is one running external-net endpoint: a tap/macvtap fd plus the two
// pump goroutines that move raw ethernet frames between that fd and the link's
// UDP relay. A nat endpoint additionally owns a DHCP server on its tap. It is
// process-less — the server drives its lifecycle directly (Start/Close) rather
// than through node.Process, but reports running/stopped the same way.
type Endpoint struct {
	dev string // tap/macvtap device name

	tap  *os.File    // /dev/net/tun (nat) or /dev/tapN (mgmt) fd
	dhcp *dhcpServer // nat only

	closeOnce sync.Once
	closed    chan struct{}

	// mu guards cfg, udpConn, sendTo and the pump generation (pumpStop/pumpWG),
	// so Rebind can swap the relay socket + restart the pumps while Close or a
	// concurrent Rebind is possible. The tap fd, dhcp server, subnet and iptables
	// rules are NOT touched by Rebind — only the UDP relay binding is.
	mu       sync.Mutex
	cfg      Config
	udpConn  *net.UDPConn // bound on ListenPort; sends to SendPort
	sendTo   *net.UDPAddr
	pumpStop chan struct{}  // closed to stop the current pump generation
	pumpWG   sync.WaitGroup // the current generation's two pumps
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

// runCmds executes each command via `sudo -n`, wrapping the first failure with
// its combined stderr so the caller sees exactly which privileged step failed
// and why. It stops at the first error. Each command is bounded by cmdTimeout.
func runCmds(cmds []cmd) error {
	for _, c := range cmds {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		full := append([]string{"-n"}, c.args...)
		out := &bytes.Buffer{}
		cmd := exec.CommandContext(ctx, "sudo", full...)
		cmd.Stderr = out
		cmd.Stdout = out
		err := cmd.Run()
		cancel()
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("extnet: `sudo -n %s` timed out after %s "+
				"(is the runtime hostname in /etc/hosts?): %s", c.String(), cmdTimeout, strings.TrimSpace(out.String()))
		}
		if err != nil {
			return fmt.Errorf("extnet: `sudo -n %s` failed: %v: %s", c.String(), err, strings.TrimSpace(out.String()))
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
		full := append([]string{"-n"}, c.args...)
		out := &bytes.Buffer{}
		cmd := exec.CommandContext(ctx, "sudo", full...)
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("`sudo -n %s`: %v: %s", c.String(), err, strings.TrimSpace(out.String())))
		}
		cancel()
	}
	return errs
}

// Start brings up the endpoint: runs the privileged setup commands, opens the
// tap fd, binds the UDP relay socket, and launches the frame pumps (+ DHCP for
// nat). On any failure it reverses whatever it already did. The returned
// Endpoint owns an fd + goroutines and must be Closed to tear everything down.
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
		if err := setupWithRetry(natSetupCmds(dev, sub, cfg.DefaultIface, owner)); err != nil {
			return nil, err
		}
		tap, oerr := openTap(dev)
		if oerr != nil {
			_ = runCmdsBestEffort(natTeardownCmds(dev, sub, cfg.DefaultIface))
			return nil, oerr
		}
		e.tap = tap
		e.dhcp = newDHCPServer(net.ParseIP(sub.GatewayIP()), sub)
	case KindMgmt:
		if err := setupWithRetry(mgmtSetupCmds(dev, cfg.MgmtIface)); err != nil {
			return nil, err
		}
		tap, oerr := openMacvtap(dev, owner)
		if oerr != nil {
			_ = runCmdsBestEffort(mgmtTeardownCmds(dev))
			return nil, oerr
		}
		e.tap = tap
	default:
		return nil, fmt.Errorf("extnet: unknown kind %q", cfg.Kind)
	}

	if err := e.bindRelay(); err != nil {
		e.teardownDevice()
		return nil, err
	}

	// Frame pumps: tap->relay and relay->tap. A nat endpoint also intercepts
	// DHCP traffic on the tap side (the DHCP server both consumes client
	// requests and injects replies straight to the tap).
	e.startPumps()
	return e, nil
}

// startPumps launches the tap<->relay pump pair against the CURRENT relay
// socket, recording the generation's stop channel + waitgroup so Rebind/Close
// can cycle it. Each pump captures its (stop, conn) so a rebind's new pumps and
// the old ones never share a socket. Caller holds no lock; startPumps takes mu.
func (e *Endpoint) startPumps() {
	e.mu.Lock()
	stop := make(chan struct{})
	e.pumpStop = stop
	conn := e.udpConn
	e.pumpWG.Add(2)
	e.mu.Unlock()
	go func() { defer e.pumpWG.Done(); e.pumpTapToRelay(stop, conn) }()
	go func() { defer e.pumpWG.Done(); e.pumpRelayToTap(stop, conn) }()
}

// stopPumps signals the current pump generation and waits for both pumps to
// exit. It nudges the tap + relay read deadlines so the blocked reads observe
// the stop promptly instead of after a full 500ms poll.
func (e *Endpoint) stopPumps() {
	e.mu.Lock()
	stop := e.pumpStop
	conn := e.udpConn
	e.pumpStop = nil
	e.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	if e.tap != nil {
		_ = e.tap.SetReadDeadline(time.Now().Add(-time.Second))
	}
	if conn != nil {
		_ = conn.SetReadDeadline(time.Now().Add(-time.Second))
	}
	e.pumpWG.Wait()
}

// Rebind re-points the relay UDP socket to new ports WITHOUT disturbing the tap,
// DHCP server, subnet, or iptables rules. It exists because a nat/mgmt node can
// be started before its link exists (the GUI auto-starts a NAT the instant it is
// dropped on the canvas, so the endpoint first binds an EPHEMERAL relay port);
// when the link is later drawn, the relay is created on the plan's deterministic
// port and the endpoint must move its socket to match or the two never meet
// (the DHCP DISCOVER lands on a dead port and the node never gets a lease). Also
// covers a link reshaped/removed+readded after the endpoint started. Idempotent:
// a no-op when already bound to these ports. Safe against a concurrent Close.
func (e *Endpoint) Rebind(sendPort, listenPort int) error {
	if e.isClosed() {
		return nil
	}
	e.mu.Lock()
	same := e.udpConn != nil && e.cfg.SendPort == sendPort && e.cfg.ListenPort == listenPort
	e.mu.Unlock()
	if same {
		return nil
	}
	e.stopPumps()
	e.mu.Lock()
	if e.udpConn != nil {
		_ = e.udpConn.Close()
		e.udpConn = nil
	}
	e.cfg.SendPort = sendPort
	e.cfg.ListenPort = listenPort
	e.mu.Unlock()
	if err := e.bindRelay(); err != nil {
		return err
	}
	e.startPumps()
	return nil
}

// Ports reports the relay ports this endpoint is currently bound to (SendPort =
// where it sends tap frames, ListenPort = where it receives). Used by the server
// to decide whether a Rebind is needed after a plan rebuild.
func (e *Endpoint) Ports() (sendPort, listenPort int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.SendPort, e.cfg.ListenPort
}

// bindRelay binds the UDP socket on ListenPort (frames the relay delivers) and
// resolves the SendPort target (frames we send to the relay). Takes mu to
// publish the new socket + target atomically for a concurrent Rebind/Close.
func (e *Endpoint) bindRelay() error {
	e.mu.Lock()
	host := e.cfg.resolvedHost()
	listenPort := e.cfg.ListenPort
	sendPort := e.cfg.SendPort
	e.mu.Unlock()

	laddr := &net.UDPAddr{IP: net.ParseIP(host), Port: listenPort}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return fmt.Errorf("extnet: bind relay udp %s: %w", laddr, err)
	}

	e.mu.Lock()
	e.udpConn = conn
	e.sendTo = &net.UDPAddr{IP: net.ParseIP(host), Port: sendPort}
	e.mu.Unlock()
	return nil
}

// pumpTapToRelay reads ethernet frames off the tap (the kernel/NAT side —
// un-NAT'd return traffic and ARP replies for the gateway IP) and forwards them
// to the relay so the connected lab node sees them. conn/stop are this pump
// generation's socket + stop channel (a Rebind starts a fresh generation with a
// new socket), so this loop never touches e.udpConn after a swap.
func (e *Endpoint) pumpTapToRelay(stop chan struct{}, conn *net.UDPConn) {
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
		if n == 0 {
			continue
		}
		e.mu.Lock()
		sendTo := e.sendTo
		e.mu.Unlock()
		if _, err := conn.WriteToUDP(buf[:n], sendTo); err != nil {
			select {
			case <-stop:
				return
			default:
			}
			if e.isClosed() {
				return
			}
		}
	}
}

// pumpRelayToTap reads frames the lab node sent over the relay. A nat endpoint
// first offers each frame to its DHCP server: a DHCP request is answered back
// over the relay (toward the lab node) and NOT delivered to the tap, since the
// exchange is between the lab node and our userspace server, not the kernel.
// Every other frame (real IP traffic, ARP for the gateway) is written into the
// tap so the kernel routes/NATs or answers it.
func (e *Endpoint) pumpRelayToTap(stop chan struct{}, conn *net.UDPConn) {
	buf := make([]byte, frameMax)
	for {
		select {
		case <-stop:
			return
		case <-e.closed:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-stop:
					return
				default:
					continue
				}
			}
			return
		}
		if n == 0 {
			continue
		}
		frame := buf[:n]
		if e.dhcp != nil {
			if reply, consumed := e.dhcp.consume(frame); consumed {
				if reply != nil {
					e.mu.Lock()
					sendTo := e.sendTo
					e.mu.Unlock()
					_, _ = conn.WriteToUDP(reply, sendTo)
				}
				continue // DHCP handled in userspace; never touches the kernel
			}
		}
		if _, err := e.tap.Write(frame); err != nil {
			if e.isClosed() {
				return
			}
		}
	}
}

func (e *Endpoint) isClosed() bool {
	select {
	case <-e.closed:
		return true
	default:
		return false
	}
}

// Close stops the pumps, closes the tap + relay socket, and runs the teardown
// commands (delete tap/macvtap, remove iptables rules by exact -D). Idempotent.
func (e *Endpoint) Close() error {
	e.closeOnce.Do(func() {
		close(e.closed)
		// Stop the current pump generation (also nudges the tap+relay deadlines).
		e.stopPumps()
		if e.tap != nil {
			_ = e.tap.Close()
		}
		e.mu.Lock()
		conn := e.udpConn
		e.udpConn = nil
		e.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		e.runTeardown()
	})
	return nil
}

// teardownDevice runs only the privileged teardown (used on a Start failure
// after the device is up but before pumps started).
func (e *Endpoint) teardownDevice() {
	if e.tap != nil {
		_ = e.tap.Close()
	}
	if e.udpConn != nil {
		_ = e.udpConn.Close()
	}
	e.runTeardown()
}

func (e *Endpoint) runTeardown() {
	switch e.cfg.Kind {
	case KindNAT:
		sub := Subnet{Index: e.cfg.SubnetIndex}
		_ = runCmdsBestEffort(natTeardownCmds(e.dev, sub, e.cfg.DefaultIface))
	case KindMgmt:
		_ = runCmdsBestEffort(mgmtTeardownCmds(e.dev))
	}
}

// openTap opens /dev/net/tun and attaches it to the named tap device via the
// standard TUNSETIFF ioctl (IFF_TAP|IFF_NO_PI so reads/writes are bare ethernet
// frames with no 4-byte packet-info prefix). The device itself was created by
// `ip tuntap add` in setup; this just grabs a data fd for it.
func openTap(name string) (*os.File, error) {
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("extnet: open /dev/net/tun: %w", err)
	}
	var ifr [ifnamsiz + 24]byte
	copy(ifr[:ifnamsiz-1], name)
	// flags at offset IFNAMSIZ (16): IFF_TAP | IFF_NO_PI.
	flags := uint16(iffTap | iffNoPI)
	ifr[ifnamsiz] = byte(flags)
	ifr[ifnamsiz+1] = byte(flags >> 8)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), tunSetIff, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("extnet: TUNSETIFF %s: %v", name, errno)
	}
	return f, nil
}

// openMacvtap opens the /dev/tapN character device backing a macvtap link. The
// device number N is the interface's ifindex, read from
// /sys/class/net/<name>/ifindex, so the path is /dev/tap<ifindex>. Reads/writes
// are bare ethernet frames (macvtap has no packet-info prefix).
//
// The kernel creates /dev/tapN root-owned mode 0600, so — unlike the nat tap,
// which `ip tuntap add ... user <owner>` hands us directly — we must chown it
// to owner first (via sudo) before this unprivileged process can open it.
func openMacvtap(name, owner string) (*os.File, error) {
	idxRaw, err := os.ReadFile("/sys/class/net/" + name + "/ifindex")
	if err != nil {
		return nil, fmt.Errorf("extnet: read ifindex for %s: %w", name, err)
	}
	idx := strings.TrimSpace(string(idxRaw))
	if _, cerr := strconv.Atoi(idx); cerr != nil {
		return nil, fmt.Errorf("extnet: bad ifindex %q for %s", idx, name)
	}
	dev := "/dev/tap" + idx
	if err := runCmds([]cmd{{[]string{"chown", owner, dev}}}); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("extnet: open %s: %w", dev, err)
	}
	// macvtap queues default to IFF_VNET_HDR: every read/write is prefixed
	// with a 10-byte virtio_net_hdr, so bare ethernet writes get parsed as a
	// bogus virtio header and silently dropped (and reads arrive shifted).
	// Clear it via the same TUNSETIFF ioctl the tap path uses — macvtap
	// accepts IFF_TAP|IFF_NO_PI flag updates on an open queue (the name field
	// is ignored) — so both directions carry bare frames like the nat tap.
	var ifr [ifnamsiz + 24]byte
	flags := uint16(iffTap | iffNoPI)
	ifr[ifnamsiz] = byte(flags)
	ifr[ifnamsiz+1] = byte(flags >> 8)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), tunSetIff, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("extnet: clear IFF_VNET_HDR on %s: %v", dev, errno)
	}
	return f, nil
}

// currentUser returns the username that should own the tap (the process user,
// e.g. "iolab"), so `ip tuntap add ... user <u>` grants us the fd without root.
func currentUser() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username, nil
	}
	if u := os.Getenv("USER"); u != "" {
		return u, nil
	}
	return "", errors.New("extnet: cannot determine current user for tap ownership")
}

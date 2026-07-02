//go:build linux

package extnet

import (
	"bytes"
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
	cfg Config
	dev string // tap/macvtap device name

	tap     *os.File     // /dev/net/tun (nat) or /dev/tapN (mgmt) fd
	udpConn *net.UDPConn // bound on ListenPort; sends to SendPort
	sendTo  *net.UDPAddr

	dhcp *dhcpServer // nat only

	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// ioctl constants for TUNSETIFF (opening a tap via /dev/net/tun).
const (
	tunSetIff = 0x400454ca // TUNSETIFF
	iffTap    = 0x0002     // IFF_TAP
	iffNoPI   = 0x1000     // IFF_NO_PI (no 4-byte packet-info prefix)
	ifnamsiz  = 16
	frameMax  = 65536
)

// runCmds executes each command via `sudo -n`, wrapping the first failure with
// its combined stderr so the caller sees exactly which privileged step failed
// and why. It stops at the first error.
func runCmds(cmds []cmd) error {
	for _, c := range cmds {
		full := append([]string{"-n"}, c.args...)
		out := &bytes.Buffer{}
		cmd := exec.Command("sudo", full...)
		cmd.Stderr = out
		cmd.Stdout = out
		if err := cmd.Run(); err != nil {
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
		full := append([]string{"-n"}, c.args...)
		out := &bytes.Buffer{}
		cmd := exec.Command("sudo", full...)
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("`sudo -n %s`: %v: %s", c.String(), err, strings.TrimSpace(out.String())))
		}
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

	switch cfg.Kind {
	case KindNAT:
		sub := Subnet{Index: cfg.SubnetIndex}
		if err := runCmds(natSetupCmds(dev, sub, cfg.DefaultIface, owner)); err != nil {
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
		if err := runCmds(mgmtSetupCmds(dev, cfg.MgmtIface)); err != nil {
			return nil, err
		}
		tap, oerr := openMacvtap(dev)
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
	e.wg.Add(2)
	go func() { defer e.wg.Done(); e.pumpTapToRelay() }()
	go func() { defer e.wg.Done(); e.pumpRelayToTap() }()
	return e, nil
}

// bindRelay binds the UDP socket on ListenPort (frames the relay delivers) and
// resolves the SendPort target (frames we send to the relay).
func (e *Endpoint) bindRelay() error {
	host := e.cfg.resolvedHost()
	laddr := &net.UDPAddr{IP: net.ParseIP(host), Port: e.cfg.ListenPort}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return fmt.Errorf("extnet: bind relay udp %s: %w", laddr, err)
	}
	e.udpConn = conn
	e.sendTo = &net.UDPAddr{IP: net.ParseIP(host), Port: e.cfg.SendPort}
	return nil
}

// pumpTapToRelay reads ethernet frames off the tap and forwards them to the
// relay. For a nat endpoint it first offers each frame to the DHCP server; a
// frame the server consumed (a DHCP request it replied to on the tap) is not
// forwarded to the relay.
func (e *Endpoint) pumpTapToRelay() {
	buf := make([]byte, frameMax)
	for {
		select {
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
		frame := buf[:n]
		if e.dhcp != nil && e.dhcp.consume(frame, e.tap) {
			continue // a DHCP request the server handled locally
		}
		if _, err := e.udpConn.WriteToUDP(frame, e.sendTo); err != nil {
			if e.isClosed() {
				return
			}
		}
	}
}

// pumpRelayToTap reads frames the relay delivers over UDP and writes them into
// the tap so the connected lab node sees them.
func (e *Endpoint) pumpRelayToTap() {
	buf := make([]byte, frameMax)
	for {
		select {
		case <-e.closed:
			return
		default:
		}
		_ = e.udpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := e.udpConn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		if n == 0 {
			continue
		}
		if _, err := e.tap.Write(buf[:n]); err != nil {
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
		// Nudge the blocking reads so the pumps observe closed within a poll.
		if e.tap != nil {
			_ = e.tap.SetReadDeadline(time.Now().Add(-time.Second))
		}
		if e.udpConn != nil {
			_ = e.udpConn.SetReadDeadline(time.Now().Add(-time.Second))
		}
		e.wg.Wait()
		if e.tap != nil {
			_ = e.tap.Close()
		}
		if e.udpConn != nil {
			_ = e.udpConn.Close()
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
func openMacvtap(name string) (*os.File, error) {
	idxRaw, err := os.ReadFile("/sys/class/net/" + name + "/ifindex")
	if err != nil {
		return nil, fmt.Errorf("extnet: read ifindex for %s: %w", name, err)
	}
	idx := strings.TrimSpace(string(idxRaw))
	if _, cerr := strconv.Atoi(idx); cerr != nil {
		return nil, fmt.Errorf("extnet: bad ifindex %q for %s", idx, name)
	}
	dev := "/dev/tap" + idx
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("extnet: open %s: %w", dev, err)
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

//go:build linux

package iouyap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// errBridgeClosed is returned by the tap read loop when the bridge is being
// torn down (Close closed b.closed / the fd), so runPump stops cleanly rather
// than treating shutdown as a transient error to retry.
var errBridgeClosed = errors.New("iouyap: tap bridge closed")

// TapBridge relays datagrams between one IOL netio unix-domain socket and one
// Linux tap device, in place of the UDP relay Bridge uses. Frames read from
// the netio socket have the netio header stripped and are written raw to the
// tap fd (IFF_NO_PI: no packet-info prefix); frames read from the tap fd are
// wrapped with a fresh netio header and delivered back to whichever peer most
// recently sent a datagram on the netio socket (IOL connects to us, so we
// learn its address from the first datagram it sends, exactly like Bridge).
//
// The tap device itself is not owned by TapBridge: the fabric manager creates
// it (`ip tuntap add ... mode tap user <uid>`) and attaches it to a Linux
// bridge alongside the peer IOL's own tap, so the pair of taps plus the
// kernel bridge together stand in for the UDP relay used in non-tap mode.
type TapBridge struct {
	cfg Config

	unixConn *net.UnixConn
	tap      *os.File
	// Test-only hooks keep lifecycle tests independent of /dev/net/tun. They
	// are nil for real bridges and never change production I/O behavior.
	tapRead  func([]byte) (int, error)
	tapWrite func([]byte) (int, error)

	peerMu   sync.RWMutex
	peerAddr *net.UnixAddr // learned from the first datagram IOL sends us

	closeOnce sync.Once
	closed    chan struct{}
}

// NewTap binds the netio unix-domain socket at cfg.NetioPath (removing any
// stale socket file first) and attaches to the existing persistent tap device
// named tapName. It does not start pumping; call Run for that.
//
// cfg is validated with validateTap (tap mode has no UDP side).
func NewTap(cfg Config, tapName string) (*TapBridge, error) {
	if err := cfg.validateTap(); err != nil {
		return nil, err
	}

	// Remove a stale socket file from a previous run, if any. IOL will
	// connect fresh once we (re)create the listener.
	if _, err := os.Stat(cfg.NetioPath); err == nil {
		if rmErr := os.Remove(cfg.NetioPath); rmErr != nil {
			return nil, fmt.Errorf("iouyap: remove stale socket %s: %w", cfg.NetioPath, rmErr)
		}
	}

	unixAddr, err := net.ResolveUnixAddr("unixgram", cfg.NetioPath)
	if err != nil {
		return nil, fmt.Errorf("iouyap: resolve unix addr %s: %w", cfg.NetioPath, err)
	}
	unixConn, err := net.ListenUnixgram("unixgram", unixAddr)
	if err != nil {
		return nil, fmt.Errorf("iouyap: listen unixgram %s: %w", cfg.NetioPath, err)
	}

	tap, err := openTap(tapName)
	if err != nil {
		_ = unixConn.Close()
		_ = os.Remove(cfg.NetioPath)
		return nil, err
	}

	return &TapBridge{
		cfg:      cfg,
		unixConn: unixConn,
		tap:      tap,
		closed:   make(chan struct{}),
	}, nil
}

// Run pumps datagrams in both directions until ctx is cancelled or an
// unrecoverable error occurs. It always returns after Close is called or
// ctx.Done fires; it never leaks goroutines.
func (b *TapBridge) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	return runPumps(ctx, func() { _ = b.Close() },
		func(ctx context.Context, errs chan<- error) { b.pumpNetioToTap(ctx, errs) },
		func(ctx context.Context, errs chan<- error) { b.pumpTapToNetio(ctx, errs) },
	)
}

// pumpNetioToTap reads datagrams IOL sends on the netio unix socket, strips
// the netio header, and writes the raw ethernet frame to the tap fd. It also
// records the sender's unix address so pumpTapToNetio knows where to deliver
// return traffic.
func (b *TapBridge) pumpNetioToTap(ctx context.Context, errs chan<- error) {
	buf := make([]byte, 65536)
	read := func() ([]byte, error) {
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			_ = b.unixConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, addr, err := b.unixConn.ReadFromUnix(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return nil, err
			}
			if addr != nil && addr.Name != "" {
				b.peerMu.Lock()
				b.peerAddr = addr
				b.peerMu.Unlock()
			}
			out := make([]byte, n)
			copy(out, buf[:n])
			return out, nil
		}
	}
	write := func(frame []byte) error {
		var err error
		if b.tapWrite != nil {
			_, err = b.tapWrite(frame)
		} else {
			_, err = b.tap.Write(frame)
		}
		return err
	}
	runPump(ctx, read, write, stripToUDP, errs)
}

// pumpTapToNetio reads raw ethernet frames from the tap fd and delivers them,
// wrapped in a fresh netio header, to IOL's netio socket. Until IOL has sent
// at least one datagram (so we know its unix address — unixgram has no
// connect/accept handshake), inbound tap frames are dropped; this matches
// Bridge.pumpUDPToNetio's behaviour of having nothing to deliver to until the
// local netio peer has dialed in.
func (b *TapBridge) pumpTapToNetio(ctx context.Context, errs chan<- error) {
	buf := make([]byte, 65536)
	read := func() ([]byte, error) {
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-b.closed:
				return nil, errBridgeClosed
			default:
			}
			var n int
			var err error
			if b.tapRead != nil {
				n, err = b.tapRead(buf)
			} else {
				n, err = b.tap.Read(buf)
			}
			if err != nil {
				// On shutdown (Close evicts the fd / ctx cancelled) exit cleanly.
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-b.closed:
					return nil, errBridgeClosed
				default:
				}
				// An unexpected read error must NOT permanently kill inbound
				// delivery for this interface (that would silently strand the
				// node — e.g. its DHCP OFFER never arrives). Drop and retry after
				// a brief pause instead of tearing the pump down.
				time.Sleep(time.Millisecond)
				continue
			}
			out := make([]byte, n)
			copy(out, buf[:n])
			return out, nil
		}
	}
	write := func(datagram []byte) error {
		b.peerMu.RLock()
		peer := b.peerAddr
		b.peerMu.RUnlock()
		if peer == nil {
			// No netio peer has connected yet; nothing to deliver to.
			return nil
		}
		// A transient write error (e.g. ECONNREFUSED while IOL is still booting
		// and has not yet bound its netio socket) must not kill the pump — that
		// would permanently stop inbound delivery. Drop the frame; a DHCP client
		// or any retransmitting sender will try again once IOL is listening.
		_, _ = b.unixConn.WriteToUnix(datagram, peer)
		return nil
	}
	runPump(ctx, read, write, b.cfg.wrapToNetio, errs)
}

// Close shuts down the unix socket and the tap fd and removes the netio
// socket file. It is safe to call multiple times and safe to call
// concurrently with Run (Run will observe the closed unix socket/deadline
// nudge and the closed tap fd's read error, and return). The tap DEVICE
// itself is left in place: the fabric manager, not TapBridge, owns its
// lifecycle (creation via `ip tuntap add` and eventual deletion).
func (b *TapBridge) Close() error {
	var err error
	b.closeOnce.Do(func() {
		close(b.closed)
		if cerr := b.unixConn.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
		if cerr := b.tap.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
		if rerr := os.Remove(b.cfg.NetioPath); rerr != nil && !os.IsNotExist(rerr) {
			err = errors.Join(err, rerr)
		}
	})
	return err
}

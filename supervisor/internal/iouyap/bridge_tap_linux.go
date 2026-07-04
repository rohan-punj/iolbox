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

	errs := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() { defer wg.Done(); b.pumpNetioToTap(ctx, errs) }()
	go func() { defer wg.Done(); b.pumpTapToNetio(ctx, errs) }()

	// Unblock the read() calls promptly on cancellation: net.UnixConn reads
	// don't respect ctx directly, so a watcher goroutine nudges its read
	// deadline into the past on cancel/close, same as Bridge.Run. The tap fd
	// read is unblocked by closing it in Close/here, since os.File reads
	// have no deadline knob wired through syscall.Read on all kernels the
	// way net.Conn does.
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
		case <-b.closed:
		}
		deadline := time.Now().Add(-time.Second)
		_ = b.unixConn.SetReadDeadline(deadline)
	}()

	wg.Wait()
	cancel()
	<-watchDone

	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
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
		_, err := b.tap.Write(frame)
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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		n, err := b.tap.Read(buf)
		if err != nil {
			return nil, err
		}
		out := make([]byte, n)
		copy(out, buf[:n])
		return out, nil
	}
	write := func(datagram []byte) error {
		b.peerMu.RLock()
		peer := b.peerAddr
		b.peerMu.RUnlock()
		if peer == nil {
			// No netio peer has connected yet; nothing to deliver to.
			return nil
		}
		_, err := b.unixConn.WriteToUnix(datagram, peer)
		return err
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

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

// Bridge relays datagrams between one IOL netio unix-domain socket and one
// UDP endpoint. Frames read from the netio socket are forwarded to the UDP
// remote (the relay's local port); frames read from UDP are delivered back
// to whichever peer most recently sent a datagram on the netio socket (IOL
// connects to us, so we learn its address from the first datagram it sends,
// exactly like the original iouyap).
type Bridge struct {
	cfg Config

	unixConn *net.UnixConn
	udpConn  *net.UDPConn
	udpRaddr *net.UDPAddr

	peerMu   sync.RWMutex
	peerAddr *net.UnixAddr // learned from the first datagram IOL sends us

	closeOnce sync.Once
	closed    chan struct{}
}

// New binds both sockets: a unix-domain datagram socket at cfg.NetioPath
// (removing any stale socket file first, matching standard unix-listener
// hygiene) and a UDP socket at cfg.Host:cfg.UDPLocal. It does not start
// pumping; call Run for that.
func New(cfg Config) (*Bridge, error) {
	if err := cfg.validate(); err != nil {
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

	host := cfg.resolvedHost()
	udpLaddr := &net.UDPAddr{IP: net.ParseIP(host), Port: cfg.UDPLocal}
	udpConn, err := net.ListenUDP("udp", udpLaddr)
	if err != nil {
		_ = unixConn.Close()
		_ = os.Remove(cfg.NetioPath)
		return nil, fmt.Errorf("iouyap: bind udp %s: %w", udpLaddr, err)
	}
	udpRaddr := &net.UDPAddr{IP: net.ParseIP(host), Port: cfg.UDPRemote}

	return &Bridge{
		cfg:      cfg,
		unixConn: unixConn,
		udpConn:  udpConn,
		udpRaddr: udpRaddr,
		closed:   make(chan struct{}),
	}, nil
}

// Run pumps datagrams in both directions until ctx is cancelled or an
// unrecoverable socket error occurs. It always returns after Close is called
// or ctx.Done fires; it never leaks goroutines.
func (b *Bridge) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() { defer wg.Done(); b.pumpNetioToUDP(ctx, errs) }()
	go func() { defer wg.Done(); b.pumpUDPToNetio(ctx, errs) }()

	// Unblock the read() calls promptly on cancellation: net.UnixConn and
	// net.UDPConn reads don't respect ctx directly, so a watcher goroutine
	// closes the read deadline loop by nudging the deadline into the past,
	// which the poll loops below already do periodically; this goroutine
	// just ensures we don't wait a full poll interval after Close/ctx-done.
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
		case <-b.closed:
		}
		deadline := time.Now().Add(-time.Second)
		_ = b.unixConn.SetReadDeadline(deadline)
		_ = b.udpConn.SetReadDeadline(deadline)
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

// pumpNetioToUDP reads datagrams IOL sends on the netio unix socket and
// forwards them to the UDP remote. It also records the sender's unix address
// so pumpUDPToNetio knows where to deliver return traffic.
func (b *Bridge) pumpNetioToUDP(ctx context.Context, errs chan<- error) {
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
	write := func(datagram []byte) error {
		_, err := b.udpConn.WriteToUDP(datagram, b.udpRaddr)
		return err
	}
	runPump(ctx, read, write, b.cfg.StripHeader, errs)
}

// pumpUDPToNetio reads datagrams the relay forwards over UDP and delivers
// them to IOL's netio socket. Until IOL has sent at least one datagram (so we
// know its unix address — unixgram has no connect/accept handshake), inbound
// UDP frames are dropped; this matches iouyap's own behaviour of having
// nothing to deliver to until the local netio peer has dialed in.
func (b *Bridge) pumpUDPToNetio(ctx context.Context, errs chan<- error) {
	buf := make([]byte, 65536)
	read := func() ([]byte, error) {
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			_ = b.udpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, _, err := b.udpConn.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return nil, err
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
		_, err := b.unixConn.WriteToUnix(datagram, peer)
		return err
	}
	runPump(ctx, read, write, b.cfg.StripHeader, errs)
}

// Close shuts down both sockets and removes the netio socket file. It is
// safe to call multiple times and safe to call concurrently with Run (Run
// will observe the closed sockets/deadline nudge and return).
func (b *Bridge) Close() error {
	var err error
	b.closeOnce.Do(func() {
		close(b.closed)
		if cerr := b.unixConn.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
		if cerr := b.udpConn.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
		if rerr := os.Remove(b.cfg.NetioPath); rerr != nil && !os.IsNotExist(rerr) {
			err = errors.Join(err, rerr)
		}
	})
	return err
}

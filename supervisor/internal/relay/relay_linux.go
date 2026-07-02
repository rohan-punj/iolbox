//go:build linux

package relay

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// udpRelay is the Linux data-plane relay. Each member endpoint has a bound UDP
// socket on 127.0.0.1:LocalPort. Received datagrams are forwarded per the relay
// kind and optionally teed to a pcapng capture server.
type udpRelay struct {
	cfg     Config
	conns   []*net.UDPConn
	remotes []*net.UDPAddr
	tee     *captureServer

	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
}

// newRelay is the platform hook called by Manager.Start on Linux.
func newRelay(cfg Config) (Relay, error) {
	r := &udpRelay{cfg: cfg, done: make(chan struct{})}

	for _, ep := range cfg.Endpoints {
		host := ep.Host
		if host == "" {
			host = "127.0.0.1"
		}
		laddr := &net.UDPAddr{IP: net.ParseIP(host), Port: ep.LocalPort}
		conn, err := net.ListenUDP("udp", laddr)
		if err != nil {
			r.closeConns()
			return nil, fmt.Errorf("relay link %d: bind %s: %w", cfg.LinkID, laddr, err)
		}
		raddr := &net.UDPAddr{IP: net.ParseIP(host), Port: ep.RemotePort}
		r.conns = append(r.conns, conn)
		r.remotes = append(r.remotes, raddr)
	}

	if cfg.CapturePort > 0 {
		tee, err := newCaptureServer(cfg.CapturePort)
		if err != nil {
			r.closeConns()
			return nil, fmt.Errorf("relay link %d: capture: %w", cfg.LinkID, err)
		}
		r.tee = tee
	}

	for i := range r.conns {
		r.wg.Add(1)
		go r.pump(i)
	}
	return r, nil
}

func (r *udpRelay) pump(src int) {
	defer r.wg.Done()
	buf := make([]byte, 65536)
	conn := r.conns[src]
	for {
		select {
		case <-r.done:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		datagram := buf[:n]

		// Tee for capture: the datagram already IS the clean ethernet frame
		// (the mesh is headerless; iouyap strips IOL's netio header at the
		// unix-socket edge).
		if r.tee != nil && len(datagram) > 0 {
			r.tee.Broadcast(datagram)
		}

		// Forward per relay kind.
		switch r.cfg.Kind {
		case KindP2P:
			// Two endpoints; forward to the other one.
			dst := 1 - src
			if dst >= 0 && dst < len(r.conns) {
				_, _ = r.conns[src].WriteToUDP(datagram, r.remotes[dst])
			}
		case KindHub:
			// Flood to every member except the source.
			for dst := range r.conns {
				if dst == src {
					continue
				}
				_, _ = r.conns[src].WriteToUDP(datagram, r.remotes[dst])
			}
		}
	}
}

func (r *udpRelay) LinkID() int { return r.cfg.LinkID }

func (r *udpRelay) CapturePort() int {
	if r.tee != nil {
		return r.tee.Port()
	}
	return 0
}

func (r *udpRelay) Close() error {
	r.closeOnce.Do(func() {
		close(r.done)
		r.closeConns()
		if r.tee != nil {
			r.tee.Close()
		}
		r.wg.Wait()
	})
	return nil
}

func (r *udpRelay) closeConns() {
	for _, c := range r.conns {
		if c != nil {
			_ = c.Close()
		}
	}
}

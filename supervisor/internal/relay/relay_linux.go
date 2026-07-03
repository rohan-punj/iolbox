//go:build linux

package relay

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
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

	// fwdFrames/fwdBytes count datagrams FORWARDED (not merely received),
	// summed across both directions and hub fan-out, for per-link throughput
	// events. Incremented in the pump forward path; read atomically by Stats.
	fwdFrames atomic.Uint64
	fwdBytes  atomic.Uint64

	// protoMu guards protoFrames, the cumulative per-protocol forwarded-frame
	// counters that drive the per-proto fps breakdown in link.stats. It's a
	// plain map under a mutex rather than atomics because the key set is
	// discovered at runtime (one label per protocol seen). Kept in lockstep
	// with fwdFrames: a datagram forwarded to N members counts N here too, so
	// the per-proto counts sum to the total frame count.
	protoMu     sync.Mutex
	protoFrames map[string]uint64

	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
}

// newRelay is the platform hook called by Manager.Start on Linux.
func newRelay(cfg Config) (Relay, error) {
	r := &udpRelay{cfg: cfg, done: make(chan struct{}), protoFrames: make(map[string]uint64)}

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
		tee, err := newCaptureServer(cfg.CaptureBind, cfg.CapturePort)
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

		// Classify once per received datagram (cheap fixed-offset byte peeks) so
		// the per-proto breakdown can be attributed on the forward path. Doing
		// it here (not per destination) keeps the work off the fan-out inner
		// loop; the count is then added once per successful forward so per-proto
		// totals track fwdFrames exactly.
		proto := Classify(datagram)
		fwd := 0

		// Forward per relay kind. Count each datagram actually forwarded (once
		// for P2P, once per destination member for a hub flood) so the stats
		// reflect real per-link throughput in both directions.
		switch r.cfg.Kind {
		case KindP2P:
			// Two endpoints; forward to the other one.
			dst := 1 - src
			if dst >= 0 && dst < len(r.conns) {
				if _, err := r.conns[src].WriteToUDP(datagram, r.remotes[dst]); err == nil {
					r.fwdFrames.Add(1)
					r.fwdBytes.Add(uint64(n))
					fwd++
				}
			}
		case KindHub:
			// Flood to every member except the source.
			for dst := range r.conns {
				if dst == src {
					continue
				}
				if _, err := r.conns[src].WriteToUDP(datagram, r.remotes[dst]); err == nil {
					r.fwdFrames.Add(1)
					r.fwdBytes.Add(uint64(n))
					fwd++
				}
			}
		}

		// Attribute the datagram to its protocol label once per forward that
		// actually happened (fwd==0 when nothing was sent, e.g. a lone P2P
		// endpoint), so the per-proto counts sum to fwdFrames.
		if fwd > 0 {
			r.protoMu.Lock()
			r.protoFrames[proto] += uint64(fwd)
			r.protoMu.Unlock()
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

func (r *udpRelay) Stats() (frames, bytes uint64) {
	return r.fwdFrames.Load(), r.fwdBytes.Load()
}

// ProtoStats returns a fresh copy of the cumulative per-protocol forwarded-frame
// counters, keyed by the label Classify assigns. The copy lets the server diff
// two snapshots without holding the relay's lock; nil is fine to return (and is
// returned implicitly as an empty map here) when no traffic has been seen.
func (r *udpRelay) ProtoStats() map[string]uint64 {
	r.protoMu.Lock()
	defer r.protoMu.Unlock()
	out := make(map[string]uint64, len(r.protoFrames))
	for k, v := range r.protoFrames {
		out[k] = v
	}
	return out
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

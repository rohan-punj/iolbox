// Package bcap captures a Linux bridge fabric link by running tcpdump on the
// bridge interface and re-serving the frames as a live pcapng-over-TCP stream
// (for the GUI/Wireshark), while also classifying and counting them for
// stats.
//
// In the bridge-fabric migration a "fabric" link is a Linux bridge
// br-<linkid>. Unlike the UDP relay mesh (internal/relay), there is no
// userspace pump to tee: frames are forwarded entirely in-kernel. tcpdump -i
// <bridge> DOES see every frame forwarded through the bridge on this kernel
// (empirically confirmed), so we shell out to it, parse its raw libpcap
// stdout stream, and reuse the EXISTING pcapng writer and protocol classifier
// from internal/relay so the GUI contract (one pcapng SHB+IDB header per
// client on connect, then one packet block per frame) is unchanged.
//
// This file holds the platform-independent pieces — the libpcap stream
// parser and the pcapng TCP server — so they're unit-testable without real
// tcpdump. The //go:build linux file wires up the actual tcpdump subprocess.
package bcap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/rohanpunj/iolab/supervisor/internal/relay"
)

// maxInclLen guards against stream corruption: no real capture snaplen we use
// exceeds this, so a bigger incl_len means we've lost sync with the record
// boundary.
const maxInclLen = 262144

// libpcap global header magic values. Each is checked against the header's
// first 4 bytes read as BOTH little-endian and big-endian; whichever
// interpretation matches one of these values tells us the record byte order
// (and, via which constant matched, the timestamp resolution). The swapped
// forms (e.g. 0xd4c3b2a1 for magicUsLE) never need their own constant: they
// fall out automatically from reading the same bytes in the other order.
const (
	magicUsLE = 0xa1b2c3d4 // microsecond timestamps
	magicNsLE = 0xa1b23c4d // nanosecond timestamps
)

// parsePcapStream reads a raw libpcap (classic, non-pcapng) byte stream — the
// format tcpdump -w - emits — and calls onFrame for every captured ethernet
// frame with its timestamp in microseconds since the Unix epoch.
//
// It auto-detects the byte order and the us-vs-ns timestamp resolution from
// the 4-byte magic at the start of the 24-byte global header, then loops
// reading 16-byte record headers followed by incl_len bytes of frame data.
// It returns nil cleanly on io.EOF or io.ErrUnexpectedEOF (the stream ended,
// e.g. tcpdump was killed), and a non-nil error on anything else, including
// an absurd incl_len that indicates the stream is corrupt / desynced.
func parsePcapStream(r io.Reader, onFrame func(frame []byte, tsMicros uint64)) error {
	var ghdr [24]byte
	if _, err := io.ReadFull(r, ghdr[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		return err
	}

	// Try interpreting the 4 magic bytes as little-endian first; if that
	// doesn't match a known magic, try big-endian. Whichever interpretation
	// yields a recognized magic tells us both the record byte order and the
	// timestamp resolution.
	var order binary.ByteOrder
	var nsResolution bool
	switch binary.LittleEndian.Uint32(ghdr[0:4]) {
	case magicUsLE:
		order, nsResolution = binary.LittleEndian, false
	case magicNsLE:
		order, nsResolution = binary.LittleEndian, true
	default:
		switch binary.BigEndian.Uint32(ghdr[0:4]) {
		case magicUsLE:
			order, nsResolution = binary.BigEndian, false
		case magicNsLE:
			order, nsResolution = binary.BigEndian, true
		default:
			return fmt.Errorf("bcap: unrecognized libpcap magic %#08x", binary.LittleEndian.Uint32(ghdr[0:4]))
		}
	}

	var rhdr [16]byte
	for {
		if _, err := io.ReadFull(r, rhdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		tsSec := order.Uint32(rhdr[0:4])
		tsFrac := order.Uint32(rhdr[4:8])
		inclLen := order.Uint32(rhdr[8:12])

		if inclLen > maxInclLen {
			return fmt.Errorf("bcap: implausible incl_len %d (stream desynced?)", inclLen)
		}

		frame := make([]byte, inclLen)
		if _, err := io.ReadFull(r, frame); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}

		var tsMicros uint64
		if nsResolution {
			tsMicros = uint64(tsSec)*1_000_000 + uint64(tsFrac)/1000
		} else {
			tsMicros = uint64(tsSec)*1_000_000 + uint64(tsFrac)
		}

		onFrame(frame, tsMicros)
	}
}

// pcapngServer accepts Wireshark/GUI clients on a TCP port and streams a
// fresh pcapng to each: every connected client independently gets the
// SHB+IDB header on connect (so it can attach mid-capture) followed by every
// frame broadcast afterwards. This mirrors internal/relay's captureServer
// nearly verbatim — see internal/relay/capture_linux.go — but lives here as
// platform-independent code (it's just net + relay.PcapngWriter, no raw
// sockets) so it can be unit-tested on any OS.
type pcapngServer struct {
	ln   *net.TCPListener
	port int

	mu      sync.Mutex
	clients map[*pcapngClient]struct{}
	closed  bool
}

type pcapngClient struct {
	conn net.Conn
	pw   *relay.PcapngWriter
}

// newPcapngServer binds the pcapng tee listener on bind:port. bind ""
// defaults to loopback.
func newPcapngServer(bind string, port int) (*pcapngServer, error) {
	if bind == "" {
		bind = "127.0.0.1"
	}
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(bind), Port: port})
	if err != nil {
		return nil, err
	}
	s := &pcapngServer{
		ln:      ln,
		port:    ln.Addr().(*net.TCPAddr).Port,
		clients: make(map[*pcapngClient]struct{}),
	}
	go s.accept()
	return s, nil
}

func (s *pcapngServer) accept() {
	for {
		conn, err := s.ln.AcceptTCP()
		if err != nil {
			return
		}
		client := &pcapngClient{conn: conn, pw: relay.NewPcapngWriter(conn)}
		// Send the header immediately so Wireshark/the GUI starts.
		if err := client.pw.WriteHeader(); err != nil {
			_ = conn.Close()
			continue
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.clients[client] = struct{}{}
		s.mu.Unlock()
	}
}

// Broadcast writes one packet block for frame, timestamped at tsMicros, to
// every connected client. Failed clients are dropped.
func (s *pcapngServer) Broadcast(frame []byte, tsMicros uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if err := c.pw.WriteFrame(frame, tsMicros); err != nil {
			_ = c.conn.Close()
			delete(s.clients, c)
		}
	}
}

// Port is the actual TCP port the server listens on.
func (s *pcapngServer) Port() int { return s.port }

// Close stops the listener and disconnects all clients. Idempotent.
func (s *pcapngServer) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	clients := s.clients
	s.clients = make(map[*pcapngClient]struct{})
	s.mu.Unlock()
	_ = s.ln.Close()
	for c := range clients {
		_ = c.conn.Close()
	}
}

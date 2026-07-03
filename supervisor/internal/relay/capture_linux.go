//go:build linux

package relay

import (
	"net"
	"sync"
	"time"
)

// captureServer accepts Wireshark clients on a TCP port and streams a fresh
// pcapng to each. Every connected client independently gets the SHB+IDB header
// (so it can attach mid-capture) followed by every frame broadcast afterwards.
type captureServer struct {
	ln   *net.TCPListener
	port int

	mu      sync.Mutex
	clients map[*captureClient]struct{}
	closed  bool
}

type captureClient struct {
	conn *net.TCPConn
	pw   *PcapngWriter
}

// newCaptureServer binds the pcapng tee listener on bind:port. bind "" defaults
// to loopback; the supervisor's -capture-bind flag threads 0.0.0.0 through
// relay.Config.CaptureBind so a native Wireshark on the GUI host can attach.
func newCaptureServer(bind string, port int) (*captureServer, error) {
	if bind == "" {
		bind = "127.0.0.1"
	}
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(bind), Port: port})
	if err != nil {
		return nil, err
	}
	cs := &captureServer{
		ln:      ln,
		port:    ln.Addr().(*net.TCPAddr).Port,
		clients: make(map[*captureClient]struct{}),
	}
	go cs.accept()
	return cs, nil
}

func (cs *captureServer) accept() {
	for {
		conn, err := cs.ln.AcceptTCP()
		if err != nil {
			return
		}
		client := &captureClient{conn: conn, pw: NewPcapngWriter(conn)}
		// Send the header immediately so Wireshark starts.
		if err := client.pw.WriteHeader(); err != nil {
			_ = conn.Close()
			continue
		}
		cs.mu.Lock()
		if cs.closed {
			cs.mu.Unlock()
			_ = conn.Close()
			return
		}
		cs.clients[client] = struct{}{}
		cs.mu.Unlock()
	}
}

// Broadcast writes a clean ethernet frame to every connected client. Failed
// clients are dropped.
func (cs *captureServer) Broadcast(frame []byte) {
	ts := uint64(time.Now().UnixMicro())
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for c := range cs.clients {
		if err := c.pw.WriteFrame(frame, ts); err != nil {
			_ = c.conn.Close()
			delete(cs.clients, c)
		}
	}
}

// Port is the actual TCP port the capture server listens on.
func (cs *captureServer) Port() int { return cs.port }

// Close stops the listener and disconnects all clients.
func (cs *captureServer) Close() {
	cs.mu.Lock()
	cs.closed = true
	clients := cs.clients
	cs.clients = make(map[*captureClient]struct{})
	cs.mu.Unlock()
	_ = cs.ln.Close()
	for c := range clients {
		_ = c.conn.Close()
	}
}

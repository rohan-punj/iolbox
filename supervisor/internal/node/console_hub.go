package node

import (
	"io"
	"net"
	"sync"

	"github.com/rohanpunj/iolab/supervisor/internal/telnet"
)

// consoleHub multiplexes ONE node's pty console across any number of telnet
// clients simultaneously. It replaces the previous one-client-at-a-time bridge
// (bridgeConsole), whose synchronous accept loop starved every client behind
// the first: the wsbridge webconsole holds its connection for the tab's whole
// lifetime, so a native telnet client's TCP connect completed in the kernel
// backlog but was never serviced ("opens but does not work").
//
// Design:
//
//   - ONE reader goroutine owns ptmx.Read and broadcasts each chunk to every
//     attached client. Clients never read the pty themselves, so concurrent
//     clients cannot steal bytes from each other.
//   - Each client has a BOUNDED output queue pumped by its own writer
//     goroutine. Backpressure policy: a client whose queue is full when a
//     broadcast arrives is DROPPED (connection closed). A console stream is
//     low-bandwidth; a client that falls hubClientQueue chunks behind is dead
//     or unrecoverably slow, and dropping it beats stalling the pty reader or
//     unboundedly buffering. (Documented in supervisor/README.md.)
//   - Client->pty writes are serialized through one mutex so interleaved
//     keystrokes from two clients never split multi-byte sequences written in
//     a single call. Each client keeps its own telnet IAC Negotiator.
//   - A replay ring of the most recent replayRingSize bytes of pty output is
//     sent to every newly attached client first, so a fresh native session
//     shows the current prompt/context instead of a blank screen.
//
// Lifecycle: the hub shuts down when the pty read fails (node exit or
// teardown's ptmx.Close unblocking the read), closing every client. The hub
// never closes the pty itself — Process.teardown owns that.
type consoleHub struct {
	pty io.ReadWriter

	mu      sync.Mutex
	clients map[*hubClient]struct{}
	ring    []byte
	closed  bool

	// wmu serializes all client->pty writes.
	wmu sync.Mutex

	done chan struct{}
	once sync.Once
}

// replayRingSize is how many bytes of recent pty output are kept for replay to
// newly attached clients (enough for a screenful of prompt/context).
const replayRingSize = 8 * 1024

// hubClientQueue is the per-client output queue depth, in broadcast chunks
// (each up to 4096 bytes). A client this far behind is dropped.
const hubClientQueue = 64

type hubClient struct {
	conn net.Conn
	out  chan []byte
	stop chan struct{}
	once sync.Once
}

// newConsoleHub starts the hub's pty reader goroutine and returns the hub.
func newConsoleHub(pty io.ReadWriter) *consoleHub {
	h := &consoleHub{
		pty:     pty,
		clients: make(map[*hubClient]struct{}),
		done:    make(chan struct{}),
	}
	go h.readLoop()
	return h
}

// readLoop is the single owner of pty reads: it appends each chunk to the
// replay ring and enqueues it to every client, dropping clients whose queue is
// full. Exits (and shuts the hub down) when the pty read errors — node exit or
// teardown closing the pty master.
func (h *consoleHub) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := h.pty.Read(buf)
		if n > 0 {
			// Copy out of the reused read buffer before it escapes to queues.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			h.broadcast(chunk)
		}
		if err != nil {
			h.shutdown()
			return
		}
	}
}

// broadcast appends chunk to the replay ring and enqueues it to every client.
// Clients with a full queue are dropped (see backpressure policy above).
func (h *consoleHub) broadcast(chunk []byte) {
	var dropped []*hubClient
	h.mu.Lock()
	// Ring append, trimmed to the newest replayRingSize bytes.
	h.ring = append(h.ring, chunk...)
	if over := len(h.ring) - replayRingSize; over > 0 {
		h.ring = append([]byte(nil), h.ring[over:]...)
	}
	for c := range h.clients {
		select {
		case c.out <- chunk:
		default:
			delete(h.clients, c)
			dropped = append(dropped, c)
		}
	}
	h.mu.Unlock()
	for _, c := range dropped {
		c.close()
	}
}

// attach registers a telnet client: it volunteers WILL ECHO + WILL SGA (so
// line-mode clients switch to character-at-a-time and let the node own echo),
// replays the recent-output ring, then joins the broadcast set. STRICTLY
// non-blocking: the preamble and replay are enqueued on the client's queue and
// written by its writer goroutine, so a slow (or dead) client can never stall
// the caller — the accept loop services every connection immediately. If the
// hub is already shut down the connection is closed instead.
func (h *consoleHub) attach(conn net.Conn) {
	c := &hubClient{
		conn: conn,
		out:  make(chan []byte, hubClientQueue),
		stop: make(chan struct{}),
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	// Queue the telnet preamble (server-side echo + suppress-go-ahead, matching
	// a real Cisco console; a dumb raw client just discards these valid
	// commands), then the replay ring — both under mu so no broadcast can
	// interleave before registration. The fresh queue always has room for two.
	c.out <- []byte{
		telnet.IAC, telnet.WILL, telnet.OptEcho,
		telnet.IAC, telnet.WILL, telnet.OptSGA,
	}
	if len(h.ring) > 0 {
		replay := make([]byte, len(h.ring))
		copy(replay, h.ring)
		c.out <- replay
	}
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	// Writer: pump queued pty output to the client.
	go func() {
		for {
			select {
			case chunk := <-c.out:
				if _, err := conn.Write(chunk); err != nil {
					h.detach(c)
					return
				}
			case <-c.stop:
				return
			}
		}
	}()

	// Reader: strip/answer IAC, forward clean bytes to the pty (serialized).
	// Negotiation replies are routed through the client's out queue — NOT
	// written directly — so the writer goroutine stays the connection's single
	// writer and a reply can never interleave into the middle of a data chunk.
	go func() {
		defer h.detach(c)
		neg := telnet.NewNegotiator()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				clean := neg.Feed(buf[:n])
				if reply := neg.Reply(); reply != nil {
					select {
					case c.out <- reply:
					default:
						return // queue jammed — same drop policy as broadcast
					}
				}
				if len(clean) > 0 {
					h.wmu.Lock()
					_, werr := h.pty.Write(clean)
					h.wmu.Unlock()
					if werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
}

// detach unregisters and closes one client. Idempotent.
func (h *consoleHub) detach(c *hubClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.close()
}

// close closes the client connection and stops its writer. Idempotent.
func (c *hubClient) close() {
	c.once.Do(func() {
		close(c.stop)
		_ = c.conn.Close()
	})
}

// shutdown closes every client and marks the hub closed. Idempotent. Called
// when the pty read fails; also safe to call from teardown paths.
func (h *consoleHub) shutdown() {
	h.once.Do(func() {
		h.mu.Lock()
		h.closed = true
		clients := make([]*hubClient, 0, len(h.clients))
		for c := range h.clients {
			clients = append(clients, c)
		}
		h.clients = make(map[*hubClient]struct{})
		h.mu.Unlock()
		for _, c := range clients {
			c.close()
		}
		close(h.done)
	})
}

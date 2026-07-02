// Package wsbridge exposes the supervisor's control protocol and node
// consoles over WebSocket so browsers (which cannot open a raw TCP socket or
// telnet) can drive iolab: the desktop app's embedded webview and a plain
// browser build both talk to the same two endpoints.
//
//   - GET /control          — NDJSON control protocol, one JSON object per
//     text frame, dispatched through the exact same
//     handler core as the TCP control listener
//     (server.ServeConn). Functionally identical to
//     the TCP path.
//   - GET /console/{nodeId} — bridges to that node's telnet console port.
//     Binary WS frames carry raw terminal bytes after
//     server-side telnet IAC negotiation; a JSON text
//     frame ({"resize":{"cols":C,"rows":R}}) may be
//     sent at any time to propagate a NAWS window-size
//     update to the node.
//
// The listener also serves the embedded browser GUI (internal/web) as its "/"
// catch-all, so the whole product ships as one binary: a browser opens
// http://<vm-ip>:4001/ and the served page connects back to
// ws://<vm-ip>:4001/control on the same origin. Because browser access needs a
// routable address, this listener may bind a non-loopback host (e.g.
// 0.0.0.0:4001) — unlike internal/server's raw control socket, which stays
// loopback-only. The WebSocket handshake (internal/ws.Accept) performs no
// Origin check, so a same-origin page served from <vm-ip>:4001 (Origin:
// http://<vm-ip>:4001) is accepted; the trust boundary is the VM's own network
// exposure, which the operator controls via -ws-addr.
package wsbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rohanpunj/iolab/supervisor/internal/telnet"
	"github.com/rohanpunj/iolab/supervisor/internal/web"
	"github.com/rohanpunj/iolab/supervisor/internal/ws"
)

// ControlServer is the subset of *server.Server the bridge needs: the shared
// NDJSON connection core and console-port lookup. Kept as an interface so
// this package doesn't import internal/server's concrete type and so the
// connection core can be exercised with a fake in tests.
type ControlServer interface {
	// ServeConn runs the NDJSON control loop over rwc until it ends or ctx is
	// cancelled (see server.Server.ServeConn).
	ServeConn(ctx context.Context, rwc io.ReadWriteCloser)
	// ConsolePort returns the allocated telnet console port for nodeID in the
	// currently loaded lab.
	ConsolePort(nodeID int) (port int, ok bool)
}

// Config configures a Bridge.
type Config struct {
	// Addr is the bind address (must be loopback), e.g. "127.0.0.1:4001".
	Addr string
	// ImageDir is where POST /api/upload/image writes uploaded image files (the
	// same directory the server registers images from). Empty disables the
	// upload endpoint, which then 503s.
	ImageDir string
	// DialConsole dials a node's local telnet console port. Defaults to
	// net.Dial("tcp", "127.0.0.1:<port>") when nil; overridable for tests.
	DialConsole func(port int) (net.Conn, error)
}

// Bridge is the WebSocket listener exposing /control and /console/{nodeId}.
type Bridge struct {
	cfg    Config
	ctrl   ControlServer
	server *http.Server
}

// New builds a Bridge. srv provides the shared control core and console port
// lookups.
func New(cfg Config, srv ControlServer) *Bridge {
	if cfg.DialConsole == nil {
		cfg.DialConsole = func(port int) (net.Conn, error) {
			return net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
		}
	}
	b := &Bridge{cfg: cfg, ctrl: srv}

	mux := http.NewServeMux()
	mux.HandleFunc("/control", b.handleControl)
	mux.HandleFunc("/console/", b.handleConsole)
	// Exact route: the GUI PUTs an image file body here, then registers it over
	// WS with the returned path. The exact "/api/upload/image" pattern is more
	// specific than the "/" catch-all, so ServeMux routes it here, not to the SPA.
	mux.HandleFunc("/api/upload/image", b.handleUploadImage)
	// Catch-all "/" serves the embedded GUI. ServeMux prefers the longer, more
	// specific patterns above, so /control and /console/ are never shadowed by
	// this fallback; everything else (the SPA and its assets) falls through
	// here. Registered last only for readability — mux precedence is by pattern
	// specificity, not registration order.
	mux.Handle("/", web.Handler())
	b.server = &http.Server{Handler: mux}
	return b
}

// ListenAndServe binds cfg.Addr and serves until ctx is cancelled.
//
// Unlike internal/server's control listener (which stays loopback-only, as it
// is the raw local NDJSON socket), the ws bridge may bind a non-loopback host
// (e.g. 0.0.0.0:4001) on purpose: it now also serves the browser GUI, and a
// browser on the Windows host must reach the VM's IP. We still validate the
// address parses as host:port so a typo fails fast rather than at Listen time.
func (b *Bridge) ListenAndServe(ctx context.Context) error {
	if _, _, err := net.SplitHostPort(b.cfg.Addr); err != nil {
		return fmt.Errorf("ws-addr %q: %w", b.cfg.Addr, err)
	}

	ln, err := net.Listen("tcp", b.cfg.Addr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = b.server.Shutdown(shutdownCtx)
	}()

	if err := b.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// handleControl upgrades to WebSocket and runs the shared NDJSON control loop
// over it, exactly as the TCP listener does for a raw socket.
func (b *Bridge) handleControl(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.Accept(w, r)
	if err != nil {
		log.Printf("wsbridge: /control handshake: %v", err)
		return
	}
	defer conn.Close()

	rwc := &textFrameRWC{conn: conn}
	b.ctrl.ServeConn(r.Context(), rwc)
}

// nodeIDFromPath extracts the {nodeId} segment from "/console/{nodeId}".
func nodeIDFromPath(path string) (int, error) {
	rest := strings.TrimPrefix(path, "/console/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return 0, fmt.Errorf("missing node id")
	}
	return strconv.Atoi(rest)
}

// handleConsole upgrades to WebSocket, dials the node's telnet console port,
// and pumps bytes bidirectionally: node->client is telnet-IAC-cleaned and
// sent as binary frames; client->node is either a resize control message
// (text frame) or raw keystrokes (binary frame) written straight through.
func (b *Bridge) handleConsole(w http.ResponseWriter, r *http.Request) {
	nodeID, err := nodeIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	port, ok := b.ctrl.ConsolePort(nodeID)
	if !ok {
		http.Error(w, fmt.Sprintf("node %d has no allocated console (load/start the lab first)", nodeID), http.StatusNotFound)
		return
	}

	telnetConn, err := b.cfg.DialConsole(port)
	if err != nil {
		http.Error(w, fmt.Sprintf("dial console port %d: %v", port, err), http.StatusBadGateway)
		return
	}
	defer telnetConn.Close()

	wsConn, err := ws.Accept(w, r)
	if err != nil {
		log.Printf("wsbridge: /console/%d handshake: %v", nodeID, err)
		return
	}
	defer wsConn.Close()

	bridgeConsole(r.Context(), wsConn, telnetConn)
}

// bridgeConsole runs the bidirectional pump for one console session until
// either side closes or the context is cancelled. It is exported at package
// level (not a method) so it's independently testable against fake
// net.Conn/ws.Conn-shaped pipes.
func bridgeConsole(ctx context.Context, wsConn *ws.Conn, telnetConn net.Conn) {
	done := make(chan struct{})
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			_ = wsConn.Close()
			_ = telnetConn.Close()
			close(done)
		})
	}

	go func() {
		select {
		case <-ctx.Done():
			closeAll()
		case <-done:
		}
	}()

	neg := telnet.NewNegotiator()

	// node -> browser: read raw telnet bytes, strip/answer IAC negotiation,
	// forward clean bytes as binary WS frames. Negotiation replies go back
	// to the node over the same telnet socket.
	go func() {
		defer closeAll()
		buf := make([]byte, 4096)
		for {
			n, err := telnetConn.Read(buf)
			if n > 0 {
				clean := neg.Feed(buf[:n])
				if reply := neg.Reply(); reply != nil {
					if _, werr := telnetConn.Write(reply); werr != nil {
						return
					}
				}
				if len(clean) > 0 {
					if werr := wsConn.WriteMessage(ws.OpBinary, clean); werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// browser -> node: keystrokes (binary frames) go straight to the telnet
	// socket; a resize control message (text frame, {"resize":{...}}) is
	// translated to a NAWS subnegotiation instead of being forwarded as data.
	go func() {
		defer closeAll()
		for {
			op, data, err := wsConn.ReadMessage()
			if err != nil {
				return
			}
			switch op {
			case ws.OpBinary:
				if _, werr := telnetConn.Write(data); werr != nil {
					return
				}
			case ws.OpText:
				if err := handleTextFrame(telnetConn, data); err != nil {
					log.Printf("wsbridge: console resize frame: %v", err)
				}
			}
		}
	}()

	<-done
}

// resizeMessage is the client->server control frame requesting a NAWS update.
// See doc comment on package wsbridge and supervisor/README.md for the
// documented framing.
type resizeMessage struct {
	Resize *struct {
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	} `json:"resize"`
}

// handleTextFrame parses a text-frame control message from the client and, if
// it is a resize request, writes the NAWS subnegotiation to the node.
func handleTextFrame(telnetConn net.Conn, data []byte) error {
	var msg resizeMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("invalid control frame: %w", err)
	}
	if msg.Resize == nil {
		return fmt.Errorf("unrecognized control frame (want {\"resize\":{\"cols\":C,\"rows\":R}})")
	}
	_, err := telnetConn.Write(telnet.NAWS(msg.Resize.Cols, msg.Resize.Rows))
	return err
}

// textFrameRWC adapts a *ws.Conn to io.ReadWriteCloser for
// protocol.Decoder/Encoder, which only need Read/Write/Close: each Write call
// becomes one WS text frame (one NDJSON line per protocol.Encoder.writeJSON
// call), and Read serves buffered bytes from incoming text frames,
// re-appending the newline the NDJSON decoder expects as the line terminator
// (WS frames carry exactly one JSON object with no trailing newline).
type textFrameRWC struct {
	conn *ws.Conn
	buf  []byte // unread bytes from the most recently received frame
}

func (t *textFrameRWC) Read(p []byte) (int, error) {
	for len(t.buf) == 0 {
		op, data, err := t.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if op != ws.OpText {
			// Ignore non-text frames on the control channel (shouldn't occur
			// from a spec-following client); keep waiting for a text frame.
			continue
		}
		// The NDJSON decoder splits on '\n'; each WS text frame is one
		// message with no trailing newline, so append one.
		t.buf = append(append([]byte(nil), data...), '\n')
	}
	n := copy(p, t.buf)
	t.buf = t.buf[n:]
	return n, nil
}

func (t *textFrameRWC) Write(p []byte) (int, error) {
	// protocol.Encoder writes one JSON object + '\n' per call; trim the
	// trailing newline since each WS text frame is already one message.
	msg := p
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	if err := t.conn.WriteMessage(ws.OpText, msg); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *textFrameRWC) Close() error {
	return t.conn.Close()
}

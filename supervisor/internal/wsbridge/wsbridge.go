// Package wsbridge exposes the supervisor's control protocol and node
// consoles over WebSocket so browsers (which cannot open a raw TCP socket or
// telnet) can drive iolbox: the desktop app's embedded webview and a plain
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
//   - GET /capture/{linkId} — bridges to that link's active pcapng capture
//     port. The raw pcapng byte stream is delivered as
//     binary WS frames so a browser can render a live
//     packet view; client->server frames are ignored.
//     404s (JSON error body) when the link has no
//     active capture.
//
// The listener also serves the embedded browser GUI (internal/web) as its "/"
// catch-all, so the whole product ships as one binary: a browser opens
// http://<vm-ip>:4001/ and the served page connects back to
// ws://<vm-ip>:4001/control on the same origin. Because browser access needs a
// routable address, this listener may bind a non-loopback host (e.g.
// 0.0.0.0:4001) — unlike internal/server's raw control socket, which stays
// loopback-only. Network-exposed bridge routes require the boot session token
// and a same-origin Origin/Referer check before internal/ws.Accept runs.
package wsbridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/node"
	"github.com/rohanpunj/iolbox/supervisor/internal/telnet"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
	"github.com/rohanpunj/iolbox/supervisor/internal/web"
	"github.com/rohanpunj/iolbox/supervisor/internal/ws"
)

// ControlServer is the subset of *server.Server the bridge needs: the shared
// NDJSON connection core, console-port lookup, and in-process console
// subscription. Kept as an interface so this package doesn't import
// internal/server's concrete type and so the connection core can be
// exercised with a fake in tests.
type ControlServer interface {
	// ServeConn runs the NDJSON control loop over rwc until it ends or ctx is
	// cancelled (see server.Server.ServeConn).
	ServeConn(ctx context.Context, rwc io.ReadWriteCloser)
	// ConsolePort returns the allocated telnet console port for nodeID in the
	// currently loaded lab. Used for the native/OS-telnet URL the frontend
	// opens directly (telnet://host:port) — that connection still terminates
	// at a real TCP socket (the hub's own listener, see
	// node.spawnIOL/serveConsole), which since v0.3.0 Phase 3 routes its
	// decoded keystrokes through the SAME input-arbitration gate the web
	// console's in-process Subscription uses, so native gets turn-arbitration
	// protection too even though it still dials a socket at the OS level.
	ConsolePort(nodeID int) (port int, ok bool)
	// ConsoleSubscribe attaches an in-process subscriber to nodeID's console
	// hub (see node.Process.Subscribe/consoleHub.Subscribe) so the web console
	// consumes decoded output and writes keystrokes without dialing
	// ConsolePort over TCP. Returns nil if the node has no running console hub
	// (not started, no such node/lab, or a VPCS node with no hub at all).
	ConsoleSubscribe(nodeID int) *node.Subscription
	// CapturePort returns the local TCP port serving the live pcapng stream for
	// linkID in the currently loaded lab, if that link has an active capture.
	CapturePort(linkID int) (port int, ok bool)
	// ToolProxyTarget returns the running tool GUI's AF_UNIX socket and its
	// manifest-declared path routes.
	ToolProxyTarget(nodeID int) (socket string, routes []tool.ProxyRoute, ok bool)
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
	//
	// PERMANENT fallback, not a transitional shim: the web console uses this
	// only for node kinds with no console hub at all — VPCS is its own telnet
	// server (see node.Process's doc comment) and is explicitly out of scope
	// for console unification (docs/v0.3.0-console-unification.md §5
	// non-goals: "VPCS nodes are out of scope... nothing here changes their
	// console path"). Every hub-owned node kind (IOL) instead subscribes
	// in-process via ControlServer.ConsoleSubscribe (see handleConsole) with
	// zero TCP hop and zero Negotiator of its own. This field — and
	// bridgeConsole's telnet.Negotiator below — are the one intentionally
	// surviving external-telnet-dial path in the whole supervisor, required
	// because VPCS has no hub to own a Negotiator on its behalf.
	DialConsole func(port int) (net.Conn, error)
	// DialCapture dials a link's local pcapng capture port. Defaults to
	// net.Dial("tcp", "127.0.0.1:<port>") when nil; overridable for tests.
	DialCapture func(port int) (net.Conn, error)
}

// Bridge is the WebSocket listener exposing /control and /console/{nodeId}.
type Bridge struct {
	cfg          Config
	ctrl         ControlServer
	sessionToken string
	server       *http.Server
}

// New builds a Bridge. srv provides the shared control core and console port
// lookups.
func New(cfg Config, srv ControlServer) *Bridge {
	if cfg.DialConsole == nil {
		cfg.DialConsole = func(port int) (net.Conn, error) {
			return net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
		}
	}
	if cfg.DialCapture == nil {
		cfg.DialCapture = func(port int) (net.Conn, error) {
			return net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
		}
	}
	b := &Bridge{cfg: cfg, ctrl: srv, sessionToken: newSessionToken()}

	mux := http.NewServeMux()
	toolHandler := b.requireSession(b.handleTool)
	mux.HandleFunc("/control", b.requireSession(b.handleControl))
	mux.HandleFunc("/console/", b.requireSession(b.handleConsole))
	mux.HandleFunc("/capture/", b.requireSession(b.handleCapture))
	mux.HandleFunc("/tool/", toolHandler)
	// Exact route: the GUI PUTs an image file body here, then registers it over
	// WS with the returned path. The exact "/api/upload/image" pattern is more
	// specific than the "/" catch-all, so ServeMux routes it here, not to the SPA.
	mux.HandleFunc("/api/upload/image", b.handleUploadImage)
	// Catch-all "/" serves the embedded GUI. ServeMux prefers the longer, more
	// specific patterns above, so /control and /console/ are never shadowed by
	// this fallback; everything else (the SPA and its assets) falls through
	// here. Registered last only for readability — mux precedence is by pattern
	// specificity, not registration order.
	mux.Handle("/", b.handleSPA(web.Handler()))
	b.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ServeMux canonicalizes dot segments before a matching handler runs.
		// Check both decoded and raw URL forms first so a tool path can never be
		// normalized into /control or another bridge endpoint.
		if r.URL.Path == "/tool" || strings.HasPrefix(r.URL.Path, "/tool/") {
			if toolPathHasTraversal(r.URL.Path, r.URL.RawPath) {
				http.NotFound(w, r)
				return
			}
			toolHandler.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})}
	return b
}

func newSessionToken() string {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(fmt.Sprintf("wsbridge: mint session token: %v", err))
	}
	return hex.EncodeToString(token[:])
}

func (b *Bridge) handleSPA(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     "iolbox_session",
			Value:    b.sessionToken,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
		})
		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !b.validSession(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !b.sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (b *Bridge) validSession(r *http.Request) bool {
	if cookie, err := r.Cookie("iolbox_session"); err == nil &&
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(b.sessionToken)) == 1 {
		return true
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") &&
		subtle.ConstantTimeCompare([]byte(parts[1]), []byte(b.sessionToken)) == 1
}

func (b *Bridge) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if isWebSocketUpgrade(r) && origin == "" {
		return false
	}
	if !isWebSocketUpgrade(r) && origin == "" {
		origin = r.Header.Get("Referer")
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) &&
		normalizedOriginHost(parsed.Host) == normalizedOriginHost(r.Host)
}

func normalizedOriginHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func toolPathHasTraversal(paths ...string) bool {
	for _, candidate := range paths {
		if candidate == "" {
			continue
		}
		decoded, err := url.PathUnescape(candidate)
		if err != nil {
			return true
		}
		for _, part := range strings.Split(decoded, "/") {
			if part == ".." {
				return true
			}
		}
	}
	return false
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

// handleConsole upgrades to WebSocket and bridges it to the node's console,
// pumping bytes bidirectionally: node->client as binary frames; client->node
// is either a resize control message (text frame) or raw keystrokes (binary
// frame).
//
// For a node with a running console hub (IOL), this attaches in-process via
// ControlServer.ConsoleSubscribe — no TCP dial, no telnet.Negotiator of its
// own, since the hub already decoded the byte stream once (see
// internal/node/console_hub.go) and arbitrates web keystrokes against any
// concurrent programmatic turn (v0.3.0 Phase 3/4). A node with no hub at all
// (VPCS, which is its own telnet server and explicitly out of scope for
// console unification — docs/v0.3.0-console-unification.md §5) falls back to
// the permanent dial-ConsolePort-directly path (see Config.DialConsole).
func (b *Bridge) handleConsole(w http.ResponseWriter, r *http.Request) {
	nodeID, err := nodeIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if sub := b.ctrl.ConsoleSubscribe(nodeID); sub != nil {
		wsConn, err := ws.Accept(w, r)
		if err != nil {
			log.Printf("wsbridge: /console/%d handshake: %v", nodeID, err)
			sub.Unsubscribe()
			return
		}
		defer wsConn.Close()
		bridgeConsoleSub(r.Context(), wsConn, sub)
		return
	}

	// Fallback: no console hub for this node (VPCS, or not started/loaded).
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
				// Resize is a documented no-op here too — see validateResizeFrame's
				// doc comment for why: VPCS's own telnet server was never actually
				// negotiated to accept a NAWS subnegotiation (our Negotiator only
				// ever agrees to SGA, refusing NAWS outright), so writing raw NAWS
				// bytes into its input stream regardless was not a working feature
				// — it was an un-negotiated, unsolicited subnegotiation that VPCS's
				// small telnet parser could fail to fully consume, leaking a
				// trailing byte into the command line as a phantom keystroke
				// (confirmed live: a stray digit matching the low byte of a resize
				// dimension appeared at the VPCS prompt right after a terminal
				// resize). Parse-and-ignore, matching bridgeConsoleSub's IOL path.
				if err := validateResizeFrame(data); err != nil {
					log.Printf("wsbridge: console resize frame: %v", err)
				}
			}
		}
	}()

	<-done
}

// bridgeConsoleSub runs the bidirectional pump for one in-process console
// subscription (v0.3.0 Phase 2) until either side closes or the context is
// cancelled. Mirrors bridgeConsole's structure and framing exactly, minus the
// telnet.Negotiator: sub.Out already delivers telnet-IAC-free application
// bytes (the hub's single Negotiator — internal/node/console_hub.go — decoded
// them once for every subscriber), so there is nothing to strip/answer here.
// Exported at package level (not a method) so it's independently testable
// against a fake hub subscription.
//
// Resize: exactly as before Phase 2, a {"resize":{...}} control frame is a
// documented no-op for IOL — internal/telnet's Negotiator has never acted on
// an inbound NAWS subnegotiation (see handleSubnegotiation's doc comment),
// so sending a NAWS sequence into the old telnet socket was already silently
// absorbed with zero effect on the pty. Preserving that exact (non-)behavior
// here keeps the console "behaviorally identical" per the refactor's
// guardrail; a real pty-resize (pty.Setsize) is a separate, not-yet-designed
// feature, not something this refactor should introduce as a side effect.
func bridgeConsoleSub(ctx context.Context, wsConn *ws.Conn, sub *node.Subscription) {
	done := make(chan struct{})
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			_ = wsConn.Close()
			sub.Unsubscribe()
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

	// node -> browser: sub.Out already carries clean application bytes (the
	// hub decoded telnet once); forward each chunk as a binary WS frame.
	go func() {
		defer closeAll()
		for {
			select {
			case chunk, ok := <-sub.Out:
				if !ok {
					return
				}
				if len(chunk) > 0 {
					if werr := wsConn.WriteMessage(ws.OpBinary, chunk); werr != nil {
						return
					}
				}
			case <-done:
				return
			}
		}
	}()

	// browser -> node: keystrokes (binary frames) go straight to the pty via
	// the subscription's Write (serialized against every other writer by the
	// hub's write mutex); a resize control message (text frame) is parsed and
	// discarded — see the no-op rationale above.
	go func() {
		defer closeAll()
		for {
			op, data, err := wsConn.ReadMessage()
			if err != nil {
				return
			}
			switch op {
			case ws.OpBinary:
				if werr := sub.Write(data); werr != nil {
					return
				}
			case ws.OpText:
				if err := validateResizeFrame(data); err != nil {
					log.Printf("wsbridge: console resize frame: %v", err)
				}
			}
		}
	}()

	<-done
}

// linkIDFromPath extracts the {linkId} segment from "/capture/{linkId}".
func linkIDFromPath(path string) (int, error) {
	rest := strings.TrimPrefix(path, "/capture/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return 0, fmt.Errorf("missing link id")
	}
	return strconv.Atoi(rest)
}

// handleCapture upgrades to WebSocket, dials the link's active pcapng capture
// port over loopback, and pumps the raw pcapng byte stream to the client as
// binary WS frames until either side closes. A link with no active capture
// gets a 404 with a JSON error body (written before any upgrade, so the client
// sees an ordinary HTTP error rather than a half-open socket). Client->server
// frames on this socket carry no meaning and are drained. Mirrors
// handleConsole's structure.
func (b *Bridge) handleCapture(w http.ResponseWriter, r *http.Request) {
	linkID, err := linkIDFromPath(r.URL.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	port, ok := b.ctrl.CapturePort(linkID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("link %d has no active capture (start a capture first)", linkID))
		return
	}

	capConn, err := b.cfg.DialCapture(port)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("dial capture port %d: %v", port, err))
		return
	}
	defer capConn.Close()

	wsConn, err := ws.Accept(w, r)
	if err != nil {
		log.Printf("wsbridge: /capture/%d handshake: %v", linkID, err)
		return
	}
	defer wsConn.Close()

	bridgeCapture(r.Context(), wsConn, capConn)
}

// writeJSONError writes a {"error":"..."} body with the given status. Used by
// the capture handler so a pre-upgrade failure is machine-readable (the console
// handler uses http.Error plain text; capture clients expect JSON).
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: msg})
}

// bridgeCapture runs the one-directional pump for one capture session:
// capture-port bytes flow to the browser as binary WS frames; anything the
// client sends is read and discarded so its read loop's close is observed.
// Runs until either side closes or the context is cancelled. Exported at
// package level (not a method) so it's independently testable against fake
// net.Conn/ws.Conn-shaped pipes, like bridgeConsole.
func bridgeCapture(ctx context.Context, wsConn *ws.Conn, capConn net.Conn) {
	done := make(chan struct{})
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			_ = wsConn.Close()
			_ = capConn.Close()
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

	// capture -> browser: forward the raw pcapng stream as binary WS frames.
	go func() {
		defer closeAll()
		buf := make([]byte, 32768)
		for {
			n, err := capConn.Read(buf)
			if n > 0 {
				if werr := wsConn.WriteMessage(ws.OpBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// browser -> server: drain and ignore. The capture channel is one-way; we
	// only read so that a client-side close surfaces as an error and tears the
	// session down.
	go func() {
		defer closeAll()
		for {
			if _, _, err := wsConn.ReadMessage(); err != nil {
				return
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

// validateResizeFrame parses a text-frame control message ({"resize":{...}})
// and otherwise ignores it — for BOTH console paths now (bridgeConsole's raw
// telnet dial to VPCS, and bridgeConsoleSub's in-process IOL subscription).
// It used to write a NAWS subnegotiation into the VPCS telnet socket, but
// that was never actually negotiated (see the OpText case in bridgeConsole)
// and could leak a stray byte into VPCS's input; IOL's path was already a
// documented no-op (there is no telnet socket to write to — a real
// pty-resize via pty.Setsize would be a separate, not-yet-designed feature).
// Returning the parse error (if any) lets the caller log a malformed frame.
func validateResizeFrame(data []byte) error {
	_, err := parseResizeFrame(data)
	return err
}

// parseResizeFrame is the shared validation validateResizeFrame builds on.
func parseResizeFrame(data []byte) (resizeMessage, error) {
	var msg resizeMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return msg, fmt.Errorf("invalid control frame: %w", err)
	}
	if msg.Resize == nil {
		return msg, fmt.Errorf("unrecognized control frame (want {\"resize\":{\"cols\":C,\"rows\":R}})")
	}
	return msg, nil
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

func (t *textFrameRWC) SetWriteDeadline(deadline time.Time) error {
	return t.conn.SetWriteDeadline(deadline)
}

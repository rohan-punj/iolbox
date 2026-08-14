package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// wsclient.go — a minimal hand-rolled RFC 6455 WebSocket client. Stdlib only.
// Just enough to drive the supervisor's /control NDJSON verb channel: a single
// long-lived client-initiated connection, text frames only, client->server
// frames masked per spec, server->server frames read unmasked, ping/pong and
// close handled. Not a general-purpose WS implementation.

const (
	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ---- Handshake -------------------------------------------------------

// wsDial performs the TCP dial + HTTP/1.1 Upgrade handshake against
// ws://host:port/path and returns the raw, now-upgraded TCP connection.
func wsDial(addr, path string) (net.Conn, error) {
	return wsDialWithHeaders(addr, path, nil)
}

// wsDialWithHeaders is wsDial plus arbitrary extra request headers (e.g.
// Cookie/Origin, required by the GUI-facing /control bridge's session and
// same-origin checks — see wsbridge.requireSession/sameOrigin). The
// guest-loopback control socket has no such requirement, so callers that
// don't need it keep using plain wsDial.
func wsDialWithHeaders(addr, path string, extraHeaders map[string]string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ws dial %s: %w", addr, err)
	}

	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws handshake: rand: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)

	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n"
	for k, v := range extraHeaders {
		req += k + ": " + v + "\r\n"
	}
	req += "\r\n"

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws handshake write: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws handshake read: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf("ws handshake: expected 101, got %d", resp.StatusCode)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		conn.Close()
		return nil, fmt.Errorf("ws handshake: missing/invalid Upgrade header %q", resp.Header.Get("Upgrade"))
	}
	wantAccept := wsAcceptKey(key)
	if resp.Header.Get("Sec-WebSocket-Accept") != wantAccept {
		conn.Close()
		return nil, fmt.Errorf("ws handshake: Sec-WebSocket-Accept mismatch (got %q, want %q)",
			resp.Header.Get("Sec-WebSocket-Accept"), wantAccept)
	}
	_ = conn.SetDeadline(time.Time{})

	// bufio.Reader may have buffered bytes beyond the HTTP response (the start
	// of the first WS frame, if the server was fast). Wrap conn so those bytes
	// aren't lost.
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// fetchSessionCookie performs a plain GET / against the GUI's HTTP origin
// and returns the iolbox_session cookie value the SPA handler sets
// (wsbridge.go, http.SetCookie). This is the same cookie a browser's cookie
// jar would carry into a subsequent /control WebSocket dial.
func fetchSessionCookie(addr string) (string, error) {
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		return "", fmt.Errorf("GET / for session cookie: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	for _, c := range resp.Cookies() {
		if c.Name == "iolbox_session" && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("GET / response carried no iolbox_session cookie")
}

// wsAcceptKey computes the Sec-WebSocket-Accept value for a given client key.
func wsAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// bufferedConn lets us keep using a net.Conn after peeking at it through a
// bufio.Reader during the handshake, without losing pre-buffered bytes.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// ---- Framing (pure functions, unit-testable over bytes.Buffer) -------

// writeTextFrame writes one client->server masked text frame (FIN=1,
// opcode=0x1) with the given maskKey.
func writeTextFrame(w io.Writer, payload []byte, maskKey [4]byte) error {
	return writeFrame(w, wsOpText, payload, &maskKey)
}

// writeControlFrame writes a masked control frame (pong/close) for client use.
func writeControlFrame(w io.Writer, opcode byte, payload []byte, maskKey [4]byte) error {
	return writeFrame(w, opcode, payload, &maskKey)
}

// writeFrame writes a single frame. If maskKey is non-nil the payload is
// masked and MASK=1 is set (client->server). If nil, the frame is sent
// unmasked (used only in tests to emulate a server).
func writeFrame(w io.Writer, opcode byte, payload []byte, maskKey *[4]byte) error {
	var hdr [14]byte
	hdr[0] = 0x80 | (opcode & 0x0F) // FIN=1, opcode
	maskBit := byte(0)
	if maskKey != nil {
		maskBit = 0x80
	}

	n := len(payload)
	var hdrLen int
	switch {
	case n <= 125:
		hdr[1] = maskBit | byte(n)
		hdrLen = 2
	case n <= 0xFFFF:
		hdr[1] = maskBit | 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		hdrLen = 4
	default:
		hdr[1] = maskBit | 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		hdrLen = 10
	}

	if maskKey != nil {
		copy(hdr[hdrLen:hdrLen+4], maskKey[:])
		hdrLen += 4
	}

	if _, err := w.Write(hdr[:hdrLen]); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}

	if maskKey == nil {
		_, err := w.Write(payload)
		return err
	}

	masked := make([]byte, n)
	for i, b := range payload {
		masked[i] = b ^ maskKey[i%4]
	}
	_, err := w.Write(masked)
	return err
}

// readFrame reads a single WebSocket frame from r and returns its opcode and
// UNMASKED payload (masking, if present per the MASK bit, is undone
// automatically — used when the client reads a masked frame in tests).
// Continuation frames and non-FIN frames are treated as errors (v1
// simplification: the supervisor's /control writes exactly one frame per
// NDJSON message).
func readFrame(r io.Reader) (opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	fin := hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0F
	if !fin {
		return 0, nil, fmt.Errorf("ws: non-FIN / fragmented frame not supported (opcode %d)", opcode)
	}
	masked := hdr[1]&0x80 != 0
	length := uint64(hdr[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(r, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}

	payload = make([]byte, length)
	if length > 0 {
		if _, err = io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}

// newMaskKey generates a fresh client->server masking key via crypto/rand.
func newMaskKey() ([4]byte, error) {
	var k [4]byte
	if _, err := rand.Read(k[:]); err != nil {
		return k, err
	}
	return k, nil
}

// ---- Request/response client -----------------------------------------

// wsRPCError mirrors the {"code","message"} error object in a failed
// response envelope.
type wsRPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *wsRPCError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// envelope is the NDJSON request/response shape used by /control.
type envelope struct {
	ID     string          `json:"id,omitempty"`
	Op     string          `json:"op,omitempty"`
	Args   any             `json:"args,omitempty"`
	OK     *bool           `json:"ok,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wsRPCError     `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
}

// controlWSClient is a single long-lived connection to ws://host:port/control
// implementing request/response over NDJSON text frames, serialized through a
// mutex (one WS connection cannot interleave multiple in-flight requests).
type controlWSClient struct {
	conn net.Conn
	br   *bufio.Reader

	mu      sync.Mutex // serializes request/response round-trips
	nextID  int64
	timeout time.Duration
}

// wsDialWithSession is wsDial plus the session cookie and Origin header the
// GUI bridge requires on every one of its WebSocket routes -- /control,
// /console/{nodeId}, and /capture/{linkId} alike (wsbridge.requireSession/
// sameOrigin). A real browser carries these automatically via its cookie
// jar after loading the page; this fetches the cookie explicitly via one
// GET / first, exercising the same host boundary a browser would.
func wsDialWithSession(addr, path string) (net.Conn, error) {
	cookie, err := fetchSessionCookie(addr)
	if err != nil {
		return nil, fmt.Errorf("%s session: %w", path, err)
	}
	return wsDialWithHeaders(addr, path, map[string]string{
		"Cookie": "iolbox_session=" + cookie,
		"Origin": "http://" + addr,
	})
}

// dialControlWS connects to ws://127.0.0.1:<port>/control.
func dialControlWS(addr string) (*controlWSClient, error) {
	conn, err := wsDialWithSession(addr, "/control")
	if err != nil {
		return nil, err
	}
	return &controlWSClient{
		conn:    conn,
		br:      bufio.NewReader(conn),
		timeout: 15 * time.Second,
	}, nil
}

func (c *controlWSClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// request sends {"id","op","args"} and waits for the matching response
// envelope, ignoring any frame (event or otherwise) whose id doesn't match.
// Enforces a per-request read deadline.
func (c *controlWSClient) request(op string, args any) (json.RawMessage, error) {
	return c.requestTimeout(op, args, c.timeout)
}

// hello is the typed browser-side control handshake. It intentionally travels
// over the GUI WebSocket rather than the guest-loopback TCP control port so
// Darwin lifecycle checks exercise the same host boundary as the browser.
func (c *controlWSClient) hello() (helloResult, error) {
	result, err := c.request("hello", map[string]string{"client": "iolbox-launcher"})
	if err != nil {
		return helloResult{}, err
	}
	var hello helloResult
	if err := json.Unmarshal(result, &hello); err != nil {
		return helloResult{}, fmt.Errorf("decode hello result: %w", err)
	}
	return hello, nil
}

// requestTimeout is request with an explicit per-call read/write deadline.
// Slow verbs need far more than the default: image.register sha256s and
// scans a ~300 MB IOL image, and inside the QEMU-TCG guest (emulated CPU)
// that runs well past the 15s default — see imageRegisterTimeout.
func (c *controlWSClient) requestTimeout(op string, args any, to time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := strconv.FormatInt(atomic.AddInt64(&c.nextID, 1), 10)
	req := envelope{ID: id, Op: op, Args: args}
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ws request: marshal: %w", err)
	}

	maskKey, err := newMaskKey()
	if err != nil {
		return nil, fmt.Errorf("ws request: mask key: %w", err)
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(to))
	if err := writeTextFrame(c.conn, buf, maskKey); err != nil {
		return nil, fmt.Errorf("ws request: write: %w", err)
	}

	deadline := time.Now().Add(to)
	for {
		_ = c.conn.SetReadDeadline(deadline)
		opcode, payload, err := readFrame(c.br)
		if err != nil {
			return nil, fmt.Errorf("ws request: read: %w", err)
		}
		switch opcode {
		case wsOpPing:
			// Reply with a pong carrying the same payload, then keep waiting.
			pongMask, merr := newMaskKey()
			if merr == nil {
				_ = c.conn.SetWriteDeadline(time.Now().Add(to))
				_ = writeControlFrame(c.conn, wsOpPong, payload, pongMask)
			}
			continue
		case wsOpPong:
			continue
		case wsOpClose:
			return nil, fmt.Errorf("ws request: server closed the connection")
		case wsOpText:
			var env envelope
			if err := json.Unmarshal(payload, &env); err != nil {
				// Malformed frame; ignore and keep waiting (could be noise).
				continue
			}
			if env.Event != "" || env.ID != id {
				// Unsolicited event, or a response to a different in-flight
				// request (shouldn't happen given the mutex, but be defensive).
				continue
			}
			if env.OK != nil && !*env.OK {
				if env.Error != nil {
					return nil, env.Error
				}
				return nil, fmt.Errorf("ws request %s: request failed with no error detail", op)
			}
			return env.Result, nil
		default:
			// Binary or unknown opcode: ignore.
			continue
		}
	}
}

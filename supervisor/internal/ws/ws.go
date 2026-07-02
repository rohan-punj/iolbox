// Package ws is a minimal, dependency-free server-side implementation of
// RFC 6455 (The WebSocket Protocol). It supports exactly what the supervisor
// needs: the opening HTTP handshake, text/binary/close/ping/pong frames,
// unmasking of client frames, masking is NOT applied to server frames (per
// spec, server-to-client frames must not be masked), and no permessage-deflate
// extension. This keeps the supervisor at zero third-party dependencies.
//
// Rationale for hand-rolling instead of taking a dependency: the subset of
// RFC 6455 a loopback-only, trusted-client control/console bridge needs is
// small (~a few hundred lines) and stable (the RFC hasn't changed since 2011).
// Avoiding a dependency keeps `go build`/`go mod tidy` deterministic offline
// and keeps the supervisor's trusted-execution surface minimal. If future
// requirements grow (compression, strict fuzz-tested conformance), swap to
// nhooyr.io/websocket or github.com/coder/websocket without touching callers
// of this package's Conn API.
package ws

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// magicGUID is the fixed GUID used to compute Sec-WebSocket-Accept (RFC 6455 §1.3).
const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Opcode identifies a WebSocket frame type.
type Opcode uint8

// Frame opcodes defined by RFC 6455 §5.2.
const (
	OpContinuation Opcode = 0x0
	OpText         Opcode = 0x1
	OpBinary       Opcode = 0x2
	OpClose        Opcode = 0x8
	OpPing         Opcode = 0x9
	OpPong         Opcode = 0xA
)

// maxFrameSize bounds a single frame payload to guard against a misbehaving
// or hostile peer exhausting memory. The control protocol and console traffic
// never need frames anywhere near this large.
const maxFrameSize = 16 << 20 // 16 MiB

// ErrClosed is returned by Read/Write after the connection has been closed.
var ErrClosed = errors.New("ws: connection closed")

// Accept performs the WebSocket opening handshake on an incoming HTTP
// request and returns a Conn wrapping the hijacked TCP connection. The
// caller's ResponseWriter must support hijacking (true for net/http's
// standard server on a TCP listener).
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "expected Upgrade: websocket", http.StatusBadRequest)
		return nil, errors.New("ws: missing Upgrade: websocket header")
	}
	if !headerContainsToken(r.Header.Get("Connection"), "upgrade") {
		http.Error(w, "expected Connection: Upgrade", http.StatusBadRequest)
		return nil, errors.New("ws: missing Connection: Upgrade header")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("ws: missing Sec-WebSocket-Key header")
	}
	if v := r.Header.Get("Sec-WebSocket-Version"); v != "13" {
		http.Error(w, "unsupported Sec-WebSocket-Version", http.StatusBadRequest)
		return nil, fmt.Errorf("ws: unsupported Sec-WebSocket-Version %q", v)
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return nil, errors.New("ws: ResponseWriter does not support hijacking")
	}
	netConn, buf, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("ws: hijack: %w", err)
	}

	accept := computeAccept(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := buf.WriteString(resp); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("ws: write handshake: %w", err)
	}
	if err := buf.Flush(); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("ws: flush handshake: %w", err)
	}

	return newConn(netConn, buf.Reader), nil
}

// computeAccept derives Sec-WebSocket-Accept from the client's key per RFC
// 6455 §1.3: base64(sha1(key + magicGUID)).
func computeAccept(key string) string {
	h := sha1.New()
	io.WriteString(h, key)
	io.WriteString(h, magicGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// headerContainsToken reports whether comma-separated header value v contains
// token (case-insensitive), e.g. Connection: "keep-alive, Upgrade".
func headerContainsToken(v, token string) bool {
	for _, part := range strings.Split(v, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// Conn is a single WebSocket connection. Reads and writes are safe from
// separate goroutines (one reader, one writer); concurrent writers must
// serialize themselves (WriteMessage takes an internal lock).
type Conn struct {
	nc net.Conn
	br *bufio.Reader

	writeMu sync.Mutex
	closed  bool
	closeMu sync.Mutex
}

func newConn(nc net.Conn, br *bufio.Reader) *Conn {
	return &Conn{nc: nc, br: br}
}

// RemoteAddr returns the underlying connection's remote address.
func (c *Conn) RemoteAddr() net.Addr { return c.nc.RemoteAddr() }

// Close closes the underlying TCP connection without a close handshake. Use
// WriteClose to send a close frame first when a graceful shutdown is wanted.
func (c *Conn) Close() error {
	c.closeMu.Lock()
	c.closed = true
	c.closeMu.Unlock()
	return c.nc.Close()
}

// isClosed reports whether Close has been called.
func (c *Conn) isClosed() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closed
}

// ReadMessage reads one complete message (coalescing continuation frames) and
// returns its opcode (OpText or OpBinary) and payload. Ping/Pong/Close frames
// are handled internally: pings are answered with a pong automatically, and a
// received Close frame returns io.EOF after echoing a Close frame back.
func (c *Conn) ReadMessage() (Opcode, []byte, error) {
	for {
		op, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case OpPing:
			if err := c.writeFrame(OpPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case OpPong:
			continue
		case OpClose:
			_ = c.writeFrame(OpClose, payload)
			return OpClose, payload, io.EOF
		case OpText, OpBinary:
			return op, payload, nil
		default:
			return 0, nil, fmt.Errorf("ws: unexpected opcode %d", op)
		}
	}
}

// frameHeader captures a decoded frame header (RFC 6455 §5.2).
type frameHeader struct {
	fin     bool
	opcode  Opcode
	masked  bool
	length  uint64
	maskKey [4]byte
}

// readFrame reads one raw frame (following continuations to reassemble a full
// message for data frames; control frames are never fragmented per spec).
func (c *Conn) readFrame() (Opcode, []byte, error) {
	var messageOp Opcode
	var payload []byte
	first := true

	for {
		hdr, err := c.readFrameHeader()
		if err != nil {
			return 0, nil, err
		}
		if !hdr.masked {
			return 0, nil, errors.New("ws: client frame not masked")
		}
		if hdr.length > maxFrameSize {
			return 0, nil, fmt.Errorf("ws: frame too large (%d bytes)", hdr.length)
		}
		data := make([]byte, hdr.length)
		if _, err := io.ReadFull(c.br, data); err != nil {
			return 0, nil, err
		}
		unmask(data, hdr.maskKey)

		if hdr.opcode == OpPing || hdr.opcode == OpPong || hdr.opcode == OpClose {
			// Control frames are never fragmented; return immediately.
			return hdr.opcode, data, nil
		}

		if first {
			if hdr.opcode == OpContinuation {
				return 0, nil, errors.New("ws: unexpected continuation frame")
			}
			messageOp = hdr.opcode
			first = false
		}
		payload = append(payload, data...)
		if len(payload) > maxFrameSize {
			return 0, nil, fmt.Errorf("ws: message too large (%d bytes)", len(payload))
		}
		if hdr.fin {
			return messageOp, payload, nil
		}
		// else: loop to read the next continuation frame
	}
}

func (c *Conn) readFrameHeader() (frameHeader, error) {
	var hdr frameHeader
	b := make([]byte, 2)
	if _, err := io.ReadFull(c.br, b); err != nil {
		return hdr, err
	}
	hdr.fin = b[0]&0x80 != 0
	rsv := b[0] & 0x70
	if rsv != 0 {
		return hdr, errors.New("ws: nonzero RSV bits (unsupported extension)")
	}
	hdr.opcode = Opcode(b[0] & 0x0F)
	hdr.masked = b[1]&0x80 != 0
	length := uint64(b[1] & 0x7F)

	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return hdr, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return hdr, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	hdr.length = length

	if hdr.masked {
		mk := make([]byte, 4)
		if _, err := io.ReadFull(c.br, mk); err != nil {
			return hdr, err
		}
		copy(hdr.maskKey[:], mk)
	}
	return hdr, nil
}

// unmask applies the RFC 6455 XOR unmasking algorithm in place.
func unmask(data []byte, key [4]byte) {
	for i := range data {
		data[i] ^= key[i%4]
	}
}

// WriteMessage sends one unfragmented message of the given opcode (OpText or
// OpBinary). Safe for concurrent use.
func (c *Conn) WriteMessage(op Opcode, payload []byte) error {
	if c.isClosed() {
		return ErrClosed
	}
	return c.writeFrame(op, payload)
}

// WriteClose sends a Close frame with the given status code and reason.
func (c *Conn) WriteClose(code uint16, reason string) error {
	if c.isClosed() {
		return ErrClosed
	}
	buf := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(buf, code)
	copy(buf[2:], reason)
	return c.writeFrame(OpClose, buf)
}

// writeFrame writes a single, unfragmented, unmasked (server->client per
// spec) frame.
func (c *Conn) writeFrame(op Opcode, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var hdr bytes.Buffer
	hdr.WriteByte(0x80 | byte(op)) // FIN=1, opcode

	n := len(payload)
	switch {
	case n < 126:
		hdr.WriteByte(byte(n))
	case n <= 0xFFFF:
		hdr.WriteByte(126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		hdr.Write(ext[:])
	default:
		hdr.WriteByte(127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr.Write(ext[:])
	}

	if _, err := c.nc.Write(hdr.Bytes()); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.nc.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

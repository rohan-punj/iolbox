package wsbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
	"github.com/rohanpunj/iolab/supervisor/internal/server"
	"github.com/rohanpunj/iolab/supervisor/internal/ws"
)

// --- minimal WebSocket client helpers (test-only; mirrors internal/ws framing) ---

func dialWS(t *testing.T, url string) net.Conn {
	t.Helper()
	addr := strings.TrimPrefix(url, "ws://")
	path := "/"
	if i := strings.Index(addr, "/"); i >= 0 {
		path = addr[i:]
		addr = addr[:i]
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("handshake failed: %s", status)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return &bufReadConn{Conn: conn, br: br}
}

// bufReadConn lets us keep using net.Conn's interface after consuming the
// handshake through a bufio.Reader (so buffered bytes aren't lost).
type bufReadConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufReadConn) Read(p []byte) (int, error) { return c.br.Read(p) }

func writeClientFrame(conn net.Conn, op ws.Opcode, payload []byte) error {
	var buf bytes.Buffer
	buf.WriteByte(0x80 | byte(op))
	n := len(payload)
	switch {
	case n < 126:
		buf.WriteByte(0x80 | byte(n))
	case n <= 0xFFFF:
		buf.WriteByte(0x80 | 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		buf.Write(ext[:])
	default:
		buf.WriteByte(0x80 | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		buf.Write(ext[:])
	}
	var key [4]byte
	rand.Read(key[:])
	buf.Write(key[:])
	masked := make([]byte, n)
	copy(masked, payload)
	for i := range masked {
		masked[i] ^= key[i%4]
	}
	buf.Write(masked)
	_, err := conn.Write(buf.Bytes())
	return err
}

func readServerFrame(t *testing.T, conn net.Conn) (ws.Opcode, []byte) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	b0, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read frame byte0: %v", err)
	}
	b1, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read frame byte1: %v", err)
	}
	op := ws.Opcode(b0 & 0x0F)
	length := uint64(b1 & 0x7F) // server frames are never masked
	switch length {
	case 126:
		var ext [2]byte
		io.ReadFull(br, ext[:])
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		io.ReadFull(br, ext[:])
		length = binary.BigEndian.Uint64(ext[:])
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(br, data); err != nil {
		t.Fatalf("read frame payload: %v", err)
	}
	return op, data
}

// --- /control end-to-end test ---

func TestControlEndpointRoundTrip(t *testing.T) {
	srv := server.New(server.Config{ControlAddr: "127.0.0.1:0", ImageDir: "/img", RunDir: "/run", Version: "test"})
	b := New(Config{Addr: "127.0.0.1:0"}, srv)

	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/control"
	conn := dialWS(t, wsURL)
	defer conn.Close()

	req := protocol.Request{ID: "1", Op: "hello", Args: mustJSON(protocol.HelloArgs{Client: "test"})}
	reqBytes, _ := json.Marshal(req)
	if err := writeClientFrame(conn, ws.OpText, reqBytes); err != nil {
		t.Fatalf("write request frame: %v", err)
	}

	op, data := readServerFrame(t, conn)
	if op != ws.OpText {
		t.Fatalf("expected text frame, got opcode %d", op)
	}
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v (data=%s)", err, data)
	}
	if !resp.OK || resp.ID != "1" {
		t.Fatalf("hello over ws failed: %+v", resp)
	}
	var hr protocol.HelloResult
	json.Unmarshal(resp.Result, &hr)
	if hr.Supervisor != "test" {
		t.Fatalf("hello result: %+v", hr)
	}
}

func TestControlEndpointMultipleRequests(t *testing.T) {
	srv := server.New(server.Config{ControlAddr: "127.0.0.1:0", ImageDir: "/img", RunDir: "/run", Version: "test"})
	b := New(Config{Addr: "127.0.0.1:0"}, srv)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/control"
	conn := dialWS(t, wsURL)
	defer conn.Close()

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("req-%d", i)
		req := protocol.Request{ID: id, Op: "status"}
		reqBytes, _ := json.Marshal(req)
		if err := writeClientFrame(conn, ws.OpText, reqBytes); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, data := readServerFrame(t, conn)
		var resp protocol.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.ID != id || !resp.OK {
			t.Fatalf("resp %d: %+v", i, resp)
		}
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// --- console bridge test ---

func TestConsoleEndpointTelnetNegotiationAndData(t *testing.T) {
	// Fake telnet server: on accept, sends IAC WILL ECHO, IAC WILL SGA, then
	// "login: " and echoes back whatever it receives after that, so the test
	// can assert both negotiation stripping and bidirectional data flow.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte{telnetIAC, telnetWILL, telnetOptEcho})
		c.Write([]byte{telnetIAC, telnetWILL, telnetOptSGA})
		c.Write([]byte("login: "))
		// The bridge answers our WILL ECHO/WILL SGA with IAC DO ECHO/IAC DO
		// SGA negotiation replies before the test's keystroke arrives; skip
		// those (any read consisting solely of IAC-prefixed bytes) and only
		// echo the first read that looks like real application data.
		buf := make([]byte, 256)
		for {
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			if n > 0 && buf[0] == telnetIAC {
				continue // negotiation reply, not a keystroke
			}
			c.Write(buf[:n]) // echo back what the client typed
			return
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	fake := &fakeControlServer{consolePorts: map[int]int{7: port}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	wsURL := "ws://" + strings.TrimPrefix(ts.URL, "http://") + "/console/7"
	conn := dialWS(t, wsURL)
	defer conn.Close()

	// First frame(s) from the bridge should be clean application data with
	// IAC negotiation stripped: "login: " with no 0xFF bytes.
	op, data := readServerFrame(t, conn)
	if op != ws.OpBinary {
		t.Fatalf("expected binary frame, got opcode %d", op)
	}
	if bytes.Contains(data, []byte{0xFF}) {
		t.Fatalf("IAC byte leaked into client frame: %v", data)
	}
	if !bytes.Contains(data, []byte("login: ")) {
		t.Fatalf("expected greeting text, got %q", data)
	}

	// Send a keystroke; expect it echoed back cleanly.
	if err := writeClientFrame(conn, ws.OpBinary, []byte("admin\n")); err != nil {
		t.Fatalf("write keystroke: %v", err)
	}
	op, data = readServerFrame(t, conn)
	if op != ws.OpBinary || string(data) != "admin\n" {
		t.Fatalf("echo: op=%d data=%q", op, data)
	}
}

func TestConsoleEndpointUnknownNode(t *testing.T) {
	fake := &fakeControlServer{consolePorts: map[int]int{}}
	b := New(Config{Addr: "127.0.0.1:0"}, fake)
	ts := httptest.NewServer(b.server.Handler)
	defer ts.Close()

	resp, err := httpGetUpgradeAttempt(ts.URL + "/console/99")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !strings.Contains(resp, "404") {
		t.Fatalf("expected 404 for unknown node, got: %s", resp)
	}
}

// httpGetUpgradeAttempt issues a plain (non-websocket) GET and returns the
// raw status line, enough to check the 404 path without a full handshake.
func httpGetUpgradeAttempt(url string) (string, error) {
	addr := strings.TrimPrefix(url, "http://")
	path := "/"
	if i := strings.Index(addr, "/"); i >= 0 {
		path = addr[i:]
		addr = addr[:i]
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", path, addr)
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	return status, err
}

// fakeControlServer implements ControlServer without a real lab/server.Server,
// for testing the console bridge in isolation.
type fakeControlServer struct {
	consolePorts map[int]int
}

func (f *fakeControlServer) ServeConn(ctx context.Context, rwc io.ReadWriteCloser) {}

func (f *fakeControlServer) ConsolePort(nodeID int) (int, bool) {
	p, ok := f.consolePorts[nodeID]
	return p, ok
}

// Telnet constants duplicated here (rather than importing internal/telnet's
// unexported details) to keep this test focused on wire bytes as an external
// telnet peer would send them.
const (
	telnetIAC     = 255
	telnetWILL    = 251
	telnetOptEcho = 1
	telnetOptSGA  = 3
)

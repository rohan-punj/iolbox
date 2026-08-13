package ws

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// clientFrame encodes a masked client->server frame the way a real browser
// would, for feeding into Conn.readFrame via a net.Pipe.
func clientFrame(op Opcode, payload []byte, fin bool) []byte {
	var buf bytes.Buffer
	b0 := byte(op)
	if fin {
		b0 |= 0x80
	}
	buf.WriteByte(b0)

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
	unmask(masked, key)
	buf.Write(masked)
	return buf.Bytes()
}

func pipeConns(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	srv := newConn(a, bufio.NewReader(a))
	return srv, b
}

func TestReadMessageText(t *testing.T) {
	srv, client := pipeConns(t)
	defer srv.Close()
	defer client.Close()

	go func() {
		client.Write(clientFrame(OpText, []byte("hello"), true))
	}()

	op, data, err := srv.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText || string(data) != "hello" {
		t.Fatalf("got op=%d data=%q", op, data)
	}
}

func TestReadMessageBinaryFragmented(t *testing.T) {
	srv, client := pipeConns(t)
	defer srv.Close()
	defer client.Close()

	go func() {
		// Split "abcdef" into two continuation frames.
		var buf bytes.Buffer
		// first frame: opcode binary, FIN=0
		f1 := clientFrame(OpBinary, []byte("abc"), false)
		buf.Write(f1)
		// continuation frame: opcode 0x0, FIN=1
		f2 := clientFrame(OpContinuation, []byte("def"), true)
		buf.Write(f2)
		client.Write(buf.Bytes())
	}()

	op, data, err := srv.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpBinary || string(data) != "abcdef" {
		t.Fatalf("got op=%d data=%q", op, data)
	}
}

func TestPingAnsweredWithPong(t *testing.T) {
	srv, client := pipeConns(t)
	defer srv.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		client.Write(clientFrame(OpPing, []byte("ping-data"), true))
		client.Write(clientFrame(OpText, []byte("after"), true))
		close(done)
	}()

	// Server should transparently answer the ping with a pong (no
	// application-visible message) and then deliver the text message.
	readerDone := make(chan struct{})
	var op Opcode
	var data []byte
	var err error
	go func() {
		op, data, err = srv.ReadMessage()
		close(readerDone)
	}()

	// Read the pong frame off the client side of the pipe.
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(client)
	b0, e1 := br.ReadByte()
	b1, e2 := br.ReadByte()
	if e1 != nil || e2 != nil {
		t.Fatalf("read pong header: %v %v", e1, e2)
	}
	if Opcode(b0&0x0F) != OpPong {
		t.Fatalf("expected pong opcode, got %d", b0&0x0F)
	}
	plen := int(b1 & 0x7F)
	pong := make([]byte, plen)
	io.ReadFull(br, pong)
	if string(pong) != "ping-data" {
		t.Fatalf("pong payload = %q", pong)
	}

	<-done
	<-readerDone
	if err != nil {
		t.Fatalf("ReadMessage after ping: %v", err)
	}
	if op != OpText || string(data) != "after" {
		t.Fatalf("got op=%d data=%q", op, data)
	}
}

func TestCloseFrameReturnsEOF(t *testing.T) {
	srv, client := pipeConns(t)
	defer srv.Close()
	defer client.Close()

	client.SetDeadline(time.Now().Add(5 * time.Second))
	go func() {
		client.Write(clientFrame(OpClose, []byte{}, true))
	}()
	// The server echoes a Close frame back per RFC 6455 §5.5.1; drain it on a
	// separate goroutine since net.Pipe is unbuffered/synchronous and the
	// server's echoed write would otherwise block forever with no reader.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		buf := make([]byte, 64)
		client.Read(buf)
	}()

	_, _, err := srv.ReadMessage()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	<-drained
}

func TestUnmaskedClientFrameRejected(t *testing.T) {
	srv, client := pipeConns(t)
	defer srv.Close()
	defer client.Close()

	go func() {
		// Unmasked frame (server MUST reject per RFC 6455 §5.1).
		client.Write([]byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'})
	}()

	_, _, err := srv.ReadMessage()
	if err == nil {
		t.Fatal("expected error for unmasked client frame")
	}
}

func TestWriteMessageRoundTrip(t *testing.T) {
	srv, client := pipeConns(t)
	defer srv.Close()
	defer client.Close()

	payload := bytes.Repeat([]byte("x"), 70000) // forces the 64-bit length path
	go func() {
		if err := srv.WriteMessage(OpBinary, payload); err != nil {
			t.Errorf("WriteMessage: %v", err)
		}
	}()

	br := bufio.NewReader(client)
	b0, _ := br.ReadByte()
	b1, _ := br.ReadByte()
	if b0 != 0x82 { // FIN + binary
		t.Fatalf("b0 = %#x", b0)
	}
	if b1 != 127 {
		t.Fatalf("expected 64-bit length marker, got %d", b1)
	}
	var extLen [8]byte
	io.ReadFull(br, extLen[:])
	n := binary.BigEndian.Uint64(extLen[:])
	if int(n) != len(payload) {
		t.Fatalf("length = %d, want %d", n, len(payload))
	}
	got := make([]byte, n)
	io.ReadFull(br, got)
	if !bytes.Equal(got, payload) {
		t.Fatal("payload mismatch")
	}
	// Server frames must not be masked (top bit of b1's length byte is 0).
}

func TestAcceptHandshake(t *testing.T) {
	// Accept flushes the 101 response to the wire before it returns, so the
	// client below can finish reading the handshake while the handler
	// goroutine has not yet stored its Conn. Hand the Conn over a channel and
	// wait for it instead of racing on a shared variable.
	conns := make(chan *Conn, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r)
		if err != nil {
			t.Errorf("Accept: %v", err)
			close(conns)
			return
		}
		conns <- c
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := "GET /control HTTP/1.1\r\n" +
		"Host: 127.0.0.1\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if statusLine != "HTTP/1.1 101 Switching Protocols\r\n" {
		t.Fatalf("status line = %q", statusLine)
	}
	var acceptHeader string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		if line == "\r\n" {
			break
		}
		if bytes.HasPrefix([]byte(line), []byte("Sec-WebSocket-Accept:")) {
			acceptHeader = line
		}
	}
	want := computeAccept("dGhlIHNhbXBsZSBub25jZQ==")
	if acceptHeader == "" || !bytes.Contains([]byte(acceptHeader), []byte(want)) {
		t.Fatalf("Sec-WebSocket-Accept = %q, want containing %q", acceptHeader, want)
	}
	select {
	case gotConn := <-conns:
		if gotConn == nil {
			t.Fatal("Accept did not produce a Conn")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Accept to produce a Conn")
	}
}

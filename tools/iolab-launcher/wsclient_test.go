package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// ---- Frame codec roundtrip ---------------------------------------------

func TestFrameRoundTrip_Sizes(t *testing.T) {
	sizes := []int{0, 1, 5, 125, 126, 200, 0xFFFF, 70000}
	for _, n := range sizes {
		n := n
		t.Run(fmt.Sprintf("size=%d", n), func(t *testing.T) {
			payload := make([]byte, n)
			for i := range payload {
				payload[i] = byte(i % 251)
			}
			maskKey, err := newMaskKey()
			if err != nil {
				t.Fatalf("newMaskKey: %v", err)
			}

			var buf bytes.Buffer
			if err := writeTextFrame(&buf, payload, maskKey); err != nil {
				t.Fatalf("writeTextFrame: %v", err)
			}

			opcode, got, err := readFrame(&buf)
			if err != nil {
				t.Fatalf("readFrame: %v", err)
			}
			if opcode != wsOpText {
				t.Errorf("opcode = %d, want %d", opcode, wsOpText)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("payload mismatch: got %d bytes, want %d bytes", len(got), len(payload))
			}
		})
	}
}

// TestFrameLengthEncoding checks the header uses the right length-prefix form
// for each size class: <=125 inline, 126 -> 16-bit extended, >0xFFFF -> 64-bit.
func TestFrameLengthEncoding(t *testing.T) {
	cases := []struct {
		n        int
		wantByte byte // the masked length byte (with MASK bit set)
	}{
		{5, 0x80 | 5},
		{125, 0x80 | 125},
		{126, 0x80 | 126},
		{70000, 0x80 | 127},
	}
	for _, c := range cases {
		maskKey, _ := newMaskKey()
		var buf bytes.Buffer
		if err := writeTextFrame(&buf, make([]byte, c.n), maskKey); err != nil {
			t.Fatalf("writeTextFrame(%d): %v", c.n, err)
		}
		b := buf.Bytes()
		if len(b) < 2 {
			t.Fatalf("frame too short for n=%d", c.n)
		}
		if b[1] != c.wantByte {
			t.Errorf("n=%d: length byte = 0x%02x, want 0x%02x", c.n, b[1], c.wantByte)
		}
		// MASK bit must always be set on client frames.
		if b[1]&0x80 == 0 {
			t.Errorf("n=%d: MASK bit not set", c.n)
		}
		// FIN + opcode byte.
		if b[0] != 0x80|wsOpText {
			t.Errorf("n=%d: first byte = 0x%02x, want FIN+text", c.n, b[0])
		}
	}
}

func TestFrameMasking_XORIsReversible(t *testing.T) {
	payload := []byte("hello control channel")
	maskKey := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}

	var buf bytes.Buffer
	if err := writeTextFrame(&buf, payload, maskKey); err != nil {
		t.Fatalf("writeTextFrame: %v", err)
	}
	raw := buf.Bytes()

	// Header is 2 bytes (len 22 <=125) + 4-byte mask key.
	hdrLen := 2 + 4
	masked := raw[hdrLen:]
	if len(masked) != len(payload) {
		t.Fatalf("masked payload length = %d, want %d", len(masked), len(payload))
	}
	for i, b := range masked {
		if b == payload[i] && payload[i] != (payload[i]^maskKey[i%4]^payload[i]) {
			// Just a sanity guard; real check is XOR below.
			_ = b
		}
	}
	for i := range payload {
		if masked[i]^maskKey[i%4] != payload[i] {
			t.Fatalf("byte %d does not XOR back to original", i)
		}
	}
}

// ---- Ping/pong + close handling ----------------------------------------

// fakeServerConn pairs a net.Pipe so we can write frames a "server" would
// send and read frames the client sends back.
func pipePair(t *testing.T) (client net.Conn, server net.Conn) {
	t.Helper()
	c, s := net.Pipe()
	return c, s
}

func TestRequest_PingIsAnsweredWithPong(t *testing.T) {
	client, server := pipePair(t)
	defer client.Close()
	defer server.Close()

	cwc := &controlWSClient{
		conn:    client,
		br:      bufio.NewReader(client),
		timeout: 3 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		res, err := cwc.request("ping.op", map[string]string{"a": "b"})
		if err != nil {
			done <- err
			return
		}
		var m map[string]string
		if jerr := json.Unmarshal(res, &m); jerr != nil {
			done <- jerr
			return
		}
		if m["ok"] != "yes" {
			done <- fmt.Errorf("unexpected result: %v", m)
			return
		}
		done <- nil
	}()

	// Server side: read the client's request frame (masked), then send a
	// ping, expect a pong back, then send the real response.
	_, reqPayload, err := readFrame(server)
	if err != nil {
		t.Fatalf("server readFrame(request): %v", err)
	}
	var reqEnv envelope
	if err := json.Unmarshal(reqPayload, &reqEnv); err != nil {
		t.Fatalf("server unmarshal request: %v", err)
	}
	if reqEnv.Op != "ping.op" {
		t.Fatalf("server got op %q, want ping.op", reqEnv.Op)
	}

	// Send an unmasked ping frame (server->client frames are unmasked).
	if err := writeFrame(server, wsOpPing, []byte("hi"), nil); err != nil {
		t.Fatalf("server write ping: %v", err)
	}

	opcode, pongPayload, err := readFrame(server)
	if err != nil {
		t.Fatalf("server read pong: %v", err)
	}
	if opcode != wsOpPong {
		t.Fatalf("expected pong opcode, got %d", opcode)
	}
	if string(pongPayload) != "hi" {
		t.Fatalf("pong payload = %q, want %q", pongPayload, "hi")
	}

	// Now send the actual response.
	respBuf, _ := json.Marshal(envelope{ID: reqEnv.ID, OK: boolPtr(true), Result: json.RawMessage(`{"ok":"yes"}`)})
	if err := writeFrame(server, wsOpText, respBuf, nil); err != nil {
		t.Fatalf("server write response: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("request() returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for request() to complete")
	}
}

func TestRequest_IgnoresEventsAndMismatchedIDs(t *testing.T) {
	client, server := pipePair(t)
	defer client.Close()
	defer server.Close()

	cwc := &controlWSClient{
		conn:    client,
		br:      bufio.NewReader(client),
		timeout: 3 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		res, err := cwc.request("lab.saveDoc", map[string]string{})
		if err != nil {
			done <- err
			return
		}
		var m map[string]string
		if jerr := json.Unmarshal(res, &m); jerr != nil {
			done <- jerr
			return
		}
		if m["id"] != "lab-42" {
			done <- fmt.Errorf("unexpected result: %v", m)
			return
		}
		done <- nil
	}()

	_, reqPayload, err := readFrame(server)
	if err != nil {
		t.Fatalf("server readFrame(request): %v", err)
	}
	var reqEnv envelope
	_ = json.Unmarshal(reqPayload, &reqEnv)

	// Send an unsolicited event frame first.
	evBuf, _ := json.Marshal(envelope{Event: "node.started", Result: nil})
	if err := writeFrame(server, wsOpText, evBuf, nil); err != nil {
		t.Fatalf("write event: %v", err)
	}

	// Send a response with a mismatched id.
	wrongBuf, _ := json.Marshal(envelope{ID: "not-the-id", OK: boolPtr(true), Result: json.RawMessage(`{"id":"wrong"}`)})
	if err := writeFrame(server, wsOpText, wrongBuf, nil); err != nil {
		t.Fatalf("write mismatched response: %v", err)
	}

	// Finally the real response.
	rightBuf, _ := json.Marshal(envelope{ID: reqEnv.ID, OK: boolPtr(true), Result: json.RawMessage(`{"id":"lab-42"}`)})
	if err := writeFrame(server, wsOpText, rightBuf, nil); err != nil {
		t.Fatalf("write real response: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("request() returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func TestRequest_ErrorEnvelope(t *testing.T) {
	client, server := pipePair(t)
	defer client.Close()
	defer server.Close()

	cwc := &controlWSClient{
		conn:    client,
		br:      bufio.NewReader(client),
		timeout: 3 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		_, err := cwc.request("image.register", map[string]string{"path": "/nope"})
		done <- err
	}()

	_, reqPayload, err := readFrame(server)
	if err != nil {
		t.Fatalf("server readFrame(request): %v", err)
	}
	var reqEnv envelope
	_ = json.Unmarshal(reqPayload, &reqEnv)

	respBuf, _ := json.Marshal(envelope{
		ID:    reqEnv.ID,
		OK:    boolPtr(false),
		Error: &wsRPCError{Code: "not_found", Message: "image not found"},
	})
	if err := writeFrame(server, wsOpText, respBuf, nil); err != nil {
		t.Fatalf("write error response: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "not_found") || !strings.Contains(err.Error(), "image not found") {
			t.Errorf("error = %q, want it to mention code+message", err.Error())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func TestRequest_ServerCloseSurfacesError(t *testing.T) {
	client, server := pipePair(t)
	defer client.Close()

	cwc := &controlWSClient{
		conn:    client,
		br:      bufio.NewReader(client),
		timeout: 3 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		_, err := cwc.request("lab.listDocs", nil)
		done <- err
	}()

	_, _, err := readFrame(server)
	if err != nil {
		t.Fatalf("server readFrame(request): %v", err)
	}
	// Send a close frame, then close the pipe.
	if err := writeFrame(server, wsOpClose, nil, nil); err != nil {
		t.Fatalf("write close: %v", err)
	}
	server.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after server close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for close to surface")
	}
}

// ---- Handshake accept-key math ------------------------------------------

func TestWsAcceptKey_KnownVector(t *testing.T) {
	// The canonical RFC 6455 example (section 1.3).
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := wsAcceptKey(key); got != want {
		t.Errorf("wsAcceptKey(%q) = %q, want %q", key, got, want)
	}
}

func boolPtr(b bool) *bool { return &b }

// ---- Full handshake + end-to-end request/response over real TCP ---------

// TestDialControlWS_HandshakeAndRequestEndToEnd starts a real TCP listener,
// hand-writes the server side of the RFC 6455 handshake (reading the request
// line/headers, computing Sec-WebSocket-Accept, writing a 101 response), then
// exchanges one full request/response over actual frames — exercising
// wsDial + controlWSClient.request together, not just the pure framing
// functions.
func TestDialControlWS_HandshakeAndRequestEndToEnd(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveOneControlHandshake(ln)
	}()

	addr := ln.Addr().String()
	cwc, err := dialControlWS(addr)
	if err != nil {
		t.Fatalf("dialControlWS: %v", err)
	}
	defer cwc.Close()

	res, err := cwc.request("lab.listDocs", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var out struct {
		Labs []string `json:"labs"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(out.Labs) != 1 || out.Labs[0] != "seed1" {
		t.Errorf("result = %+v, want labs=[seed1]", out)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server side: %v", err)
	}
}

// serveOneControlHandshake accepts one connection, performs the server side
// of the WS upgrade handshake by hand, reads one client request frame, and
// writes back one text-frame response.
func serveOneControlHandshake(ln net.Listener) error {
	conn, err := ln.Accept()
	if err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	reqLine, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read request line: %w", err)
	}
	if !strings.HasPrefix(reqLine, "GET /control ") {
		return fmt.Errorf("unexpected request line: %q", reqLine)
	}

	var key string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Sec-WebSocket-Key") {
			key = strings.TrimSpace(v)
		}
	}
	if key == "" {
		return fmt.Errorf("no Sec-WebSocket-Key header seen")
	}

	accept := wsAcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		return fmt.Errorf("write 101: %w", err)
	}

	// Read the client's masked request frame.
	_, payload, err := readFrame(br)
	if err != nil {
		return fmt.Errorf("read request frame: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return fmt.Errorf("unmarshal request: %w", err)
	}
	if env.Op != "lab.listDocs" {
		return fmt.Errorf("unexpected op %q", env.Op)
	}

	respEnv := envelope{ID: env.ID, OK: boolPtr(true), Result: json.RawMessage(`{"labs":["seed1"]}`)}
	buf, err := json.Marshal(respEnv)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	// Server->client frames are unmasked.
	if err := writeFrame(conn, wsOpText, buf, nil); err != nil {
		return fmt.Errorf("write response frame: %w", err)
	}
	return nil
}
